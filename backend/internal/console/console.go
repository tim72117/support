// Package console implements the HTTP API behind apps/console — the
// business-owner-facing UI for managing accounts, businesses (each one a
// consumer-facing support page), and the content the AI answers from.
//
// Ownership checks return 404, not 403, for a business the caller doesn't
// own — a nonexistent id and someone else's id must be indistinguishable to
// the caller, the same reasoning onagent's own console.go documents at its
// ownedAppOrNotFound.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/conversation"
	"github.com/tim72117/ai-support/internal/onagentclient"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

type Handler struct {
	Businesses *business.Store
	Session    *session.Store
	// Quota may be nil (QUOTA_ENABLED=false); GET /console/quota then
	// reports enabled=false instead of failing.
	Quota *quota.Service
	// Onagent and Chats are optional, set directly after NewHandler (same
	// convention as onagent's console Handler.Events). A nil/disabled Onagent
	// means businesses are saved but never provisioned on onagent; a nil Chats
	// makes the conversation-reading routes answer 503.
	Onagent *onagentclient.Client
	Chats   *conversation.Store
	log     *slog.Logger
}

// MaxContentRunes caps the owner-written content (it is sent to onagent as
// part of every prompt's system context).
const MaxContentRunes = onagentclient.MaxContentRunes

// Values of the "onagentSync" field in create/save/sync responses.
const (
	syncOK       = "ok"
	syncFailed   = "failed"   // saved here, onagent push failed; retry with POST .../onagent-sync
	syncDisabled = "disabled" // onagent integration not configured on this server
)

func NewHandler(businesses *business.Store, sessionStore *session.Store, quotaSvc *quota.Service, log *slog.Logger) *Handler {
	return &Handler{Businesses: businesses, Session: sessionStore, Quota: quotaSvc, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.withAuth(h.me))

	mux.HandleFunc("GET /console/quota", h.withAuth(h.getQuota))

	mux.HandleFunc("GET /console/businesses", h.withAuth(h.listBusinesses))
	mux.HandleFunc("POST /console/businesses", h.withAuth(h.createBusiness))
	mux.HandleFunc("GET /console/businesses/{id}", h.withOwnedBusiness(h.getBusiness))
	mux.HandleFunc("PATCH /console/businesses/{id}", h.withOwnedBusiness(h.updateBusiness))
	mux.HandleFunc("DELETE /console/businesses/{id}", h.withOwnedBusiness(h.deleteBusiness))

	mux.HandleFunc("GET /console/businesses/{id}/content", h.withOwnedBusiness(h.getContent))
	mux.HandleFunc("PUT /console/businesses/{id}/content", h.withOwnedBusiness(h.putContent))
	mux.HandleFunc("POST /console/businesses/{id}/onagent-sync", h.withOwnedBusiness(h.syncBusiness))

	mux.HandleFunc("GET /console/businesses/{id}/conversations", h.withOwnedBusiness(h.listConversations))
	mux.HandleFunc("GET /console/businesses/{id}/conversations/{cid}", h.withOwnedBusiness(h.getConversation))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, err := h.Session.Register(body.Email, body.Password)
	if err != nil {
		if errors.Is(err, session.ErrEmailTaken) || errors.Is(err, session.ErrInvalidEmail) || errors.Is(err, session.ErrPasswordTooShort) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.log.Error("register failed", "err", err)
		http.Error(w, "registration failed", http.StatusInternalServerError)
		return
	}
	if _, err := h.Session.CreateSession(w, user.ID); err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, user)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, err := h.Session.Login(body.Email, body.Password)
	if err != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}
	if _, err := h.Session.CreateSession(w, user.ID); err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, user)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.Session.Logout(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request, user *session.User) {
	writeJSON(w, user)
}

// quotaResponse is the caller's own plan + usage-this-period standing.
// Limit/Used are deliberately NOT omitempty: 0 is a real value there.
type quotaResponse struct {
	Enabled     bool      `json:"enabled"`
	Tier        string    `json:"tier,omitempty"`
	PlanName    string    `json:"planName,omitempty"`
	Limit       int       `json:"limit"`
	Used        int       `json:"used"`
	UsedPercent int       `json:"usedPercent"`
	PeriodStart time.Time `json:"periodStart,omitempty"`
	PeriodEnd   time.Time `json:"periodEnd,omitempty"`
}

// getQuota reports the calling owner's plan and current-period usage, across
// every business they own. A nil h.Quota is a normal 200 with enabled=false.
func (h *Handler) getQuota(w http.ResponseWriter, r *http.Request, user *session.User) {
	if h.Quota == nil {
		writeJSON(w, quotaResponse{Enabled: false})
		return
	}
	st, err := h.Quota.StandingFor(r.Context(), user.ID)
	if err != nil {
		h.log.Error("quota standing", "err", err)
		http.Error(w, "failed to load quota", http.StatusInternalServerError)
		return
	}
	var usedPercent int
	if st.Limit > 0 {
		usedPercent = (st.Used*100 + st.Limit/2) / st.Limit
	}
	writeJSON(w, quotaResponse{
		Enabled:     true,
		Tier:        string(st.Tier),
		PlanName:    st.PlanName,
		Limit:       st.Limit,
		Used:        st.Used,
		UsedPercent: usedPercent,
		PeriodStart: st.PeriodStart,
		PeriodEnd:   st.PeriodEnd,
	})
}

func (h *Handler) listBusinesses(w http.ResponseWriter, r *http.Request, user *session.User) {
	list, err := h.Businesses.ListByOwner(user.ID)
	if err != nil {
		http.Error(w, "failed to list businesses", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (h *Handler) createBusiness(w http.ResponseWriter, r *http.Request, user *session.User) {
	var body struct{ Slug, Name, Tagline, Mascot, ThemeColor string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	b, err := h.Businesses.CreateWithBranding(user.ID, body.Slug, business.Branding{
		Name: body.Name, Tagline: body.Tagline, Mascot: body.Mascot, ThemeColor: body.ThemeColor,
	})
	if err != nil {
		if errors.Is(err, business.ErrSlugTaken) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if isBadBusinessInput(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to create business", http.StatusInternalServerError)
		return
	}
	// The business row is already saved; provisioning its onagent app is
	// best-effort so a flaky/unconfigured onagent never loses the owner's
	// input. On failure it can be retried (POST .../onagent-sync, or any
	// later content save).
	status := h.syncOnagent(r.Context(), b)
	if fresh, err := h.Businesses.Get(b.ID); err == nil {
		b = fresh
	}
	writeJSON(w, struct {
		*business.Business
		OnagentSync string `json:"onagentSync"`
	}{b, status})
}

func (h *Handler) getBusiness(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	writeJSON(w, b)
}

func (h *Handler) deleteBusiness(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	if err := h.Businesses.Delete(b.ID); err != nil {
		http.Error(w, "failed to delete business", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getContent(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	content, sections, err := h.Businesses.GetContentAndSections(b.ID)
	if err != nil {
		http.Error(w, "failed to load content", http.StatusInternalServerError)
		return
	}
	// Sections is the editor's own state, stored and returned verbatim (null
	// when none was saved); Content is the flat text the AI reads.
	writeJSON(w, struct {
		Content  string
		Sections json.RawMessage
	}{content, sections})
}

// updateBusiness changes a business's name, tagline, mascot and colour. The
// slug is not editable: it is the public URL and may already be printed or shared.
func (h *Handler) updateBusiness(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	var body struct{ Name, Tagline, Mascot, ThemeColor *string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.Businesses.Update(b.ID, business.Patch{
		Name: body.Name, Tagline: body.Tagline, Mascot: body.Mascot, ThemeColor: body.ThemeColor,
	})
	if err != nil {
		if isBadBusinessInput(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.log.Error("update business", "err", err)
		http.Error(w, "failed to update business", http.StatusInternalServerError)
		return
	}
	writeJSON(w, updated)
}

// isBadBusinessInput reports errors that are the caller's to fix.
func isBadBusinessInput(err error) bool {
	return errors.Is(err, business.ErrInvalidSlug) || errors.Is(err, business.ErrInvalidName) || errors.Is(err, business.ErrInvalidBranding)
}

func (h *Handler) putContent(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	var body struct {
		Content  string
		Sections json.RawMessage
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(body.Content) > MaxContentRunes {
		http.Error(w, "content is too long", http.StatusBadRequest)
		return
	}
	// PUT replaces both: sections omitted/null clears the editor state so it
	// can never describe a different text than Content.
	if string(body.Sections) == "null" {
		body.Sections = nil
	}
	if err := h.Businesses.SetContentAndSections(b.ID, body.Content, body.Sections); err != nil {
		if errors.Is(err, business.ErrInvalidSections) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to save content", http.StatusInternalServerError)
		return
	}
	// Saved; pushing to onagent is best-effort (see createBusiness). The
	// outcome travels in a header so the success status stays 204.
	w.Header().Set("X-Onagent-Sync", h.syncOnagent(r.Context(), b))
	w.WriteHeader(http.StatusNoContent)
}

// syncBusiness retries provisioning/pushing for one business.
func (h *Handler) syncBusiness(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	switch status := h.syncOnagent(r.Context(), b); status {
	case syncDisabled:
		http.Error(w, "onagent integration is not configured", http.StatusServiceUnavailable)
	case syncFailed:
		http.Error(w, "could not sync with onagent, try again later", http.StatusBadGateway)
	default:
		writeJSON(w, map[string]string{"onagentSync": status})
	}
}

// syncOnagent brings onagent in line with this business: creates the app and
// issues its key if not done yet (each step is stored as soon as it succeeds,
// so a half-finished run resumes instead of duplicating), re-binds the allowed
// origins, and pushes the current content. Never returns an error: failures
// are logged (without tokens, keys or content) and reported as syncFailed.
func (h *Handler) syncOnagent(ctx context.Context, b *business.Business) string {
	if !h.Onagent.Enabled() {
		return syncDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	// Re-read: the caller's copy may predate an earlier partial provisioning.
	cur, err := h.Businesses.Get(b.ID)
	if err != nil {
		h.log.Error("onagent sync: load business", "business", b.ID, "err", err)
		return syncFailed
	}
	fail := func(step string, err error) string {
		h.log.Error("onagent sync failed", "business", b.ID, "step", step, "err", err)
		return syncFailed
	}

	appID := ""
	if cur.OnagentAppID != nil {
		appID = *cur.OnagentAppID
	}
	if appID == "" {
		appID = h.Onagent.NewAppID(cur.Slug)
		if err := h.Onagent.CreateApp(ctx, appID); err != nil {
			return fail("create app", err)
		}
		if err := h.Businesses.SetOnagentAppID(cur.ID, appID); err != nil {
			return fail("store app id", err)
		}
	}
	if cur.OnagentAPIKey == nil || *cur.OnagentAPIKey == "" {
		key, err := h.Onagent.IssueKey(ctx, appID)
		if err != nil {
			return fail("issue key", err)
		}
		if err := h.Businesses.SetOnagentKey(cur.ID, key); err != nil {
			return fail("store key", err)
		}
	}
	if err := h.Onagent.SetOrigins(ctx, appID); err != nil {
		return fail("set origins", err)
	}
	content, err := h.Businesses.GetContent(cur.ID)
	if err != nil {
		return fail("load content", err)
	}
	if err := h.Onagent.PushContent(ctx, appID, cur.Name, content); err != nil {
		return fail("push content", err)
	}
	return syncOK
}

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	if h.Chats == nil {
		http.Error(w, "conversations are unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset := 50, 0
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		limit = n
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = n
	}
	list, err := h.Chats.ListByBusiness(b.ID, limit, offset)
	if err != nil {
		h.log.Error("list conversations", "err", err)
		http.Error(w, "failed to list conversations", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []conversation.Conversation{}
	}
	writeJSON(w, list)
}

// getConversation returns one conversation's messages; a conversation that
// does not exist or belongs to another business is a plain 404.
func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	if h.Chats == nil {
		http.Error(w, "conversations are unavailable", http.StatusServiceUnavailable)
		return
	}
	c, err := h.Chats.Get(r.PathValue("cid"))
	if errors.Is(err, conversation.ErrNotFound) || (err == nil && c.BusinessID != b.ID) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "failed to load conversation", http.StatusInternalServerError)
		return
	}
	msgs, err := h.Chats.Messages(c.ID)
	if err != nil {
		http.Error(w, "failed to load conversation", http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []conversation.Message{}
	}
	writeJSON(w, struct {
		conversation.Conversation
		Messages []conversation.Message `json:"messages"`
	}{*c, msgs})
}

// withAuth resolves the caller's session cookie and rejects the request if
// it doesn't verify.
func (h *Handler) withAuth(next func(http.ResponseWriter, *http.Request, *session.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.Session.Verify(r)
		if !ok {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		next(w, r, user)
	}
}

// withOwnedBusiness resolves both the caller and the {id} path business,
// returning 404 (not 403) if it doesn't exist or belongs to someone else —
// see this file's package doc comment for why.
func (h *Handler) withOwnedBusiness(next func(http.ResponseWriter, *http.Request, *session.User, *business.Business)) http.HandlerFunc {
	return h.withAuth(func(w http.ResponseWriter, r *http.Request, user *session.User) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		b, err := h.Businesses.Get(id)
		if errors.Is(err, gorm.ErrRecordNotFound) || (b != nil && b.OwnerID != user.ID) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "failed to load business", http.StatusInternalServerError)
			return
		}
		next(w, r, user, b)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
