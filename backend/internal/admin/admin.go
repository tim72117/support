// Package admin implements the HTTP API behind apps/admin — the
// platform-operator back office. It is NOT a separate account system: a
// caller authenticates exactly the way any console user does (the existing
// /auth/login, internal/session's cookie), and this package only adds one
// extra check on top of that session — is the caller's email in the
// ADMIN_EMAILS allowlist (internal/adminauth) — before it answers anything.
//
// Every route here returns 404, not 403, to a caller who is authenticated
// but not an admin — the same "don't confirm this exists" reasoning
// internal/console documents at withOwnedBusiness: a non-admin business
// owner poking at /admin/api/* should not be able to learn that an admin
// API exists at all, just as they can't learn whether some other owner's
// business id exists. An unauthenticated caller (no/invalid session) gets a
// plain 401, same as the rest of the console API — there is nothing to hide
// about whether a login is required.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/tim72117/ai-support/internal/adminauth"
	"github.com/tim72117/ai-support/internal/billing"
	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

type Handler struct {
	Session    *session.Store
	Admins     *adminauth.Allowlist
	Businesses *business.Store
	// Quota and Billing may be nil (QUOTA_ENABLED=false / billing not
	// configured — see cmd/server/main.go); the affected fields are simply
	// left zero/empty in the response rather than failing the request, same
	// convention as console.Handler.getQuota's nil-Quota handling.
	Quota   *quota.Service
	Billing *billing.Service
	log     *slog.Logger
}

func NewHandler(sessionStore *session.Store, admins *adminauth.Allowlist, businesses *business.Store, quotaSvc *quota.Service, billingSvc *billing.Service, log *slog.Logger) *Handler {
	return &Handler{Session: sessionStore, Admins: admins, Businesses: businesses, Quota: quotaSvc, Billing: billingSvc, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/api/me", h.me)
	mux.HandleFunc("GET /admin/api/users", h.withAdmin(h.listUsers))
	mux.HandleFunc("GET /admin/api/businesses", h.withAdmin(h.listBusinesses))
	mux.HandleFunc("POST /admin/api/users/{id}/tier", h.withAdmin(h.setTier))
}

// meResponse is deliberately the only admin route an authenticated non-admin
// can reach — the frontend calls it first to decide whether to show "you
// don't have admin access" instead of the back office, so it answers 200
// with isAdmin:false rather than 404 (unlike every other route here, there
// is no "existence" to hide: the caller already knows /admin/api/me exists
// because the frontend that is showing them this message just called it).
type meResponse struct {
	IsAdmin bool   `json:"isAdmin"`
	Email   string `json:"email,omitempty"`
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := h.Session.Verify(r)
	if !ok {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	isAdmin := h.Admins.IsAdmin(user.Email)
	resp := meResponse{IsAdmin: isAdmin}
	if isAdmin {
		resp.Email = user.Email
	}
	writeJSON(w, resp)
}

// withAdmin resolves the caller's session and rejects the request unless it
// both verifies AND the email is in the admin allowlist. A verified
// non-admin and an unverified caller are deliberately told apart (401 vs
// 404) — see this file's package doc comment.
func (h *Handler) withAdmin(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.Session.Verify(r)
		if !ok {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		if !h.Admins.IsAdmin(user.Email) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// ownerSummary is one row of the admin owner list: identity, businesses,
// plan/usage standing (quota), and billing status (TapPay), merged from the
// three sources that each track their own slice (see quota.UserSummary and
// billing.Profile's own doc comments for why these stay separate tables).
type ownerSummary struct {
	ID            int64  `json:"id"`
	Email         string `json:"email"`
	BusinessCount int    `json:"businessCount"`
	Tier          string `json:"tier,omitempty"`
	PlanName      string `json:"planName,omitempty"`
	Limit         int    `json:"limit"`
	Used          int    `json:"used"`

	// Billing fields are omitted entirely when there is no billing_profiles
	// row for this owner (never subscribed/trialed) or billing is disabled
	// on this server.
	BillingStatus    string `json:"billingStatus,omitempty"`
	CardLastFour     string `json:"cardLastFour,omitempty"`
	CurrentPeriodEnd string `json:"currentPeriodEnd,omitempty"`
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if h.Quota == nil {
		writeJSON(w, []ownerSummary{})
		return
	}
	users, err := h.Quota.ListUsers(r.Context())
	if err != nil {
		h.log.Error("admin: list users", "err", err)
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}

	var profiles map[int64]*billing.Profile
	if h.Billing != nil {
		profiles, err = h.Billing.ListProfiles(r.Context())
		if err != nil {
			h.log.Error("admin: list billing profiles", "err", err)
			// Non-fatal: the owner list is still useful without billing
			// status layered on, so degrade rather than fail the request.
			profiles = nil
		}
	}

	out := make([]ownerSummary, 0, len(users))
	for _, u := range users {
		row := ownerSummary{
			ID: u.ID, Email: u.Email, BusinessCount: u.BusinessCount,
			Tier: string(u.Tier), PlanName: u.PlanName, Limit: u.Limit, Used: u.Used,
		}
		if p, ok := profiles[u.ID]; ok {
			row.BillingStatus = p.Status
			row.CardLastFour = p.CardLastFour
			if !p.CurrentPeriodEnd.IsZero() {
				row.CurrentPeriodEnd = p.CurrentPeriodEnd.Format("2006-01-02")
			}
		}
		out = append(out, row)
	}
	writeJSON(w, out)
}

func (h *Handler) listBusinesses(w http.ResponseWriter, r *http.Request) {
	list, err := h.Businesses.ListAll()
	if err != nil {
		h.log.Error("admin: list businesses", "err", err)
		http.Error(w, "failed to list businesses", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// setTier lets an admin manually change an owner's plan tier — e.g. to
// comp a business, correct a failed webhook, or move someone off a
// deprecated plan. It only ever changes quota.subscriptions (what plan the
// owner is on), never billing_profiles or payments: this is a support
// action on entitlement, not a claim about what was actually paid, so it
// deliberately cannot be used to "operate payment" on the owner's behalf
// (issue refunds, charge cards, cancel in TapPay, ...).
func (h *Handler) setTier(w http.ResponseWriter, r *http.Request) {
	if h.Quota == nil {
		http.Error(w, "quota is not enabled on this server", http.StatusServiceUnavailable)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var body struct{ Tier string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.Quota.SetTier(r.Context(), id, quota.Tier(body.Tier)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
