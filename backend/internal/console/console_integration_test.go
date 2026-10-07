//go:build integration

package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

// HTTP-level tests of the owner API (register / login / session / businesses
// CRUD / quota) against a real Postgres. There is no outbound network call
// anywhere on these paths, so nothing needs mocking beyond the HTTP layer
// itself (httptest).
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/console

var seq atomic.Int64

type server struct {
	t        *testing.T
	srv      *httptest.Server
	gdb      *gorm.DB
	sessions *session.Store
	userIDs  []int64
}

func newServer(t *testing.T, withQuota bool) *server {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(gdb, false)
	var q *quota.Service
	if withQuota {
		q = quota.New(gdb)
	}
	h := NewHandler(business.New(gdb), sessions, q, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.Register(mux)
	s := &server{t: t, srv: httptest.NewServer(mux), gdb: gdb, sessions: sessions}
	t.Cleanup(func() {
		s.srv.Close()
		for _, id := range s.userIDs {
			gdb.Exec("DELETE FROM users WHERE id = ?", id) // cascades to businesses, sessions, ...
		}
	})
	return s
}

// client is one browser: its own cookie jar.
type client struct {
	s *server
	c *http.Client
}

func (s *server) newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{s: s, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func uniqueEmail() string {
	return fmt.Sprintf("console-test-%d-%d@example.com", time.Now().UnixNano(), seq.Add(1))
}

func (c *client) do(method, path string, body any) (int, string, *http.Response) {
	c.s.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			buf, _ := json.Marshal(b)
			rd = strings.NewReader(string(buf))
		}
	}
	req, _ := http.NewRequest(method, c.s.srv.URL+path, rd)
	res, err := c.c.Do(req)
	if err != nil {
		c.s.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(raw)), res
}

// register creates an account through the API and returns its id.
func (c *client) register(email string) int64 {
	c.s.t.Helper()
	code, body, _ := c.do("POST", "/auth/register", map[string]string{"email": email, "password": "password123"})
	if code != 200 {
		c.s.t.Fatalf("register %s: %d %s", email, code, body)
	}
	var u struct{ ID int64 }
	_ = json.Unmarshal([]byte(body), &u)
	c.s.userIDs = append(c.s.userIDs, u.ID)
	return u.ID
}

// ---- register -------------------------------------------------------------

func TestRegister(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	email := uniqueEmail()

	code, body, res := c.do("POST", "/auth/register", map[string]string{"email": "  " + email + "  ", "password": "password123"})
	if code != 200 {
		t.Fatalf("register: %d %s", code, body)
	}
	var u struct {
		ID    int64
		Email string
	}
	_ = json.Unmarshal([]byte(body), &u)
	s.userIDs = append(s.userIDs, u.ID)
	if u.Email != email {
		t.Errorf("email should be stored trimmed: got %q", u.Email)
	}
	if strings.Contains(body, "password") || strings.Contains(strings.ToLower(body), "hash") {
		t.Errorf("response must not expose credentials: %s", body)
	}
	var cookie *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == session.CookieName {
			cookie = ck
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("register must start an HttpOnly session: %+v", cookie)
	}

	// New accounts start on the free tier, with a subscription row.
	st, err := quota.New(s.gdb).StandingFor(context.Background(), u.ID)
	if err != nil || st.Tier != quota.TierFree {
		t.Errorf("standing = %+v, err %v; want free tier", st, err)
	}
	var n int64
	s.gdb.Table("subscriptions").Where("user_id = ?", u.ID).Count(&n)
	if n != 1 {
		t.Errorf("subscription rows = %d, want 1", n)
	}

	// Registering logged us in.
	if code, _, _ := c.do("GET", "/auth/me", nil); code != 200 {
		t.Errorf("/auth/me after register = %d", code)
	}
}

func TestRegisterRejections(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	email := uniqueEmail()
	c.register(email)

	cases := []struct {
		name string
		body any
		want int
	}{
		{"duplicate email", map[string]string{"email": email, "password": "password123"}, 400},
		{"duplicate email, different case", map[string]string{"email": strings.ToUpper(email), "password": "password123"}, 400},
		{"not an email", map[string]string{"email": "nope", "password": "password123"}, 400},
		{"empty email", map[string]string{"email": "", "password": "password123"}, 400},
		{"short password", map[string]string{"email": uniqueEmail(), "password": "short"}, 400},
		{"malformed json", "{not json", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := s.newClient()
			code, body, _ := other.do("POST", "/auth/register", tc.body)
			if code != tc.want {
				t.Fatalf("status %d (%s), want %d", code, body, tc.want)
			}
			if code, _, _ := other.do("GET", "/auth/me", nil); code != 401 {
				t.Errorf("a rejected registration must not start a session (/auth/me = %d)", code)
			}
		})
	}
}

// ---- login / session lifecycle -------------------------------------------

func TestLogin(t *testing.T) {
	s := newServer(t, true)
	email := uniqueEmail()
	s.newClient().register(email)

	c := s.newClient()
	code, body, _ := c.do("POST", "/auth/login", map[string]string{"email": email, "password": "password123"})
	if code != 200 || !strings.Contains(body, email) {
		t.Fatalf("login: %d %s", code, body)
	}
	if code, _, _ := c.do("GET", "/auth/me", nil); code != 200 {
		t.Errorf("/auth/me after login = %d", code)
	}

	// Case-insensitive email match.
	if code, _, _ := s.newClient().do("POST", "/auth/login", map[string]string{"email": strings.ToUpper(email), "password": "password123"}); code != 200 {
		t.Errorf("login with different email case = %d", code)
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	s := newServer(t, true)
	email := uniqueEmail()
	s.newClient().register(email)

	// A Google-only account has no password at all.
	googleEmail := uniqueEmail()
	gu, _, err := s.sessions.LoginOrCreateWithGoogle("google-sub-"+googleEmail, googleEmail)
	if err != nil {
		t.Fatal(err)
	}
	s.userIDs = append(s.userIDs, gu.ID)

	attempts := map[string]map[string]string{
		"wrong password":      {"email": email, "password": "wrong-password"},
		"unknown email":       {"email": uniqueEmail(), "password": "password123"},
		"google-only account": {"email": googleEmail, "password": "password123"},
		"empty password":      {"email": email, "password": ""},
	}
	var bodies []string
	for name, a := range attempts {
		c := s.newClient()
		code, body, _ := c.do("POST", "/auth/login", a)
		if code != 401 {
			t.Errorf("%s: status %d, want 401", name, code)
		}
		bodies = append(bodies, body)
		if code, _, _ := c.do("GET", "/auth/me", nil); code != 401 {
			t.Errorf("%s: a failed login must not start a session", name)
		}
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("failure bodies differ (%q vs %q): that would reveal whether an email is registered", b, bodies[0])
		}
	}
	if code, _, _ := s.newClient().do("POST", "/auth/login", "{bad"); code != 400 {
		t.Errorf("malformed login body = %d, want 400", code)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()

	if code, _, _ := c.do("GET", "/auth/me", nil); code != 401 {
		t.Fatalf("/auth/me without a session = %d, want 401", code)
	}
	email := uniqueEmail()
	c.register(email)
	code, body, _ := c.do("GET", "/auth/me", nil)
	if code != 200 || !strings.Contains(body, email) {
		t.Fatalf("/auth/me = %d %s", code, body)
	}

	// Logout invalidates the session server-side, not just in the browser.
	u := mustURL(t, s.srv.URL)
	saved := c.c.Jar.Cookies(u)
	if code, _, _ := c.do("POST", "/auth/logout", nil); code != 204 {
		t.Errorf("logout = %d, want 204", code)
	}
	if code, _, _ := c.do("GET", "/auth/me", nil); code != 401 {
		t.Errorf("/auth/me after logout = %d, want 401", code)
	}
	replay := s.newClient()
	replay.c.Jar.SetCookies(u, saved)
	if code, _, _ := replay.do("GET", "/auth/me", nil); code != 401 {
		t.Errorf("replaying the old cookie after logout = %d, want 401 (session must be deleted server-side)", code)
	}

	// Garbage cookie.
	bogus := s.newClient()
	bogus.c.Jar.SetCookies(u, []*http.Cookie{{Name: session.CookieName, Value: "not-a-real-session", Path: "/"}})
	if code, _, _ := bogus.do("GET", "/auth/me", nil); code != 401 {
		t.Errorf("bogus cookie = %d, want 401", code)
	}

	// Expired session.
	exp := s.newClient()
	expID := exp.register(uniqueEmail())
	s.gdb.Exec("UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE user_id = ?", expID)
	if code, _, _ := exp.do("GET", "/auth/me", nil); code != 401 {
		t.Errorf("expired session = %d, want 401", code)
	}
}

// ---- businesses CRUD ------------------------------------------------------

type biz struct {
	ID      int64
	OwnerID int64
	Slug    string
	Name    string
}

func (c *client) createBusiness(slug, name string) (int, biz, string) {
	code, body, _ := c.do("POST", "/console/businesses", map[string]string{"slug": slug, "name": name})
	var b biz
	_ = json.Unmarshal([]byte(body), &b)
	return code, b, body
}

func uniqueSlug() string { return fmt.Sprintf("shop-%d-%d", time.Now().UnixNano(), seq.Add(1)) }

func TestBusinessRoutesRequireLogin(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	for _, r := range []struct{ method, path string }{
		{"GET", "/console/businesses"},
		{"POST", "/console/businesses"},
		{"GET", "/console/businesses/1"},
		{"DELETE", "/console/businesses/1"},
		{"GET", "/console/businesses/1/content"},
		{"PUT", "/console/businesses/1/content"},
		{"GET", "/console/quota"},
	} {
		if code, _, _ := c.do(r.method, r.path, "{}"); code != 401 {
			t.Errorf("%s %s without login = %d, want 401", r.method, r.path, code)
		}
	}
}

func TestBusinessCRUD(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	owner := c.register(uniqueEmail())

	// Create
	slug := uniqueSlug()
	code, b, body := c.createBusiness(slug, "晨光麵包坊")
	if code != 200 || b.ID == 0 || b.Slug != slug || b.Name != "晨光麵包坊" || b.OwnerID != owner {
		t.Fatalf("create: %d %s", code, body)
	}
	path := fmt.Sprintf("/console/businesses/%d", b.ID)

	// Read one / list
	if code, body, _ := c.do("GET", path, nil); code != 200 || !strings.Contains(body, slug) {
		t.Errorf("get: %d %s", code, body)
	}
	_, listBody, _ := c.do("GET", "/console/businesses", nil)
	var list []biz
	_ = json.Unmarshal([]byte(listBody), &list)
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("list = %s", listBody)
	}

	// Content: starts empty, can be replaced, persists.
	code, body, _ = c.do("GET", path+"/content", nil)
	if code != 200 || !strings.Contains(body, `"Content":""`) {
		t.Errorf("fresh content: %d %s", code, body)
	}
	if code, _, _ := c.do("PUT", path+"/content", map[string]string{"content": "營業時間 7:00–19:00"}); code != 204 {
		t.Errorf("put content = %d, want 204", code)
	}
	if code, _, _ := c.do("PUT", path+"/content", map[string]string{"content": "改過的內容"}); code != 204 {
		t.Errorf("second put = %d", code)
	}
	if _, body, _ := c.do("GET", path+"/content", nil); !strings.Contains(body, "改過的內容") || strings.Contains(body, "7:00") {
		t.Errorf("content should be replaced, got %s", body)
	}
	if code, _, _ := c.do("PUT", path+"/content", "{bad"); code != 400 {
		t.Errorf("malformed content body = %d, want 400", code)
	}

	// Delete: gone, and its content with it.
	if code, _, _ := c.do("DELETE", path, nil); code != 204 {
		t.Errorf("delete = %d, want 204", code)
	}
	for _, p := range []string{path, path + "/content"} {
		if code, _, _ := c.do("GET", p, nil); code != 404 {
			t.Errorf("GET %s after delete = %d, want 404", p, code)
		}
	}
	var n int64
	s.gdb.Table("business_content").Where("business_id = ?", b.ID).Count(&n)
	if n != 0 {
		t.Errorf("content row should be deleted with the business, %d left", n)
	}
	if code, _, _ := c.do("DELETE", path, nil); code != 404 {
		t.Errorf("deleting twice = %d, want 404", code)
	}
}

func TestBusinessValidation(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())

	slug := uniqueSlug()
	if code, _, body := c.createBusiness(slug, "A"); code != 200 {
		t.Fatalf("setup: %d %s", code, body)
	}
	// Another owner cannot take the same URL.
	other := s.newClient()
	other.register(uniqueEmail())
	if code, _, _ := other.createBusiness(slug, "B"); code != 409 {
		t.Errorf("duplicate slug = %d, want 409", code)
	}

	// Bad input is the caller's mistake (400), never a server error.
	for name, in := range map[string][2]string{
		"uppercase slug":  {"Has-Upper", "ok"},
		"space in slug":   {"has space", "ok"},
		"leading hyphen":  {"-lead", "ok"},
		"empty slug":      {"", "ok"},
		"slash in slug":   {"a/b", "ok"},
		"empty name":      {uniqueSlug(), ""},
		"whitespace name": {uniqueSlug(), "   "},
	} {
		if code, _, body := c.createBusiness(in[0], in[1]); code != 400 {
			t.Errorf("%s: status %d (%s), want 400", name, code, body)
		}
	}
	if code, _, _ := c.do("POST", "/console/businesses", "{bad"); code != 400 {
		t.Errorf("malformed body = %d, want 400", code)
	}
}

func TestBusinessOwnershipIsolation(t *testing.T) {
	s := newServer(t, true)
	alice, mallory := s.newClient(), s.newClient()
	alice.register(uniqueEmail())
	mallory.register(uniqueEmail())

	_, b, _ := alice.createBusiness(uniqueSlug(), "Alice 的店")
	alice.do("PUT", fmt.Sprintf("/console/businesses/%d/content", b.ID), map[string]string{"content": "secret"})
	path := fmt.Sprintf("/console/businesses/%d", b.ID)

	// Someone else's id must look exactly like an id that does not exist.
	_, missingBody, _ := mallory.do("GET", "/console/businesses/999999999", nil)
	for _, r := range []struct{ method, path string }{
		{"GET", path}, {"DELETE", path}, {"GET", path + "/content"}, {"PUT", path + "/content"},
	} {
		code, body, _ := mallory.do(r.method, r.path, map[string]string{"content": "pwned"})
		if code != 404 {
			t.Errorf("%s %s as another user = %d, want 404 (not 403)", r.method, r.path, code)
		}
		if body != missingBody {
			t.Errorf("%s %s: body %q differs from a nonexistent id's %q", r.method, r.path, body, missingBody)
		}
	}
	if _, body, _ := mallory.do("GET", "/console/businesses", nil); strings.Contains(body, "Alice") {
		t.Errorf("list leaked another owner's business: %s", body)
	}
	if code, _, _ := mallory.do("GET", "/console/businesses/not-a-number", nil); code != 404 {
		t.Errorf("non-numeric id = %d, want 404", code)
	}

	// Untouched by the failed attempts.
	if _, body, _ := alice.do("GET", path+"/content", nil); !strings.Contains(body, "secret") {
		t.Errorf("content was modified by a non-owner: %s", body)
	}
	if code, _, _ := alice.do("GET", path, nil); code != 200 {
		t.Errorf("owner lost access: %d", code)
	}
}

// ---- quota endpoint -------------------------------------------------------

func TestQuotaEndpoint(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	owner := c.register(uniqueEmail())
	_, b, _ := c.createBusiness(uniqueSlug(), "Q")

	_, body, _ := c.do("GET", "/console/quota", nil)
	var q struct {
		Enabled     bool
		Tier        string
		Limit, Used int
		UsedPercent int
	}
	_ = json.Unmarshal([]byte(body), &q)
	if !q.Enabled || q.Tier != "free" || q.Used != 0 || q.Limit != quota.FreePlan.MonthlyTokens {
		t.Fatalf("fresh quota = %s", body)
	}

	if err := quota.New(s.gdb).Record(context.Background(), b.ID, owner, "evt", &quota.Usage{TotalTokens: q.Limit / 2}); err != nil {
		t.Fatal(err)
	}
	_, body, _ = c.do("GET", "/console/quota", nil)
	_ = json.Unmarshal([]byte(body), &q)
	if q.Used != q.Limit/2 || q.UsedPercent != 50 {
		t.Errorf("after recording half the allowance: %s", body)
	}
}

func TestQuotaDisabledReportsEnabledFalse(t *testing.T) {
	s := newServer(t, false)
	c := s.newClient()
	c.register(uniqueEmail())
	code, body, _ := c.do("GET", "/console/quota", nil)
	if code != 200 || !strings.Contains(body, `"enabled":false`) {
		t.Errorf("disabled quota = %d %s, want 200 enabled:false (not an error)", code, body)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
