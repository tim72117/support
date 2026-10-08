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
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

type Handler struct {
	Businesses *business.Store
	Session    *session.Store
	// Quota may be nil (QUOTA_ENABLED=false); GET /console/quota then
	// reports enabled=false instead of failing.
	Quota *quota.Service
	// Chats is optional, set directly after NewHandler (same convention as
	// onagent's console Handler.Events). A nil Chats makes the
	// conversation-reading routes answer 503.
	Chats *conversation.Store
	log   *slog.Logger
}

// MaxContentRunes caps the owner-written content. All businesses share one
// fixed onagent app now (see ONAGENT_APP_ID/ONAGENT_APP_KEY in
// cmd/server/main.go), so this backend never pushes content into onagent at
// all; content is only ever read back on demand through the
// list_sections/read_section tools (see backend/internal/public and
// backend/onagent-tools/*.yaml). The cap still exists because that content
// is read by an LLM on every tool call, so an unbounded value would still be
// an unbounded per-message cost.
const MaxContentRunes = 20000

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
	var body struct{ Slug, Name, Tagline, Mascot, ThemeColor, Layout string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	b, err := h.Businesses.CreateWithBranding(user.ID, body.Slug, business.Branding{
		Name: body.Name, Tagline: body.Tagline, Mascot: body.Mascot, ThemeColor: body.ThemeColor, Layout: body.Layout,
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
	// Nothing is provisioned on onagent here: every business shares the same
	// fixed onagent app (ONAGENT_APP_ID/ONAGENT_APP_KEY, read once at startup
	// in cmd/server/main.go, never stored per-business) — there is no
	// per-business app/key to create or sync.
	writeJSON(w, b)
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

// updateBusiness changes a business's name, tagline, mascot, colour and
// layout. The slug is not editable: it is the public URL and may already be
// printed or shared.
func (h *Handler) updateBusiness(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	var body struct{ Name, Tagline, Mascot, ThemeColor, Layout *string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.Businesses.Update(b.ID, business.Patch{
		Name: body.Name, Tagline: body.Tagline, Mascot: body.Mascot, ThemeColor: body.ThemeColor, Layout: body.Layout,
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
	w.WriteHeader(http.StatusNoContent)
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
