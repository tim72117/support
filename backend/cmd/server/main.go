// Command server runs the ai-support backend: business-owner accounts, the
// console API for managing businesses and their content, and the anonymous
// /public/* API behind each consumer chat page. It never talks to an LLM
// itself, and it never calls onagent's console API: the chat page forwards
// messages to onagent from the browser with @onagent/bridge, but only after
// this backend has recorded and quota-checked them. Every business's
// consumer page shares the one onagent app configured by ONAGENT_APP_ID/
// ONAGENT_APP_KEY below (provisioned once, by hand, with the onagent CLI —
// this backend has no write access to onagent's console API at all); the AI
// tells businesses apart itself via the businessSlug parameter on the
// list_sections/read_section tools (see backend/onagent-tools/*.yaml).
package main

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/tim72117/ai-support/internal/admin"
	"github.com/tim72117/ai-support/internal/adminauth"
	"github.com/tim72117/ai-support/internal/billing"
	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/console"
	"github.com/tim72117/ai-support/internal/conversation"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/googleauth"
	"github.com/tim72117/ai-support/internal/public"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/reservation"
	"github.com/tim72117/ai-support/internal/session"
	"github.com/tim72117/ai-support/internal/tappay"
)

const usage = `Usage: server [-h|--help]

Runs the ai-support backend: business-owner accounts and the console API.

Configured entirely via environment variables (optionally loaded from a
.env file in the working directory):

  ADDR                       Listen address (default ":8082"). Cloud Run
                              instead injects PORT (just the port number);
                              if ADDR is unset and PORT is set, this
                              listens on ":$PORT".
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
  PUBLIC_ALLOWED_ORIGIN      Comma-separated origins of the consumer chat page
                              (apps/support) allowed to call the anonymous
                              /public/* API (no credentials, never "*"). Also
                              the origins that must be registered as the
                              shared onagent app's allowed origins (with the
                              onagent CLI's "app origin set", run by hand —
                              this backend does not call onagent's console
                              API at all).
  PUBLIC_TRUST_PROXY         "true" when behind a reverse proxy: take the client
                              IP for rate limiting from X-Forwarded-For.
  ONAGENT_BASE_URL           onagent's HTTP origin, e.g.
                              "https://onagent.example.com". Used to derive the
                              default WebSocket URL handed to browsers (see
                              ONAGENT_WS_URL). This backend makes no HTTP calls
                              to onagent itself.
  ONAGENT_WS_URL             Optional: WebSocket URL handed to browsers
                              (default: ONAGENT_BASE_URL with ws(s):// and /ws).
  ONAGENT_APP_ID             The onagent app every business's consumer page
  ONAGENT_APP_KEY             shares, and its browser-side API key. Both are
                              provisioned once, by hand, with the onagent CLI
                              (see backend/onagent-tools/*.yaml for the tool
                              definitions pushed to that one app) — this
                              backend never creates an app or issues a key.
                              The browser tells the AI which business it is
                              talking to via the list_sections/read_section
                              tools' businessSlug parameter, not via the app.
                              Unset means chat reports unavailable.
  ADMIN_EMAILS               Comma-separated emails allowed to use the
                              platform-admin back office (/admin/api/*,
                              apps/admin). Not a separate account system: an
                              admin signs in through the normal /auth/login
                              like any business owner, and this just checks
                              whether that account's email is in the list.
                              Unset or empty disables the admin API for
                              everyone (every /admin/api/* route 404s).
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
	chatStore := conversation.New(gormDB)
	consoleHandler.Chats = chatStore

	publicOrigins := splitOrigins(os.Getenv("PUBLIC_ALLOWED_ORIGIN"))
	if len(publicOrigins) == 0 {
		log.Warn("no PUBLIC_ALLOWED_ORIGIN set; browsers on other origins cannot call /public/* (dev mode only — the shared onagent app's allowed origins are set separately, by hand, with the onagent CLI)")
	}

	// onagent integration: this backend never calls onagent's console API —
	// every business's consumer page shares the one onagent app set up by
	// hand with the onagent CLI. Chat is off (log one line, keep starting)
	// unless the app id, its key, and a WS URL are all available.
	onagentAppID := os.Getenv("ONAGENT_APP_ID")
	onagentAppKey := os.Getenv("ONAGENT_APP_KEY")
	onagentWSURL := os.Getenv("ONAGENT_WS_URL")
	if onagentWSURL == "" {
		onagentWSURL = deriveWSURL(os.Getenv("ONAGENT_BASE_URL"))
	}
	if onagentAppID != "" && onagentAppKey != "" && onagentWSURL != "" {
		log.Info("onagent integration enabled", "ws", onagentWSURL, "appId", onagentAppID)
	} else {
		log.Warn("ONAGENT_APP_ID / ONAGENT_APP_KEY / ONAGENT_WS_URL (or ONAGENT_BASE_URL) not all set; onagent integration is disabled (public chat reports unavailable)")
		onagentAppID, onagentAppKey, onagentWSURL = "", "", ""
	}
	publicHandler := public.NewHandler(public.Config{
		Businesses:    businessStore,
		Chats:         chatStore,
		Quota:         quotaSvc,
		Reservations:  reservation.New(gormDB),
		OnagentWSURL:  onagentWSURL,
		OnagentAppID:  onagentAppID,
		OnagentAppKey: onagentAppKey,
		TrustProxy:    os.Getenv("PUBLIC_TRUST_PROXY") == "true",
		Log:           log,
	})

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

	// Platform-admin back office. See adminauth's package doc comment for
	// why this is deliberately not a separate account/login system: an
	// admin is just a normal session (internal/session) whose email is in
	// ADMIN_EMAILS. Mounted unconditionally (an empty allowlist just means
	// nobody's email ever matches, so every /admin/api/* route 404s — see
	// admin.Handler.withAdmin) rather than only when the env var is set, so
	// there's no separate "is the admin API compiled in" branch to reason
	// about in addition to "who's on the list".
	adminAllowlist := adminauth.New(os.Getenv("ADMIN_EMAILS"))
	if adminAllowlist.Empty() {
		log.Warn("no ADMIN_EMAILS set; the admin back office is mounted but nobody can use it")
	}
	adminHandler := admin.NewHandler(sessionStore, adminAllowlist, businessStore, quotaSvc, billingSvc, log)

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
	mountCredentialedRoutes(mux, multiRegistrar{consoleHandler, billingHandler, adminHandler}, allowlistChecker(siteOrigins), googleAuthHandler)
	mountPublicRoutes(mux, publicHandler, allowlistChecker(publicOrigins))
	if googleAuthHandler != nil {
		// Deliberately NOT behind corsMiddleware, same reasoning as
		// onagent's own main.go: these are top-level browser navigations
		// (redirects), not fetch() calls, so they carry no Origin header for
		// CORS to check in the first place.
		googleAuthHandler.RegisterRedirects(mux)
	}

	// Embedded frontend static builds (see static_landing.go,
	// static_console.go, static_support.go, static_admin.go — all four npm
	// projects are built into this binary by backend/Dockerfile*, no
	// separate frontend deploys). Mounted under their own exact prefixes so
	// http.ServeMux's most-specific-match-wins routing picks these over
	// the catch-all landing handler at "/" below; none of these prefixes
	// collide with the API's "/console/", "/auth/" or "/public/" (the
	// owner console SPA is deliberately NOT at "/console/" — see
	// static_console.go for why it's "/app/" instead). "/admin/" (the
	// frontend) and "/admin/api/" (the API, registered above via
	// mountCredentialedRoutes) coexist the same way "/app/" and "/console/"
	// do: ServeMux's longest-prefix-match sends /admin/api/* to the API
	// mux and everything else under /admin/ to this static handler.
	//
	// *apps/admin is NOT currently added to backend/Dockerfile's build
	// stages — see static_admin.go's doc comment.
	mux.Handle("/app/", consoleStaticHandler())
	mux.Handle("/support/", supportStaticHandler())
	mux.Handle("/admin/", adminStaticHandler())
	mux.Handle("/", landingStaticHandler())

	// Cloud Run injects PORT (just the port number, e.g. "8080"), not ADDR —
	// see https://cloud.google.com/run/docs/container-contract#port. Fall
	// back to it only when ADDR itself isn't set, so any explicit ADDR
	// (e.g. local dev's ":8082") still wins.
	addr := os.Getenv("ADDR")
	if addr == "" {
		if port := os.Getenv("PORT"); port != "" {
			addr = ":" + port
		} else {
			addr = ":8082"
		}
	}
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
	// /admin/api/* (internal/admin) shares the same session cookie and
	// origin allowlist as /console/ and /auth/ — it's the same kind of
	// credentialed, cookie-based browser API, just gated by an extra
	// allowlist check on top (see adminauth). No separate CORS policy or
	// cookie needed for it.
	mux.Handle("/admin/api/", siteCORS(consoleMux))
}

// mountPublicRoutes mounts the anonymous consumer-page API under /public/ with
// its own CORS policy: separate allowlist, no credentials. Kept apart from
// the credentialed /console/ and /auth/ group on purpose.
func mountPublicRoutes(mux *http.ServeMux, h routeRegistrar, origins func(string) bool) {
	publicMux := http.NewServeMux()
	h.Register(publicMux)
	mux.Handle("/public/", public.CORS(origins)(publicMux))
}

// splitOrigins parses a comma-separated origin list, dropping blanks.
func splitOrigins(raw string) []string {
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
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

// deriveWSURL turns onagent's HTTP origin into its WebSocket endpoint
// (onagent serves it at /ws — see its own cmd/server/main.go), e.g.
// "https://onagent.example.com" -> "wss://onagent.example.com/ws". Empty in,
// empty out. ONAGENT_WS_URL overrides this when set, for a deployment where
// the two differ (e.g. a WS-specific load balancer).
func deriveWSURL(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	return u.String()
}
