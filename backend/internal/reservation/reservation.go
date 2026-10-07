// Package reservation stores inbound "book a demo / talk to sales" leads
// submitted from the marketing site's plan pages (apps/landing). These are
// NOT customer accounts and NOT part of the billing flow: a visitor who is
// not ready to enter card details on pay.html can instead leave their name
// and contact info here, for the platform operator to follow up with by
// hand. Nothing in this package touches users, sessions or billing.
package reservation

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Product lines the marketing site has today (apps/landing/candidate,
// apps/landing/business). Kept as a small fixed set, like business.go's
// mascots/layouts, rather than open text, since this value drives which
// plan-tier strings are meaningful and is never user-visible as free text.
const (
	LineCandidate = "candidate"
	LineBusiness  = "business"
)

var productLines = map[string]bool{LineCandidate: true, LineBusiness: true}

// Field limits, generous enough for real names/messages but bounded so one
// abusive POST cannot store an arbitrarily large row.
const (
	MaxNameRunes    = 100
	MaxContactRunes = 200
	MaxTierRunes    = 60
	MaxMessageRunes = 2000
)

var (
	ErrInvalidProductLine = errors.New("reservation: invalid product line")
	ErrNameRequired       = errors.New("reservation: name is required")
	ErrContactRequired    = errors.New("reservation: email or phone is required")
	ErrTooLong            = errors.New("reservation: a field is too long")
)

// Reservation is one submitted inquiry.
type Reservation struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	ProductLine string    `gorm:"column:product_line" json:"productLine"`
	Tier        string    `gorm:"column:tier" json:"tier,omitempty"`
	Name        string    `gorm:"column:name" json:"name"`
	Contact     string    `gorm:"column:contact" json:"contact"`
	Message     string    `gorm:"column:message" json:"message,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"createdAt"`
}

func (Reservation) TableName() string { return "reservations" }

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

// Input is the caller-supplied, not-yet-validated submission.
type Input struct {
	ProductLine string
	Tier        string
	Name        string
	Contact     string
	Message     string
}

// Create validates and stores one inquiry, returning its id. Validation
// mirrors the plain-field checks in internal/business (required-ness and
// rune-count limits); there is no uniqueness constraint — the same visitor
// may legitimately submit more than once (e.g. once per plan they're
// considering).
func (s *Store) Create(in Input) (int64, error) {
	if !productLines[in.ProductLine] {
		return 0, ErrInvalidProductLine
	}
	if in.Name == "" {
		return 0, ErrNameRequired
	}
	if in.Contact == "" {
		return 0, ErrContactRequired
	}
	if utf8.RuneCountInString(in.Name) > MaxNameRunes ||
		utf8.RuneCountInString(in.Contact) > MaxContactRunes ||
		utf8.RuneCountInString(in.Tier) > MaxTierRunes ||
		utf8.RuneCountInString(in.Message) > MaxMessageRunes {
		return 0, ErrTooLong
	}
	r := Reservation{
		ProductLine: in.ProductLine,
		Tier:        in.Tier,
		Name:        in.Name,
		Contact:     in.Contact,
		Message:     in.Message,
	}
	if err := s.db.Create(&r).Error; err != nil {
		return 0, fmt.Errorf("reservation: create: %w", err)
	}
	return r.ID, nil
}
