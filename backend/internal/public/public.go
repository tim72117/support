// Package public serves the anonymous, cookie-less API behind the consumer
// chat page (/support/<slug>).
//
// Conversation flow (decided by the product owner):
//  1. the visitor sends a message to POST .../chat on THIS backend;
//  2. this backend validates, rate-limits, checks the business owner's quota,
//     stores the message, and returns it;
//  3. only then does the browser forward the returned content to onagent over
//     @onagent/bridge and show onagent's reply;
//  4. the browser reports the reply back via POST .../chat/reply so it is
//     stored and its (estimated) usage recorded.
//
// Trust model: steps 3-4 happen in the visitor's browser. The onagent API key
// handed out by GET /public/businesses/{slug} is a browser-side key (onagent
// binds it to the app's allowed origins), but a non-browser client can send any
// Origin header, so nothing stops someone from using the key against onagent
// directly and skipping this backend. Reported replies are untrusted text:
// token usage is therefore ESTIMATED here from message lengths, never taken
// from the client, and each user message accepts at most one reply.
package public

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/conversation"
	"github.com/tim72117/ai-support/internal/quota"
)

const (
	// MaxMessageRunes matches onagent's default per-prompt limit
	// (inference.defaultMaxPromptLength = 500); a longer message would be
	// rejected by onagent anyway, after we had already logged and billed it.
	MaxMessageRunes = 500
	// MaxReplyRunes bounds what a browser may report as the AI's reply.
	MaxReplyRunes = 8000
	// MaxMessagesPerConversation bounds one conversation's visitor messages.
	MaxMessagesPerConversation = 100

	maxChatBody  = 8 << 10
	maxReplyBody = 64 << 10
)

// Error codes returned in {"error":{"code":...}}; the frontend branches on them.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeContentTooLong       = "content_too_long"
	CodeNotFound             = "not_found"
	CodeConversationNotFound = "conversation_not_found"
	CodeAlreadyReplied       = "already_replied"
	CodeRateLimited          = "rate_limited"
	CodeConversationFull     = "conversation_full"
	CodeQuotaExceeded        = "quota_exceeded"
	CodeUnavailable          = "chat_unavailable"
	CodeInternal             = "internal_error"
)

type businessLookup interface {
	GetBySlug(slug string) (*business.Business, error)
}

type chatStore interface {
	Create(businessID int64) (string, error)
	Get(id string) (*conversation.Conversation, error)
	CountUserMessages(conversationID string) (int, error)
	AddUserMessage(conversationID, content string) (int64, error)
	AddAssistantReply(conversationID string, replyTo int64, content string) (int64, error)
}

type quotaService interface {
	Check(ctx context.Context, userID int64) (quota.Decision, error)
	Record(ctx context.Context, businessID, userID int64, eventID string, usage *quota.Usage) error
}

// Config wires a Handler.
type Config struct {
	Businesses businessLookup
	Chats      chatStore
	// Quota may be a nil *quota.Service (QUOTA_ENABLED=false): checks always
	// allow and records are no-ops.
	Quota quotaService
	// OnagentWSURL is what the browser's bridge connects to; empty means
	// onagent integration is off and chat is reported unavailable.
	OnagentWSURL string
	// TrustProxy: take the client IP from the right-most X-Forwarded-For.
	TrustProxy bool
	Log        *slog.Logger
}

type Handler struct {
	cfg      Config
	ipChat   *limiter
	ipReply  *limiter
	convChat *limiter
	log      *slog.Logger
}

func NewHandler(cfg Config) *Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		cfg:      cfg,
		ipChat:   newLimiter(30, time.Minute),
		ipReply:  newLimiter(60, time.Minute),
		convChat: newLimiter(10, time.Minute),
		log:      log,
	}
}

// Register mounts the routes. Wrap the mux with CORS (see CORS) — these
// routes carry no cookies and need no credentials.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /public/businesses/{slug}", h.getBusiness)
	mux.HandleFunc("POST /public/businesses/{slug}/chat", h.chat)
	mux.HandleFunc("POST /public/businesses/{slug}/chat/reply", h.reply)
}

// --- GET /public/businesses/{slug} ------------------------------------------

type chatInfo struct {
	Available bool   `json:"available"`
	WSURL     string `json:"wsUrl,omitempty"`
	AppID     string `json:"appId,omitempty"`
	// APIKey is onagent's browser-side key for this business's app: onagent
	// only accepts it from the app's allowed origins and it can only talk to
	// this one app. It is intentionally public.
	APIKey string `json:"apiKey,omitempty"`
}

type businessResponse struct {
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Tagline          string   `json:"tagline"`
	Mascot           string   `json:"mascot"`
	ThemeColor       string   `json:"themeColor"`
	MaxMessageLength int      `json:"maxMessageLength"`
	Chat             chatInfo `json:"chat"`
}

func (h *Handler) getBusiness(w http.ResponseWriter, r *http.Request) {
	b, ok := h.lookup(w, r)
	if !ok {
		return
	}
	resp := businessResponse{
		Slug: b.Slug, Name: b.Name, Tagline: b.Tagline, Mascot: b.Mascot, ThemeColor: b.ThemeColor,
		MaxMessageLength: MaxMessageRunes,
	}
	if h.chatAvailable(b) {
		resp.Chat = chatInfo{Available: true, WSURL: h.cfg.OnagentWSURL, AppID: *b.OnagentAppID, APIKey: *b.OnagentAPIKey}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) chatAvailable(b *business.Business) bool {
	return h.cfg.OnagentWSURL != "" && b.OnagentAppID != nil && *b.OnagentAppID != "" &&
		b.OnagentAPIKey != nil && *b.OnagentAPIKey != ""
}

// --- POST /public/businesses/{slug}/chat -------------------------------------

type chatRequest struct {
	ConversationID string `json:"conversationId"`
	Content        string `json:"content"`
}

type chatResponse struct {
	ConversationID string `json:"conversationId"`
	MessageID      int64  `json:"messageId"`
	Content        string `json:"content"`
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if !h.ipChat.allow(clientIP(r, h.cfg.TrustProxy)) {
		writeError(w, http.StatusTooManyRequests, CodeRateLimited, "請求太頻繁，請稍後再試。")
		return
	}
	var req chatRequest
	if !decode(w, r, maxChatBody, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "訊息不可為空。")
		return
	}
	if utf8.RuneCountInString(content) > MaxMessageRunes {
		writeError(w, http.StatusBadRequest, CodeContentTooLong, fmt.Sprintf("訊息最多 %d 個字。", MaxMessageRunes))
		return
	}

	b, ok := h.lookup(w, r)
	if !ok {
		return
	}
	if !h.chatAvailable(b) {
		writeError(w, http.StatusServiceUnavailable, CodeUnavailable, "目前無法服務。")
		return
	}

	convID := strings.TrimSpace(req.ConversationID)
	if convID != "" {
		c, err := h.cfg.Chats.Get(convID)
		if errors.Is(err, conversation.ErrNotFound) || (err == nil && c.BusinessID != b.ID) {
			writeError(w, http.StatusNotFound, CodeConversationNotFound, "找不到這段對話。")
			return
		}
		if err != nil {
			h.internal(w, "get conversation", err)
			return
		}
		if !h.convChat.allow(convID) {
			writeError(w, http.StatusTooManyRequests, CodeRateLimited, "訊息送得太快了，請稍後再試。")
			return
		}
		n, err := h.cfg.Chats.CountUserMessages(convID)
		if err != nil {
			h.internal(w, "count messages", err)
			return
		}
		if n >= MaxMessagesPerConversation {
			writeError(w, http.StatusTooManyRequests, CodeConversationFull, "這段對話已達訊息上限，請重新開始。")
			return
		}
	}

	// Quota gate, before anything is stored or forwarded. A database error
	// fails closed here (unlike onagent's own fail-open): the very next step
	// writes to the same database, so allowing would not help anyone.
	dec, err := h.cfg.Quota.Check(r.Context(), b.OwnerID)
	if err != nil {
		h.internal(w, "quota check", err)
		return
	}
	if !dec.Allowed {
		h.log.Info("chat rejected: owner over quota", "business", b.ID, "used", dec.Used, "limit", dec.Limit)
		writeError(w, http.StatusTooManyRequests, CodeQuotaExceeded, "目前無法服務，請稍後再試。")
		return
	}

	if convID == "" {
		if convID, err = h.cfg.Chats.Create(b.ID); err != nil {
			h.internal(w, "create conversation", err)
			return
		}
	}
	msgID, err := h.cfg.Chats.AddUserMessage(convID, content)
	if err != nil {
		h.internal(w, "store message", err)
		return
	}
	// The prompt side of usage is recorded now, from what this backend itself
	// saw. A failure to record must not drop the visitor's message.
	if err := h.cfg.Quota.Record(r.Context(), b.ID, b.OwnerID, fmt.Sprintf("msg-%d", msgID), promptUsage(content)); err != nil {
		h.log.Error("record prompt usage", "business", b.ID, "err", err)
	}
	writeJSON(w, http.StatusOK, chatResponse{ConversationID: convID, MessageID: msgID, Content: content})
}

// --- POST /public/businesses/{slug}/chat/reply -------------------------------

type replyRequest struct {
	ConversationID string `json:"conversationId"`
	MessageID      int64  `json:"messageId"`
	Content        string `json:"content"`
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request) {
	if !h.ipReply.allow(clientIP(r, h.cfg.TrustProxy)) {
		writeError(w, http.StatusTooManyRequests, CodeRateLimited, "請求太頻繁，請稍後再試。")
		return
	}
	var req replyRequest
	if !decode(w, r, maxReplyBody, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if req.ConversationID == "" || req.MessageID <= 0 || content == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "資料不完整。")
		return
	}
	if utf8.RuneCountInString(content) > MaxReplyRunes {
		writeError(w, http.StatusBadRequest, CodeContentTooLong, "回覆內容過長。")
		return
	}
	b, ok := h.lookup(w, r)
	if !ok {
		return
	}
	c, err := h.cfg.Chats.Get(req.ConversationID)
	if errors.Is(err, conversation.ErrNotFound) || (err == nil && c.BusinessID != b.ID) {
		writeError(w, http.StatusNotFound, CodeConversationNotFound, "找不到這段對話。")
		return
	}
	if err != nil {
		h.internal(w, "get conversation", err)
		return
	}
	replyID, err := h.cfg.Chats.AddAssistantReply(req.ConversationID, req.MessageID, content)
	switch {
	case errors.Is(err, conversation.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "找不到對應的訊息。")
		return
	case errors.Is(err, conversation.ErrAlreadyReplied):
		writeError(w, http.StatusConflict, CodeAlreadyReplied, "這則訊息已經有回覆了。")
		return
	case err != nil:
		h.internal(w, "store reply", err)
		return
	}
	if err := h.cfg.Quota.Record(r.Context(), b.ID, b.OwnerID, fmt.Sprintf("reply-%d", replyID), completionUsage(content)); err != nil {
		h.log.Error("record completion usage", "business", b.ID, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messageId": replyID})
}

// --- usage estimation --------------------------------------------------------

// estimateTokens is a deliberately crude, client-independent token estimate:
// one token per two characters (rounded up, at least 1). That overestimates
// English (~4 chars/token) and is about right for CJK (~1-2 chars/token) —
// biased toward over- rather than under-charging, per quota.Record's note.
// It ignores the business content onagent prepends as the system prompt.
func estimateTokens(s string) int {
	n := (utf8.RuneCountInString(s) + 1) / 2
	if n < 1 {
		n = 1
	}
	return n
}

func promptUsage(content string) *quota.Usage {
	n := estimateTokens(content)
	return &quota.Usage{PromptTokens: n, TotalTokens: n}
}

func completionUsage(content string) *quota.Usage {
	n := estimateTokens(content)
	return &quota.Usage{CompletionTokens: n, TotalTokens: n}
}

// --- helpers -----------------------------------------------------------------

// lookup resolves {slug}, writing the 404/500 itself.
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) (*business.Business, bool) {
	b, err := h.cfg.Businesses.GetBySlug(r.PathValue("slug"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, CodeNotFound, "找不到這個服務頁面。")
		return nil, false
	}
	if err != nil {
		h.internal(w, "lookup business", err)
		return nil, false
	}
	return b, true
}

// internal logs the real error and answers with a generic one, so internals
// never reach an anonymous caller.
func (h *Handler) internal(w http.ResponseWriter, what string, err error) {
	h.log.Error("public api: "+what, "err", err)
	writeError(w, http.StatusInternalServerError, CodeInternal, "系統忙碌中，請稍後再試。")
}

func decode(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, CodeContentTooLong, "內容過長。")
			return false
		}
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "資料格式不正確。")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// CORS answers cross-origin requests for the anonymous public routes: only
// origins on the allowlist are echoed back, no credentials are allowed (the
// routes use no cookies), and "*" is never used.
func CORS(allowed func(origin string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			if origin := r.Header.Get("Origin"); origin != "" && allowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
