// Package business owns the businesses/business_content tables — one row
// per consumer-facing support page a business owner has set up, and the
// content their AI answers from. Everything about actually running the AI
// (tool-calling, WebSocket sessions, inference) lives in onagent, not here;
// this package only tracks what the business owner configured and, once
// internal/onagentclient exists, which onagent app that maps to.
package business

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

var (
	slugRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	colorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Limits and defaults for a business's look. Mascots are the set the console
// and the consumer page can draw.
const (
	MaxSlugLen    = 40
	MaxNameRunes  = 60
	MaxTagRunes   = 80
	DefaultMascot = "fox"
	DefaultColor  = "#FF8A5B"
	DefaultLayout = "center"
)

var mascots = map[string]bool{
	"fox": true, "bear": true, "cat": true, "bird": true,
	"rabbit": true, "dog": true, "owl": true, "penguin": true, "panda": true, "pig": true,
}
var layouts = map[string]bool{"center": true, "split": true}

// ErrInvalidSlug / ErrInvalidName are returned by Create for input the caller
// should fix (as opposed to a server fault).
var (
	ErrInvalidSlug = errors.New("invalid URL: use lowercase letters, digits and hyphens, starting with a letter or digit")
	ErrInvalidName = errors.New("name is required")
	// ErrInvalidBranding wraps a description of which look setting is wrong.
	ErrInvalidBranding = errors.New("invalid branding")
)

// ErrSlugTaken is returned by Create when the requested slug is already in
// use by another business.
var ErrSlugTaken = errors.New("this URL is already taken")

// Business is the caller-facing shape of one business owner's support page.
type Business struct {
	ID         int64
	OwnerID    int64
	Slug       string
	Name       string
	Tagline    string
	Mascot     string
	ThemeColor string
	Layout     string
	// Connected is true once the business has an onagent app and key, i.e. its
	// consumer page can actually chat. Derived, not stored.
	Connected    bool
	OnagentAppID *string
	// OnagentAPIKey is never serialized by the owner-facing console API; it is
	// only exposed (deliberately, it is a browser-side key) by the public
	// consumer endpoint.
	OnagentAPIKey *string `json:"-"`
}

type businessRow struct {
	ID            int64   `gorm:"column:id;primaryKey"`
	OwnerID       int64   `gorm:"column:owner_id"`
	Slug          string  `gorm:"column:slug"`
	Name          string  `gorm:"column:name"`
	Tagline       string  `gorm:"column:tagline"`
	Mascot        string  `gorm:"column:mascot"`
	ThemeColor    string  `gorm:"column:theme_color"`
	Layout        string  `gorm:"column:layout"`
	OnagentAppID  *string `gorm:"column:onagent_app_id"`
	OnagentAPIKey *string `gorm:"column:onagent_api_key"`
}

func (businessRow) TableName() string { return "businesses" }

func (r businessRow) toBusiness() *Business {
	return &Business{
		ID: r.ID, OwnerID: r.OwnerID, Slug: r.Slug, Name: r.Name,
		Tagline: r.Tagline, Mascot: r.Mascot, ThemeColor: r.ThemeColor, Layout: r.Layout,
		Connected:    r.OnagentAppID != nil && *r.OnagentAppID != "" && r.OnagentAPIKey != nil && *r.OnagentAPIKey != "",
		OnagentAppID: r.OnagentAppID, OnagentAPIKey: r.OnagentAPIKey,
	}
}

type contentRow struct {
	BusinessID int64   `gorm:"column:business_id;primaryKey"`
	Content    string  `gorm:"column:content"`
	Sections   *string `gorm:"column:sections"` // opaque editor JSON, never interpreted here
}

func (contentRow) TableName() string { return "business_content" }

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Branding is the owner-chosen look of a business. Empty Mascot/ThemeColor
// mean "use the default".
type Branding struct {
	Name       string
	Tagline    string
	Mascot     string
	ThemeColor string
	Layout     string
}

// normalize trims and defaults b, and reports what is wrong with it.
func (b Branding) normalize() (Branding, error) {
	b.Name = strings.TrimSpace(b.Name)
	b.Tagline = strings.TrimSpace(b.Tagline)
	if b.Mascot == "" {
		b.Mascot = DefaultMascot
	}
	if b.ThemeColor == "" {
		b.ThemeColor = DefaultColor
	}
	if b.Layout == "" {
		b.Layout = DefaultLayout
	}
	switch {
	case b.Name == "":
		return b, ErrInvalidName
	case utf8.RuneCountInString(b.Name) > MaxNameRunes:
		return b, fmt.Errorf("%w: name is longer than %d characters", ErrInvalidBranding, MaxNameRunes)
	case utf8.RuneCountInString(b.Tagline) > MaxTagRunes:
		return b, fmt.Errorf("%w: tagline is longer than %d characters", ErrInvalidBranding, MaxTagRunes)
	case !mascots[b.Mascot]:
		return b, fmt.Errorf("%w: unknown mascot %q", ErrInvalidBranding, b.Mascot)
	case !colorRE.MatchString(b.ThemeColor):
		return b, fmt.Errorf("%w: theme color must look like #RRGGBB", ErrInvalidBranding)
	case !layouts[b.Layout]:
		return b, fmt.Errorf("%w: unknown layout %q", ErrInvalidBranding, b.Layout)
	}
	return b, nil
}

// Create makes a new business with the default look. See CreateWithBranding.
func (s *Store) Create(ownerID int64, slug, name string) (*Business, error) {
	return s.CreateWithBranding(ownerID, slug, Branding{Name: name})
}

// CreateWithBranding makes a new business owned by ownerID. slug is lowercased
// alphanumeric-and-hyphens (the consumer-facing URL segment,
// /support/<slug>), globally unique, and cannot be changed later (links that
// were already shared must not break).
func (s *Store) CreateWithBranding(ownerID int64, slug string, in Branding) (*Business, error) {
	if !slugRE.MatchString(slug) || len(slug) > MaxSlugLen {
		return nil, ErrInvalidSlug
	}
	br, err := in.normalize()
	if err != nil {
		return nil, err
	}
	row := businessRow{OwnerID: ownerID, Slug: slug, Name: br.Name, Tagline: br.Tagline, Mascot: br.Mascot, ThemeColor: br.ThemeColor, Layout: br.Layout}
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

// Patch is a partial update of a business's look; nil fields are left alone.
type Patch struct {
	Name       *string
	Tagline    *string
	Mascot     *string
	ThemeColor *string
	Layout     *string
}

// Update applies p to the business and returns the result. The slug is
// deliberately not updatable.
func (s *Store) Update(id int64, p Patch) (*Business, error) {
	cur, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	next := Branding{Name: cur.Name, Tagline: cur.Tagline, Mascot: cur.Mascot, ThemeColor: cur.ThemeColor, Layout: cur.Layout}
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.Tagline != nil {
		next.Tagline = *p.Tagline
	}
	if p.Mascot != nil {
		next.Mascot = *p.Mascot
	}
	if p.ThemeColor != nil {
		next.ThemeColor = *p.ThemeColor
	}
	if p.Layout != nil {
		next.Layout = *p.Layout
	}
	br, err := next.normalize()
	if err != nil {
		return nil, err
	}
	res := s.db.Model(&businessRow{}).Where("id = ?", id).
		Updates(map[string]any{"name": br.Name, "tagline": br.Tagline, "mascot": br.Mascot, "theme_color": br.ThemeColor, "layout": br.Layout})
	if res.Error != nil {
		return nil, fmt.Errorf("business: update: %w", res.Error)
	}
	return s.Get(id)
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

// ListAll returns every business regardless of owner, newest first. Unlike
// ListByOwner, this is not scoped to a single caller — it exists only for
// the platform-admin back office (internal/admin), which is allowed to see
// across every owner; normal console callers must never reach this (they go
// through ListByOwner, which enforces the owner_id scope itself).
func (s *Store) ListAll() ([]*Business, error) {
	var rows []businessRow
	if err := s.db.Order("id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("business: list all: %w", err)
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

// MaxSectionsBytes bounds the opaque editor state stored next to the content.
const MaxSectionsBytes = 64 << 10

// ErrInvalidSections is returned for editor state that is not valid JSON or is too large.
var ErrInvalidSections = errors.New("invalid sections")

// GetContentAndSections returns the flat content the AI reads together with
// the editor state it was built from (nil when none was saved).
func (s *Store) GetContentAndSections(businessID int64) (string, json.RawMessage, error) {
	var row contentRow
	if err := s.db.Where("business_id = ?", businessID).Take(&row).Error; err != nil {
		return "", nil, fmt.Errorf("business: get content: %w", err)
	}
	if row.Sections == nil {
		return row.Content, nil, nil
	}
	return row.Content, json.RawMessage(*row.Sections), nil
}

// SetContentAndSections replaces the content and the editor state together.
// sections may be nil to clear it.
func (s *Store) SetContentAndSections(businessID int64, content string, sections json.RawMessage) error {
	var stored *string
	if len(sections) > 0 {
		if len(sections) > MaxSectionsBytes || !json.Valid(sections) {
			return ErrInvalidSections
		}
		v := string(sections)
		stored = &v
	}
	res := s.db.Model(&contentRow{}).Where("business_id = ?", businessID).
		Updates(map[string]any{"content": content, "sections": stored})
	if res.Error != nil {
		return fmt.Errorf("business: set content: %w", res.Error)
	}
	return nil
}

// Section is one parsed content section, matching the console's
// ContentSection shape (apps/console/src/model.ts) as stored verbatim in
// business_content.sections. Only id/title/body are read here — the
// console's "placeholder" field is editor-only guidance and is never part of
// what SetContentAndSections receives (sectionsForBackend strips it before
// saving), so there is nothing to decode for it.
type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ErrSectionNotFound is returned by GetSection when the business has no
// saved sections, or none with the requested id.
var ErrSectionNotFound = errors.New("section not found")

// ListSections returns the business's saved sections with non-empty bodies,
// in their saved order, omitting the body text itself (title/id only) — this
// is what the onagent list_sections tool answers with, letting the AI
// discover what's available before asking for one section's full content.
// Returns an empty (non-nil) slice when no sections were saved or none has a
// body yet.
func (s *Store) ListSections(businessID int64) ([]Section, error) {
	_, raw, err := s.GetContentAndSections(businessID)
	if err != nil {
		return nil, err
	}
	all, err := parseSections(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Section, 0, len(all))
	for _, sec := range all {
		if strings.TrimSpace(sec.Body) == "" {
			continue
		}
		out = append(out, Section{ID: sec.ID, Title: sec.Title})
	}
	return out, nil
}

// GetSection returns one saved section's full content by id. ErrSectionNotFound
// if the business has no sections saved, the id doesn't match any saved
// section, or that section's body is empty (nothing to read yet).
func (s *Store) GetSection(businessID int64, sectionID string) (Section, error) {
	_, raw, err := s.GetContentAndSections(businessID)
	if err != nil {
		return Section{}, err
	}
	all, err := parseSections(raw)
	if err != nil {
		return Section{}, err
	}
	for _, sec := range all {
		if sec.ID == sectionID {
			if strings.TrimSpace(sec.Body) == "" {
				return Section{}, ErrSectionNotFound
			}
			return sec, nil
		}
	}
	return Section{}, ErrSectionNotFound
}

// parseSections decodes the opaque sections JSON into Section values. A nil
// or empty raw is "no sections saved" (empty result, not an error) — this is
// the normal state for a business that was created but never filled in, or
// whose owner saved content before the editor's structured sections existed.
func parseSections(raw json.RawMessage) ([]Section, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Section
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("business: parse sections: %w", err)
	}
	return out, nil
}

// SetOnagentApp records which onagent app this business maps to, once
// internal/onagentclient has created one. Not called anywhere yet in this
// skeleton (SetOnagentAppID/SetOnagentKey are used instead, step by step).
func (s *Store) SetOnagentApp(businessID int64, appID, apiKey string) error {
	res := s.db.Model(&businessRow{}).Where("id = ?", businessID).
		Updates(map[string]any{"onagent_app_id": appID, "onagent_api_key": apiKey})
	if res.Error != nil {
		return fmt.Errorf("business: set onagent app: %w", res.Error)
	}
	return nil
}

// GetBySlug looks up a business by its public slug (gorm.ErrRecordNotFound if
// there is none).
func (s *Store) GetBySlug(slug string) (*Business, error) {
	var row businessRow
	if err := s.db.Where("slug = ?", slug).Take(&row).Error; err != nil {
		return nil, err
	}
	return row.toBusiness(), nil
}

// SetOnagentAppID records the onagent app created for this business before
// its key is issued, so a half-finished provisioning can resume instead of
// creating a second app.
func (s *Store) SetOnagentAppID(businessID int64, appID string) error {
	if err := s.db.Model(&businessRow{}).Where("id = ?", businessID).Update("onagent_app_id", appID).Error; err != nil {
		return fmt.Errorf("business: set onagent app id: %w", err)
	}
	return nil
}

// SetOnagentKey records the app's API key.
func (s *Store) SetOnagentKey(businessID int64, apiKey string) error {
	if err := s.db.Model(&businessRow{}).Where("id = ?", businessID).Update("onagent_api_key", apiKey).Error; err != nil {
		return fmt.Errorf("business: set onagent key: %w", err)
	}
	return nil
}
