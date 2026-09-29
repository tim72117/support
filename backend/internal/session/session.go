// Package session implements email/password accounts and database-backed
// login sessions for the console API (internal/console) — this is the
// business-owner login, not the onagent API key each business uses to
// authenticate its own consumer-facing chat widget (that key lives on the
// businesses row and is handed to the browser SDK directly, never checked
// by this package).
package session

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CookieName is the cookie the console API's session id travels in. httpOnly
// so page JavaScript can't read it (XSS can't exfiltrate the session even
// if it can run arbitrary code), Secure in production (see Store.Secure).
const CookieName = "ai_support_session"

const sessionTTL = 30 * 24 * time.Hour

var emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ErrInvalidCredentials covers both "no such user" and "wrong password"
// without distinguishing them in the response — telling a caller which one
// failed lets them enumerate registered emails.
var ErrInvalidCredentials = errors.New("invalid email or password")

// ErrEmailTaken is returned by Register when the email is already in use
// (case-insensitively — see the schema's users_email_lower_idx).
var ErrEmailTaken = errors.New("an account with this email already exists")

// ErrInvalidEmail is returned by Register and LoginOrCreateWithGoogle when
// the email fails emailRE's format check.
var ErrInvalidEmail = errors.New("invalid email address")

// User is the caller-facing shape of an authenticated account. Never
// includes the password hash.
type User struct {
	ID    int64
	Email string
}

// userRow/identityRow/sessionRow are the GORM-mapped shapes of their
// respective tables (see internal/db/schema.sql for the authoritative
// column definitions — schema management stays there, not AutoMigrate).
//
// PasswordHash is a pointer because it's nullable: an account created via
// Google sign-in has no password until (if ever) the user sets one.
type userRow struct {
	ID           int64   `gorm:"column:id;primaryKey"`
	Email        string  `gorm:"column:email"`
	PasswordHash *string `gorm:"column:password_hash"`
}

func (userRow) TableName() string { return "users" }

// identityRow is the GORM-mapped shape of one linked OAuth account.
type identityRow struct {
	ID             int64  `gorm:"column:id;primaryKey"`
	UserID         int64  `gorm:"column:user_id"`
	Provider       string `gorm:"column:provider"`
	ProviderUserID string `gorm:"column:provider_user_id"`
	ProviderEmail  string `gorm:"column:provider_email"`
}

func (identityRow) TableName() string { return "identities" }

type sessionRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    int64     `gorm:"column:user_id"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
}

func (sessionRow) TableName() string { return "sessions" }

// Store implements registration, login, and session verification against
// the users/sessions/identities tables (internal/db/schema.sql).
type Store struct {
	db *gorm.DB
	// Secure controls the cookie's Secure attribute. true in any real
	// deployment (HTTPS-only cookie); false only for http://localhost dev,
	// where the browser would otherwise silently refuse to store it.
	Secure bool
}

func New(db *gorm.DB, secure bool) *Store {
	return &Store{db: db, Secure: secure}
}

// Register creates a new account. Fails with ErrEmailTaken if the email
// (case-insensitively) is already registered, or a validation error for a
// malformed email / too-short password.
func (s *Store) Register(email, password string) (*User, error) {
	email = strings.TrimSpace(email)
	if !emailRE.MatchString(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("session: hash password: %w", err)
	}

	hashStr := string(hash)
	u := userRow{Email: email, PasswordHash: &hashStr}
	if err := s.db.Create(&u).Error; err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("session: insert user: %w", err)
	}

	return &User{ID: u.ID, Email: email}, nil
}

// Login verifies email/password and returns the matching user.
// ErrInvalidCredentials covers both a nonexistent email and a wrong
// password (and an OAuth-only account with no password at all) — see that
// error's doc comment for why these aren't distinguished in the response.
func (s *Store) Login(email, password string) (*User, error) {
	var row userRow
	err := s.db.Where("lower(email) = lower(?)", email).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("session: query user: %w", err)
	}

	if row.PasswordHash == nil {
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(*row.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}

	return &User{ID: row.ID, Email: email}, nil
}

// LoginOrCreateWithGoogle resolves a Google sign-in to a user account:
//
//  1. If this Google account (identified by its stable subject id, not its
//     email) has been linked before, return the user it's linked to.
//  2. Otherwise, if an account already exists with this Google account's
//     email (an email/password signup, or a different provider), link this
//     Google identity to that existing account and return it.
//  3. Otherwise, create a brand-new account with no password and link the
//     Google identity to it.
func (s *Store) LoginOrCreateWithGoogle(googleID, email string) (user *User, created bool, err error) {
	email = strings.TrimSpace(email)
	if googleID == "" {
		return nil, false, fmt.Errorf("session: empty google subject id")
	}
	if !emailRE.MatchString(email) {
		return nil, false, ErrInvalidEmail
	}

	var result User
	var isNew bool
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		var identity identityRow
		err := tx.Where("provider = ? AND provider_user_id = ?", "google", googleID).First(&identity).Error
		if err == nil {
			var row userRow
			if err := tx.Where("id = ?", identity.UserID).First(&row).Error; err != nil {
				return fmt.Errorf("session: load linked user: %w", err)
			}
			result = User{ID: row.ID, Email: row.Email}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("session: query identity: %w", err)
		}

		var existing userRow
		err = tx.Where("lower(email) = lower(?)", email).First(&existing).Error
		if err == nil {
			if err := tx.Create(&identityRow{UserID: existing.ID, Provider: "google", ProviderUserID: googleID, ProviderEmail: email}).Error; err != nil {
				return fmt.Errorf("session: link google identity: %w", err)
			}
			result = User{ID: existing.ID, Email: existing.Email}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("session: query user by email: %w", err)
		}

		u := userRow{Email: email, PasswordHash: nil}
		if err := tx.Create(&u).Error; err != nil {
			return fmt.Errorf("session: insert user: %w", err)
		}
		if err := tx.Create(&identityRow{UserID: u.ID, Provider: "google", ProviderUserID: googleID, ProviderEmail: email}).Error; err != nil {
			return fmt.Errorf("session: link google identity: %w", err)
		}
		result = User{ID: u.ID, Email: email}
		isNew = true
		return nil
	})
	if txErr != nil {
		return nil, false, txErr
	}

	return &result, isNew, nil
}

// CreateSession mints a new session for userID and sets its cookie on w.
func (s *Store) CreateSession(w http.ResponseWriter, userID int64) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", fmt.Errorf("session: generate id: %w", err)
	}
	expiresAt := time.Now().Add(sessionTTL)

	if err := s.db.Create(&sessionRow{ID: id, UserID: userID, ExpiresAt: expiresAt}).Error; err != nil {
		return "", fmt.Errorf("session: insert: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: sameSite(s.Secure),
	})

	return id, nil
}

// Verify resolves the session cookie on r, if any, to its user. ok is false
// for a missing cookie, an unknown/expired session, or a user that no
// longer exists.
func (s *Store) Verify(r *http.Request) (user *User, ok bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, false
	}

	var row userRow
	res := s.db.
		Table("sessions").
		Select("users.id, users.email").
		Joins("JOIN users ON users.id = sessions.user_id").
		Where("sessions.id = ? AND sessions.expires_at > ?", cookie.Value, time.Now()).
		Scan(&row)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, false
	}

	return &User{ID: row.ID, Email: row.Email}, true
}

// Logout deletes the session named by r's cookie (if any) and clears the
// cookie on w. Not an error if there was no session.
func (s *Store) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		s.db.Where("id = ?", cookie.Value).Delete(&sessionRow{})
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: sameSite(s.Secure),
	})
}

// sameSite picks the cookie's SameSite attribute to match what the browser
// will actually accept. SameSite=None requires Secure — browsers silently
// drop a None cookie set without it. Secure=true deployments use None
// because the console origin and the backend origin differ; Secure=false
// (plain-HTTP local dev) falls back to Lax.
func sameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func randomID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
