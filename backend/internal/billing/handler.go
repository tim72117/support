package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
	"github.com/tim72117/ai-support/internal/tappay"
)

// Handler serves the owner-facing billing API under /console/billing/.
// Service may be nil (TapPay not configured): config then reports
// enabled=false and every money-moving route answers 503.
type Handler struct {
	Service *Service
	Session *session.Store
	// Public is the browser-side TapPay config (app id / app key / env).
	// Never put the partner key in here.
	Public tappay.Config
	log    *slog.Logger
}

func NewHandler(svc *Service, sessionStore *session.Store, cfg tappay.Config, log *slog.Logger) *Handler {
	return &Handler{Service: svc, Session: sessionStore, Public: cfg, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /console/billing/config", h.config)
	mux.HandleFunc("GET /console/billing/plans", h.plans)
	mux.HandleFunc("GET /console/billing/subscription", h.withAuth(h.subscription))
	mux.HandleFunc("POST /console/billing/subscribe", h.withAuth(h.subscribe))
	mux.HandleFunc("POST /console/billing/start-trial", h.withAuth(h.startTrial))
	mux.HandleFunc("POST /console/billing/cancel", h.withAuth(h.cancel))
}

// config tells the page how to initialise TapPay's hosted card fields
// (TPDirect.setupSDK). Needs no login: the landing/subscribe page asks
// before the visitor has an account.
func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil || h.Public.AppID == "" || h.Public.AppKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "appId": h.Public.AppID, "appKey": h.Public.AppKey, "env": h.Public.Env(),
	})
}

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	type planView struct {
		Tier   string `json:"tier"`
		Name   string `json:"name"`
		Amount int    `json:"amount"`
	}
	out := []planView{}
	for _, p := range Prices() {
		out = append(out, planView{Tier: string(p.Tier), Name: quota.PlanFor(p.Tier).Name, Amount: p.Amount})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) subscription(w http.ResponseWriter, r *http.Request, user *session.User) {
	if h.Service == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	p, err := h.Service.Get(r.Context(), user.ID)
	if errors.Is(err, ErrNoSubscription) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request, user *session.User) {
	if h.Service == nil {
		http.Error(w, "billing is not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Tier       string `json:"tier"`
		Prime      string `json:"prime"`
		Cardholder struct {
			Name        string `json:"name"`
			Email       string `json:"email"`
			PhoneNumber string `json:"phoneNumber"`
		} `json:"cardholder"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	p, err := h.Service.Subscribe(r.Context(), user.ID, quota.Tier(body.Tier), body.Prime, tappay.Cardholder{
		Name: body.Cardholder.Name, Email: body.Cardholder.Email, PhoneNumber: body.Cardholder.PhoneNumber,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// startTrial begins a 7-day free trial for the logged-in owner. Unlike
// subscribe, it takes no body: no card is collected and nothing is charged,
// so there is no prime/cardholder to send.
func (h *Handler) startTrial(w http.ResponseWriter, r *http.Request, user *session.User) {
	if h.Service == nil {
		http.Error(w, "billing is not configured", http.StatusServiceUnavailable)
		return
	}
	p, err := h.Service.StartTrial(r.Context(), user.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, user *session.User) {
	if h.Service == nil {
		http.Error(w, "billing is not configured", http.StatusServiceUnavailable)
		return
	}
	p, err := h.Service.Cancel(r.Context(), user.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// fail maps service errors onto HTTP statuses. Internal detail (which may
// mention gateway internals) is logged, not sent to the browser.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var declined *DeclinedError
	switch {
	case errors.As(err, &declined):
		http.Error(w, "card declined: "+declined.Msg, http.StatusPaymentRequired)
	case errors.Is(err, ErrUnknownPlan), errors.Is(err, ErrInvalidRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrAlreadySubscribed), errors.Is(err, ErrPaymentInProgress):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoSubscription):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrGatewayUnavailable):
		h.log.Error("billing", "err", err)
		http.Error(w, "payment status unknown; please contact support before retrying", http.StatusBadGateway)
	default:
		h.log.Error("billing", "err", err)
		http.Error(w, "billing error", http.StatusInternalServerError)
	}
}

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
