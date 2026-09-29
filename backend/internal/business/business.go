// Package business owns the businesses/business_content tables — one row
// per consumer-facing support page a business owner has set up, and the
// content their AI answers from. Everything about actually running the AI
// (tool-calling, WebSocket sessions, inference) lives in onagent, not here;
// this package only tracks what the business owner configured and, once
// internal/onagentclient exists, which onagent app that maps to.
package business

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ErrSlugTaken is returned by Create when the requested slug is already in
// use by another business.
var ErrSlugTaken = errors.New("this URL is already taken")

// Business is the caller-facing shape of one business owner's support page.
type Business struct {
	ID            int64
	OwnerID       int64
	Slug          string
	Name          string
	OnagentAppID  *string
	OnagentAPIKey *string
}

type businessRow struct {
	ID            int64   `gorm:"column:id;primaryKey"`
	OwnerID       int64   `gorm:"column:owner_id"`
	Slug          string  `gorm:"column:slug"`
	Name          string  `gorm:"column:name"`
	OnagentAppID  *string `gorm:"column:onagent_app_id"`
	OnagentAPIKey *string `gorm:"column:onagent_api_key"`
}

func (businessRow) TableName() string { return "businesses" }

func (r businessRow) toBusiness() *Business {
	return &Business{
		ID: r.ID, OwnerID: r.OwnerID, Slug: r.Slug, Name: r.Name,
		OnagentAppID: r.OnagentAppID, OnagentAPIKey: r.OnagentAPIKey,
	}
}

type contentRow struct {
	BusinessID int64  `gorm:"column:business_id;primaryKey"`
	Content    string `gorm:"column:content"`
}

func (contentRow) TableName() string { return "business_content" }

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Create makes a new business owned by ownerID. slug is lowercased
// alphanumeric-and-hyphens (the consumer-facing URL segment,
// /support/<slug>) and must be globally unique.
func (s *Store) Create(ownerID int64, slug, name string) (*Business, error) {
	if !slugRE.MatchString(slug) {
		return nil, fmt.Errorf("business: invalid slug %q", slug)
	}
	row := businessRow{OwnerID: ownerID, Slug: slug, Name: name}
	if err := s.db.Create(&row).Error; err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("business: insert: %w", err)
	}
	if err := s.db.Create(&contentRow{BusinessID: row.ID, Content: ""}).Error; err != nil {
		return nil, fmt.Errorf("business: insert content row: %w", err)
	}
	return row.toBusiness(), nil
}

// Get looks up a business by id regardless of owner — callers that need
// ownership enforced (e.g. console.Handler.withOwnedBusiness) check
// Business.OwnerID themselves, the same "look up, then compare" shape
// onagent's own toolschema.Registry uses.
func (s *Store) Get(id int64) (*Business, error) {
	var row businessRow
	if err := s.db.Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, err
	}
	return row.toBusiness(), nil
}

// ListByOwner returns every business ownerID owns.
func (s *Store) ListByOwner(ownerID int64) ([]*Business, error) {
	var rows []businessRow
	if err := s.db.Where("owner_id = ?", ownerID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("business: list: %w", err)
	}
	out := make([]*Business, len(rows))
	for i, r := range rows {
		out[i] = r.toBusiness()
	}
	return out, nil
}

// Delete removes a business and its content (business_content cascades via
// its foreign key). Does not touch anything on the onagent side — the
// corresponding onagent app, if one was ever created, is left as-is; tearing
// that down too is future onagentclient scope, not this skeleton's.
func (s *Store) Delete(id int64) error {
	return s.db.Where("id = ?", id).Delete(&businessRow{}).Error
}

// GetContent returns the business's current content configuration.
func (s *Store) GetContent(businessID int64) (string, error) {
	var row contentRow
	if err := s.db.Where("business_id = ?", businessID).Take(&row).Error; err != nil {
		return "", fmt.Errorf("business: get content: %w", err)
	}
	return row.Content, nil
}

// SetContent replaces the business's content configuration.
func (s *Store) SetContent(businessID int64, content string) error {
	res := s.db.Model(&contentRow{}).Where("business_id = ?", businessID).Update("content", content)
	if res.Error != nil {
		return fmt.Errorf("business: set content: %w", res.Error)
	}
	return nil
}

// SetOnagentApp records which onagent app this business maps to, once
// internal/onagentclient has created one. Not called anywhere yet in this
// skeleton.
func (s *Store) SetOnagentApp(businessID int64, appID, apiKey string) error {
	res := s.db.Model(&businessRow{}).Where("id = ?", businessID).
		Updates(map[string]any{"onagent_app_id": appID, "onagent_api_key": apiKey})
	if res.Error != nil {
		return fmt.Errorf("business: set onagent app: %w", res.Error)
	}
	return nil
}
