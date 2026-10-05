// Command server runs the ai-support backend: business-owner accounts and
// the console API for managing businesses and their content. It never talks
// to an LLM itself — the consumer-facing chat page embeds onagent's own
// @onagent/bridge SDK directly, and this backend's only relationship to
// onagent is (once internal/onagentclient is wired up) pushing a business's
// content to onagent as that business's tool definitions.
package main

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/tim72117/ai-support/internal/billing"
	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/console"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/googleauth"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
	"github.com/tim72117/ai-support/internal/tappay"
)

const usage = `Usage: server [-h|--help]

Runs the ai-support backend: business-owner accounts and the console API.

Configured entirely via environment variables (optionally loaded from a
.env file in the working directory):

  ADDR                       Listen address (default ":8082")
  DATABASE_URL               Postgres DSN
  ALLOWED_ORIGIN             Comma-separated origins allowed to call
                              /console/* and /auth/* with credentials
                              (the apps/console dev server, and its
                              production origin).
  COOKIE_SECURE              "true" to send the session cookie with Secure
                              (required for any real deployment).
  GOOGLE_OAUTH_CLIENT_ID     Optional — enables "Sign in with Google".
  GOOGLE_OAUTH_CLIENT_SECRET
  GOOGLE_OAUTH_REDIRECT_URL
  TAPPAY_APP_ID              TapPay browser-side credentials (safe to expose;
  TAPPAY_APP_KEY              used by the page's TPDirect.setupSDK).
  TAPPAY_PARTNER_KEY         TapPay server-side secret. Never log or send to
  TAPPAY_MERCHANT_ID          a client. Billing stays off until both are set.
  TAPPAY_ENV                 "production" for live charges (default: sandbox).
  QUOTA_ENABLED              "false" to disable the monthly usage quota
                              (default "true").
  CONSOLE_URL                Where the browser lands after Google sign-in
                              succeeds (default "http://localhost:5175").
`

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			os.Stdout.WriteString(usage)
			return
		}
	}

	_ = godotenv.Load()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	dsn := envOr("DATABASE_URL", "postgres://platform:platform@localhost:5434/platform?sslmode=disable")
	gormDB, err := db.Open(dsn)
	if err != nil {
		log.Error("failed to open database", "err", err)
		os.Exit(1)
	}

	cookieSecure := os.Getenv("COOKIE_SECURE") == "true"
	if !cookieSecure {
		log.Warn(`COOKIE_SECURE not set to "true"; session cookie will be sent over plain HTTP (dev mode only)`)
	}

	sessionStore := session.New(gormDB, cookieSecure)
	businessStore := business.New(gormDB)
	var quotaSvc *quota.Service
	if envOr("QUOTA_ENABLED", "true") == "true" {
		quotaSvc = quota.New(gormDB)
	} else {
		log.Info("QUOTA_ENABLED=false: usage quota is not enforced")
	}
	consoleHandler := console.NewHandler(businessStore, sessionStore, quotaSvc, log)

	// TapPay billing. Needs the quota service (it moves owners between
	// tiers) and TapPay credentials; without either, /console/billing/*
	// still answers but reports billing as disabled.
	tpCfg := tappay.Config{
		AppID:      os.Getenv("TAPPAY_APP_ID"),
		AppKey:     os.Getenv("TAPPAY_APP_KEY"),
		PartnerKey: os.Getenv("TAPPAY_PARTNER_KEY"),
		MerchantID: os.Getenv("TAPPAY_MERCHANT_ID"),
		Production: os.Getenv("TAPPAY_ENV") == "production",
	}
	var billingSvc *billing.Service
	switch {
	case !tpCfg.Configured():
		log.Warn("TAPPAY_PARTNER_KEY / TAPPAY_MERCHANT_ID not set; billing is disabled")
	case quotaSvc == nil:
		log.Warn("QUOTA_ENABLED=false; billing is disabled (it needs the quota service)")
	default:
		billingSvc = billing.New(gormDB, tappay.New(tpCfg), quotaSvc, log)
		go billingSvc.Run(context.Background(), time.Hour)
		log.Info("TapPay billing enabled", "env", tpCfg.Env())
	}
	billingHandler := billing.NewHandler(billingSvc, sessionStore, tpCfg, log)

	siteOrigins := strings.Split(os.Getenv("ALLOWED_ORIGIN"), ",")
	if os.Getenv("ALLOWED_ORIGIN") == "" {
		siteOrigins = nil
		log.Warn("no ALLOWED_ORIGIN set; /console and /auth will reject every cross-origin request (dev mode only — set this before any real deployment)")
	}

	var googleAuthHandler *googleauth.Handler
	if clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID"); clientID != "" {
		googleAuthHandler = googleauth.New(
			clientID,
			os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
			os.Getenv("GOOGLE_OAUTH_REDIRECT_URL"),
			sessionStore,
			cookieSecure,
			envOr("CONSOLE_URL", "http://localhost:5175"),
			envOr("CONSOLE_URL", "http://localhost:5175")+"/login",
		)
		log.Info("Google sign-in enabled")
	}

	mux := http.NewServeMux()
	mountCredentialedRoutes(mux, multiRegistrar{consoleHandler, billingHandler}, allowlistChecker(siteOrigins), googleAuthHandler)
	if googleAuthHandler != nil {
		// Deliberately NOT behind corsMiddleware, same reasoning as
		// onagent's own main.go: these are top-level browser navigations
		// (redirects), not fetch() calls, so they carry no Origin header for
		// CORS to check in the first place.
		googleAuthHandler.RegisterRedirects(mux)
	}

	addr := envOr("ADDR", ":8082")
	log.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// routeRegistrar is what mountCredentialedRoutes needs from a handler —
// see onagent's own cmd/server/main.go for why this indirection exists
// (testability with a fake, without depending on a live Postgres).
type routeRegistrar interface {
	Register(mux *http.ServeMux)
}

// multiRegistrar registers several handlers onto the same mux.
type multiRegistrar []routeRegistrar

func (m multiRegistrar) Register(mux *http.ServeMux) {
	for _, r := range m {
		r.Register(mux)
	}
}

func mountCredentialedRoutes(mux *http.ServeMux, console routeRegistrar, siteOrigins func(string) bool, googleAuth *googleauth.Handler) {
	siteCORS := corsMiddleware(siteOrigins)

	consoleMux := http.NewServeMux()
	console.Register(consoleMux)
	if googleAuth != nil {
		googleAuth.RegisterConfig(consoleMux)
	}
	mux.Handle("/console/", siteCORS(consoleMux))
	mux.Handle("/auth/", siteCORS(consoleMux))
}

// corsMiddleware builds a CORS middleware bound to a single origin
// allowlist — see onagent's own cmd/server/main.go corsMiddleware for the
// full reasoning (only ever echoes back a matched origin, never "*", since
// this is a credentialed/cookie-bearing route group).
func corsMiddleware(allowed func(string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func allowlistChecker(allowed []string) func(string) bool {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[o] = true
	}
	return func(origin string) bool {
		return set[origin]
	}
}

func envOr(key, fallback string) string {
	return cmp.Or(os.Getenv(key), fallback)
}
