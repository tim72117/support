// Package conversation stores the consumer-page chat transcripts: one
// conversation per anonymous visitor session, made of user and assistant
// messages. The conversation id is an unguessable random string and doubles
// as the visitor's anonymous credential for that one conversation.
package conversation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

var (
	// ErrNotFound: no such conversation, or no such user message in it.
	ErrNotFound = errors.New("conversation: not found")
	// ErrAlreadyReplied: that user message already has an assistant reply.
	ErrAlreadyReplied = errors.New("conversation: message already has a reply")
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Conversation struct {
	ID         string    `gorm:"column:id;primaryKey" json:"id"`
	BusinessID int64     `gorm:"column:business_id" json:"businessId"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"createdAt"`
}

func (Conversation) TableName() string { return "conversations" }

type Message struct {
	ID             int64     `gorm:"column:id;primaryKey" json:"id"`
	ConversationID string    `gorm:"column:conversation_id" json:"conversationId"`
	Role           string    `gorm:"column:role" json:"role"`
	Content        string    `gorm:"column:content" json:"content"`
	ReplyTo        *int64    `gorm:"column:reply_to" json:"replyTo,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"createdAt"`
}

func (Message) TableName() string { return "messages" }

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

// Create starts a new conversation for a business and returns its id.
func (s *Store) Create(businessID int64) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := s.db.Create(&Conversation{ID: id, BusinessID: businessID}).Error; err != nil {
		return "", fmt.Errorf("conversation: create: %w", err)
	}
	return id, nil
}

// Get returns the conversation (ErrNotFound if absent).
func (s *Store) Get(id string) (*Conversation, error) {
	var c Conversation
	err := s.db.Where("id = ?", id).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("conversation: get: %w", err)
	}
	return &c, nil
}

// CountUserMessages is how many visitor messages the conversation holds.
func (s *Store) CountUserMessages(conversationID string) (int, error) {
	var n int64
	err := s.db.Model(&Message{}).Where("conversation_id = ? AND role = ?", conversationID, RoleUser).Count(&n).Error
	return int(n), err
}

// AddUserMessage appends a visitor message and returns its id.
func (s *Store) AddUserMessage(conversationID, content string) (int64, error) {
	m := Message{ConversationID: conversationID, Role: RoleUser, Content: content}
	if err := s.db.Create(&m).Error; err != nil {
		return 0, fmt.Errorf("conversation: add user message: %w", err)
	}
	return m.ID, nil
}

// AddAssistantReply appends the AI's reply to the user message replyTo of the
// same conversation. ErrNotFound if that user message does not exist in this
// conversation; ErrAlreadyReplied if it already has a reply.
func (s *Store) AddAssistantReply(conversationID string, replyTo int64, content string) (int64, error) {
	var id int64
	res := s.db.Raw(`
		INSERT INTO messages (conversation_id, role, content, reply_to)
		SELECT $1, 'assistant', $2, m.id FROM messages m
		WHERE m.id = $3 AND m.conversation_id = $1 AND m.role = 'user'
		RETURNING id`, conversationID, content, replyTo).Scan(&id)
	if res.Error != nil {
		var pqErr *pq.Error
		if errors.As(res.Error, &pqErr) && pqErr.Code == "23505" {
			return 0, ErrAlreadyReplied
		}
		return 0, fmt.Errorf("conversation: add reply: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	return id, nil
}

// ListByBusiness returns a business's conversations, newest first.
func (s *Store) ListByBusiness(businessID int64, limit, offset int) ([]Conversation, error) {
	var out []Conversation
	err := s.db.Where("business_id = ?", businessID).Order("created_at DESC, id").Limit(limit).Offset(offset).Find(&out).Error
	return out, err
}

// Messages returns a conversation's messages in order.
func (s *Store) Messages(conversationID string) ([]Message, error) {
	var out []Message
	err := s.db.Where("conversation_id = ?", conversationID).Order("id").Find(&out).Error
	return out, err
}
