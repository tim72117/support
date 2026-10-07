//go:build integration

package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tim72117/ai-support/internal/adminauth"
	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/console"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

// HTTP-level tests of /admin/api/* against a real Postgres, following the
// same pattern as internal/console's own integration tests.
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/admin

var seq atomic.Int64

type server struct {
	t       *testing.T
	srv     *httptest.Server
	userIDs []int64
}

// newServer wires up both the admin API and the normal console/auth API
// (console.Handler) on the same mux, exactly like cmd/server/main.go's
// mountCredentialedRoutes does — a test client signs in through the real
// /auth/login, never a test-only shortcut, since that is the whole point
// being verified: admin-ness rides on the ordinary session.
func newServer(t *testing.T, adminEmails string) *server {
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
	businesses := business.New(gdb)
	q := quota.New(gdb)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	consoleHandler := console.NewHandler(businesses, sessions, q, log)
	adminHandler := NewHandler(sessions, adminauth.New(adminEmails), businesses, q, nil, log)

	mux := http.NewServeMux()
	consoleHandler.Register(mux)
	adminHandler.Register(mux)

	s := &server{t: t, srv: httptest.NewServer(mux)}
	t.Cleanup(func() {
		s.srv.Close()
		for _, id := range s.userIDs {
			gdb.Exec("DELETE FROM users WHERE id = ?", id)
		}
	})
	return s
}

type client struct {
	s *server
	c *http.Client
}

func (s *server) newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{s: s, c: &http.Client{Jar: jar}}
}

func uniqueEmail() string {
	return fmt.Sprintf("admin-test-%d-%d@example.com", time.Now().UnixNano(), seq.Add(1))
}

func (c *client) do(method, path string, body any) (int, string) {
	c.s.t.Helper()
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = strings.NewReader(string(buf))
	}
	req, _ := http.NewRequest(method, c.s.srv.URL+path, rd)
	res, err := c.c.Do(req)
	if err != nil {
		c.s.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(raw))
}

func (c *client) register(email string) int64 {
	c.s.t.Helper()
	code, body := c.do("POST", "/auth/register", map[string]string{"email": email, "password": "password123"})
	if code != 200 {
		c.s.t.Fatalf("register %s: %d %s", email, code, body)
	}
	var u struct{ ID int64 }
	_ = json.Unmarshal([]byte(body), &u)
	c.s.userIDs = append(c.s.userIDs, u.ID)
	return u.ID
}

// ---- /admin/api/me ----------------------------------------------------

func TestMeUnauthenticated(t *testing.T) {
	s := newServer(t, "")
	c := s.newClient()
	code, _ := c.do("GET", "/admin/api/me", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /admin/api/me: got %d, want 401", code)
	}
}

func TestMeNonAdmin(t *testing.T) {
	s := newServer(t, "someone-else@example.com")
	c := s.newClient()
	email := uniqueEmail()
	c.register(email)

	code, body := c.do("GET", "/admin/api/me", nil)
	if code != http.StatusOK {
		t.Fatalf("/admin/api/me for logged-in non-admin: got %d %s, want 200", code, body)
	}
	var resp meResponse
	_ = json.Unmarshal([]byte(body), &resp)
	if resp.IsAdmin {
		t.Fatalf("non-admin email reported as admin: %s", body)
	}
	if resp.Email != "" {
		t.Errorf("email must not be echoed back for a non-admin: %s", body)
	}
}

func TestMeAdmin(t *testing.T) {
	email := uniqueEmail()
	// ADMIN_EMAILS matching is case-insensitive and tolerates whitespace.
	s := newServer(t, " Other@example.com ,"+strings.ToUpper(email))
	c := s.newClient()
	c.register(email)

	code, body := c.do("GET", "/admin/api/me", nil)
	if code != http.StatusOK {
		t.Fatalf("/admin/api/me for admin: got %d %s", code, body)
	}
	var resp meResponse
	_ = json.Unmarshal([]byte(body), &resp)
	if !resp.IsAdmin {
		t.Fatalf("admin email not recognised as admin: %s", body)
	}
}

// ---- gated routes -------------------------------------------------------

func TestListUsersRequiresAdmin(t *testing.T) {
	email := uniqueEmail()
	s := newServer(t, email) // this one user is admin
	admin := s.newClient()
	admin.register(email)

	nonAdmin := s.newClient()
	nonAdmin.register(uniqueEmail())

	// Not logged in at all: 401.
	anon := s.newClient()
	if code, _ := anon.do("GET", "/admin/api/users", nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous /admin/api/users: got %d, want 401", code)
	}

	// Logged in, but not an admin: 404 (existence of the admin API must not
	// be revealed to a non-admin — see admin.go's package doc comment).
	if code, _ := nonAdmin.do("GET", "/admin/api/users", nil); code != http.StatusNotFound {
		t.Errorf("non-admin /admin/api/users: got %d, want 404", code)
	}

	// The admin account can list users, and sees at least the two accounts
	// just created.
	code, body := admin.do("GET", "/admin/api/users", nil)
	if code != http.StatusOK {
		t.Fatalf("admin /admin/api/users: got %d %s", code, body)
	}
	var users []ownerSummary
	if err := json.Unmarshal([]byte(body), &users); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	emails := map[string]bool{}
	for _, u := range users {
		emails[u.Email] = true
	}
	if !emails[email] {
		t.Errorf("admin's own account missing from list: %s", body)
	}
}

func TestListBusinessesRequiresAdmin(t *testing.T) {
	adminEmail := uniqueEmail()
	s := newServer(t, adminEmail)
	admin := s.newClient()
	admin.register(adminEmail)

	owner := s.newClient()
	owner.register(uniqueEmail())
	slug := fmt.Sprintf("biz-%d", time.Now().UnixNano())
	code, body := owner.do("POST", "/console/businesses", map[string]string{"slug": slug, "name": "Test Biz"})
	if code != http.StatusOK {
		t.Fatalf("create business: %d %s", code, body)
	}

	nonAdmin := s.newClient()
	nonAdmin.register(uniqueEmail())
	if code, _ := nonAdmin.do("GET", "/admin/api/businesses", nil); code != http.StatusNotFound {
		t.Errorf("non-admin /admin/api/businesses: got %d, want 404", code)
	}

	code, body = admin.do("GET", "/admin/api/businesses", nil)
	if code != http.StatusOK {
		t.Fatalf("admin /admin/api/businesses: got %d %s", code, body)
	}
	if !strings.Contains(body, slug) {
		t.Errorf("admin business list should include every owner's businesses, missing %q: %s", slug, body)
	}
}
