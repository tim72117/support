//go:build integration

package session

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/db"
)

// Store-level tests. The Google OAuth network exchange itself is not
// reachable from here (see internal/googleauth's tests); what is tested is
// the account resolution that runs after Google has vouched for an identity.
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/session

func newStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return New(gdb, false), gdb
}

func email() string { return fmt.Sprintf("session-test-%d@example.com", time.Now().UnixNano()) }

func cleanup(t *testing.T, gdb *gorm.DB, ids ...int64) {
	t.Cleanup(func() {
		for _, id := range ids {
			gdb.Exec("DELETE FROM users WHERE id = ?", id)
		}
	})
}

func subscriptionTier(t *testing.T, gdb *gorm.DB, userID int64) (string, int64) {
	t.Helper()
	var rows []struct{ Tier string }
	gdb.Table("subscriptions").Select("tier").Where("user_id = ?", userID).Scan(&rows)
	if len(rows) == 0 {
		return "", 0
	}
	return rows[0].Tier, int64(len(rows))
}

func TestGoogleSignInCreatesAccountOnce(t *testing.T) {
	s, gdb := newStore(t)
	mail, sub := email(), fmt.Sprintf("g-%d", time.Now().UnixNano())

	u, created, err := s.LoginOrCreateWithGoogle(sub, mail)
	if err != nil || !created || u.Email != mail {
		t.Fatalf("first sign-in: %+v created=%v err=%v", u, created, err)
	}
	cleanup(t, gdb, u.ID)
	if tier, n := subscriptionTier(t, gdb, u.ID); tier != "free" || n != 1 {
		t.Errorf("a Google signup must also get a free subscription row, got tier=%q rows=%d", tier, n)
	}

	// Same Google subject again, even if the email on the token changed.
	u2, created2, err := s.LoginOrCreateWithGoogle(sub, "changed-"+mail)
	if err != nil || created2 || u2.ID != u.ID {
		t.Errorf("returning sign-in: %+v created=%v err=%v; want the same account, created=false", u2, created2, err)
	}
}

func TestGoogleSignInLinksExistingEmailAccount(t *testing.T) {
	s, gdb := newStore(t)
	mail := email()
	existing, err := s.Register(mail, "password123")
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, gdb, existing.ID)

	u, created, err := s.LoginOrCreateWithGoogle("g-link-"+mail, mail)
	if err != nil || created || u.ID != existing.ID {
		t.Fatalf("link: %+v created=%v err=%v; want the existing account", u, created, err)
	}
	// The password keeps working after linking.
	if _, err := s.Login(mail, "password123"); err != nil {
		t.Errorf("password login after linking: %v", err)
	}
	// Different casing of the email is still the same account.
	u2, created2, err := s.LoginOrCreateWithGoogle("g-link2-"+mail, "  "+upper(mail)+"  ")
	if err != nil || created2 || u2.ID != existing.ID {
		t.Errorf("case-insensitive match: %+v created=%v err=%v", u2, created2, err)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func TestGoogleSignInRejectsBadInput(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.LoginOrCreateWithGoogle("", email()); err == nil {
		t.Error("empty subject id must be rejected")
	}
	if _, _, err := s.LoginOrCreateWithGoogle("g-x", "not-an-email"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad email err = %v, want ErrInvalidEmail", err)
	}
}

func TestRegisterAndLoginRules(t *testing.T) {
	s, gdb := newStore(t)
	mail := email()

	if _, err := s.Register(mail, "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password err = %v", err)
	}
	if _, err := s.Register("bad-email", "password123"); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad email err = %v", err)
	}
	u, err := s.Register(mail, "password123")
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, gdb, u.ID)
	if _, err := s.Register(mail, "password123"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate err = %v", err)
	}
	if tier, n := subscriptionTier(t, gdb, u.ID); tier != "free" || n != 1 {
		t.Errorf("signup must create a free subscription, got %q (%d rows)", tier, n)
	}

	var hash *string
	gdb.Table("users").Select("password_hash").Where("id = ?", u.ID).Row().Scan(&hash)
	if hash == nil || *hash == "password123" || len(*hash) < 40 {
		t.Errorf("password must be stored as a bcrypt hash, got %v", hash)
	}

	if _, err := s.Login(mail, "password123"); err != nil {
		t.Errorf("login: %v", err)
	}
	for _, bad := range []string{"wrong-password", "", "password1234"} {
		if _, err := s.Login(mail, bad); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("login with %q err = %v, want ErrInvalidCredentials", bad, err)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	s, gdb := newStore(t)
	u, err := s.Register(email(), "password123")
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, gdb, u.ID)

	for _, secure := range []bool{false, true} {
		s.Secure = secure
		rec := httptest.NewRecorder()
		if _, err := s.CreateSession(rec, u.ID); err != nil {
			t.Fatal(err)
		}
		ck := rec.Result().Cookies()[0]
		if !ck.HttpOnly || ck.Secure != secure {
			t.Errorf("secure=%v: cookie %+v", secure, ck)
		}
		// SameSite=None is silently dropped by browsers unless Secure is set.
		if secure && ck.SameSite != http.SameSiteNoneMode || !secure && ck.SameSite != http.SameSiteLaxMode {
			t.Errorf("secure=%v: SameSite=%v", secure, ck.SameSite)
		}
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(ck)
		if got, ok := s.Verify(req); !ok || got.ID != u.ID {
			t.Errorf("verify = %+v %v", got, ok)
		}
	}
}
