package googleauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/tim72117/ai-support/internal/session"
)

// The browser-redirect OAuth flow, with Google replaced by a local fake token
// endpoint. Not covered (needs Google's real signing keys, and there is no
// hook to inject them): the success path through idtoken.Validate.
// Everything that decides whether a callback may even get that far is covered
// here, and the account resolution after it is covered in internal/session.

const (
	success = "http://console.test/"
	failure = "http://console.test/login"
)

func newHandler(secure bool) (*Handler, *http.ServeMux) {
	h := New("client-id.apps.googleusercontent.com", "client-secret", "http://api.test/auth/google/callback",
		session.New(nil, secure), secure, success, failure)
	mux := http.NewServeMux()
	h.RegisterConfig(mux)
	h.RegisterRedirects(mux)
	return h, mux
}

func get(mux *http.ServeMux, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func fakeTokenEndpoint(t *testing.T, h *Handler, status int, body string) {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fake.Close)
	h.oauthConfig.Endpoint = oauth2.Endpoint{TokenURL: fake.URL}
}

func TestConfigAdvertisesGoogleSignIn(t *testing.T) {
	_, mux := newHandler(false)
	rec := get(mux, "/auth/config")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"googleSignIn":true`) {
		t.Errorf("config = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStartRedirectsToGoogleWithBoundState(t *testing.T) {
	for _, secure := range []bool{false, true} {
		_, mux := newHandler(secure)
		rec := get(mux, "/auth/google/start")
		if rec.Code != http.StatusFound {
			t.Fatalf("start = %d", rec.Code)
		}
		loc, _ := url.Parse(rec.Header().Get("Location"))
		if loc.Host != "accounts.google.com" {
			t.Errorf("redirects to %s, want Google", loc.Host)
		}
		q := loc.Query()
		if q.Get("client_id") != "client-id.apps.googleusercontent.com" ||
			q.Get("redirect_uri") != "http://api.test/auth/google/callback" ||
			q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), "email") {
			t.Errorf("authorize URL params = %v", q)
		}
		state := cookie(rec, stateCookieName)
		if state == nil || state.Value == "" || state.Value != q.Get("state") {
			t.Fatalf("state cookie %+v must equal the state sent to Google (%q)", state, q.Get("state"))
		}
		if !state.HttpOnly || state.Secure != secure || state.Path != "/auth/google" {
			t.Errorf("state cookie attributes: %+v", state)
		}
	}
	// A fresh state every time.
	_, mux := newHandler(false)
	a, b := cookie(get(mux, "/auth/google/start"), stateCookieName), cookie(get(mux, "/auth/google/start"), stateCookieName)
	if a.Value == b.Value {
		t.Error("state must be unguessable and unique per attempt")
	}
}

func TestCallbackRejectsBeforeTalkingToGoogle(t *testing.T) {
	h, mux := newHandler(false)
	// Any outbound token exchange would reach this and fail the test.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("token endpoint must not be called, got %s", r.URL)
	}))
	defer fake.Close()
	h.oauthConfig.Endpoint = oauth2.Endpoint{TokenURL: fake.URL}

	st := &http.Cookie{Name: stateCookieName, Value: "good-state"}
	cases := []struct {
		name    string
		path    string
		cookies []*http.Cookie
		reason  string
	}{
		{"no state cookie", "/auth/google/callback?state=x&code=c", nil, "missing_state"},
		{"state mismatch (login CSRF)", "/auth/google/callback?state=attacker&code=c", []*http.Cookie{st}, "state_mismatch"},
		{"user declined consent", "/auth/google/callback?state=good-state&error=access_denied", []*http.Cookie{st}, "google_access_denied"},
		{"no code", "/auth/google/callback?state=good-state", []*http.Cookie{st}, "missing_code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(mux, tc.path, tc.cookies...)
			if rec.Code != http.StatusFound {
				t.Fatalf("status %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != failure+"?error="+tc.reason {
				t.Errorf("redirect = %q, want %s?error=%s", loc, failure, tc.reason)
			}
			if cookie(rec, session.CookieName) != nil {
				t.Error("a rejected callback must never start a session")
			}
			if len(tc.cookies) > 0 {
				if c := cookie(rec, stateCookieName); c == nil || c.MaxAge >= 0 {
					t.Errorf("the one-time state cookie must be cleared, got %+v", c)
				}
			}
		})
	}
}

func TestCallbackSurfacesGoogleSideFailures(t *testing.T) {
	st := &http.Cookie{Name: stateCookieName, Value: "good-state"}
	cases := []struct {
		name   string
		status int
		body   string
		reason string
	}{
		{"token exchange refused", 400, `{"error":"invalid_grant"}`, "exchange_failed"},
		{"exchange ok but no id_token", 200, `{"access_token":"at","token_type":"Bearer","expires_in":3600}`, "no_id_token"},
		{"forged id_token", 200, `{"access_token":"at","token_type":"Bearer","id_token":"not.a.jwt"}`, "invalid_id_token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, mux := newHandler(false)
			fakeTokenEndpoint(t, h, tc.status, tc.body)
			rec := get(mux, "/auth/google/callback?state=good-state&code=abc", st)
			if loc := rec.Header().Get("Location"); loc != failure+"?error="+tc.reason {
				t.Errorf("redirect = %q, want ?error=%s", loc, tc.reason)
			}
			if cookie(rec, session.CookieName) != nil {
				t.Error("nothing short of a verified id_token may log anyone in")
			}
		})
	}
}
