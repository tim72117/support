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

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/session"
)

type Handler struct {
	Businesses *business.Store
	Session    *session.Store
	log        *slog.Logger
}

func NewHandler(businesses *business.Store, sessionStore *session.Store, log *slog.Logger) *Handler {
	return &Handler{Businesses: businesses, Session: sessionStore, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.withAuth(h.me))

	mux.HandleFunc("GET /console/businesses", h.withAuth(h.listBusinesses))
	mux.HandleFunc("POST /console/businesses", h.withAuth(h.createBusiness))
	mux.HandleFunc("GET /console/businesses/{id}", h.withOwnedBusiness(h.getBusiness))
	mux.HandleFunc("DELETE /console/businesses/{id}", h.withOwnedBusiness(h.deleteBusiness))

	mux.HandleFunc("GET /console/businesses/{id}/content", h.withOwnedBusiness(h.getContent))
	mux.HandleFunc("PUT /console/businesses/{id}/content", h.withOwnedBusiness(h.putContent))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, err := h.Session.Register(body.Email, body.Password)
	if err != nil {
		if errors.Is(err, session.ErrEmailTaken) || errors.Is(err, session.ErrInvalidEmail) {
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

func (h *Handler) listBusinesses(w http.ResponseWriter, r *http.Request, user *session.User) {
	list, err := h.Businesses.ListByOwner(user.ID)
	if err != nil {
		http.Error(w, "failed to list businesses", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (h *Handler) createBusiness(w http.ResponseWriter, r *http.Request, user *session.User) {
	var body struct{ Slug, Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	b, err := h.Businesses.Create(user.ID, body.Slug, body.Name)
	if err != nil {
		if errors.Is(err, business.ErrSlugTaken) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "failed to create business", http.StatusInternalServerError)
		return
	}
	// TODO: call the onagentclient package here to create the corresponding
	// onagent app and store its app id / API key on this business row —
	// left unwired in this skeleton since it depends on onagent's actual
	// console API contract, which this scaffold doesn't assume.
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
	content, err := h.Businesses.GetContent(b.ID)
	if err != nil {
		http.Error(w, "failed to load content", http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct{ Content string }{content})
}

func (h *Handler) putContent(w http.ResponseWriter, r *http.Request, user *session.User, b *business.Business) {
	var body struct{ Content string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.Businesses.SetContent(b.ID, body.Content); err != nil {
		http.Error(w, "failed to save content", http.StatusInternalServerError)
		return
	}
	// TODO: push body.Content into onagent as this business's tool
	// definition via internal/onagentclient, so the change takes effect for
	// the consumer-facing chat immediately — not wired in this skeleton.
	w.WriteHeader(http.StatusNoContent)
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
