package public

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/conversation"
	"github.com/tim72117/ai-support/internal/quota"
)

type fakeBusinesses struct {
	byslug   map[string]*business.Business
	sections map[int64][]business.Section // businessID -> sections (including empty-body ones)
}

func (f fakeBusinesses) GetBySlug(slug string) (*business.Business, error) {
	if b, ok := f.byslug[slug]; ok {
		return b, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f fakeBusinesses) ListSections(businessID int64) ([]business.Section, error) {
	out := make([]business.Section, 0)
	for _, s := range f.sections[businessID] {
		if strings.TrimSpace(s.Body) == "" {
			continue
		}
		out = append(out, business.Section{ID: s.ID, Title: s.Title})
	}
	return out, nil
}

func (f fakeBusinesses) GetSection(businessID int64, sectionID string) (business.Section, error) {
	for _, s := range f.sections[businessID] {
		if s.ID == sectionID {
			if strings.TrimSpace(s.Body) == "" {
				return business.Section{}, business.ErrSectionNotFound
			}
			return s, nil
		}
	}
	return business.Section{}, business.ErrSectionNotFound
}

type fakeChats struct {
	convs   map[string]int64 // id -> business id
	msgs    []conversation.Message
	replies map[int64]bool
}

func newFakeChats() *fakeChats {
	return &fakeChats{convs: map[string]int64{}, replies: map[int64]bool{}}
}

func (f *fakeChats) Create(bid int64) (string, error) {
	id := "conv" + string(rune('a'+len(f.convs)))
	f.convs[id] = bid
	return id, nil
}
func (f *fakeChats) Get(id string) (*conversation.Conversation, error) {
	bid, ok := f.convs[id]
	if !ok {
		return nil, conversation.ErrNotFound
	}
	return &conversation.Conversation{ID: id, BusinessID: bid}, nil
}
func (f *fakeChats) CountUserMessages(id string) (int, error) {
	n := 0
	for _, m := range f.msgs {
		if m.ConversationID == id && m.Role == "user" {
			n++
		}
	}
	return n, nil
}
func (f *fakeChats) AddUserMessage(id, content string) (int64, error) {
	mid := int64(len(f.msgs) + 1)
	f.msgs = append(f.msgs, conversation.Message{ID: mid, ConversationID: id, Role: "user", Content: content})
	return mid, nil
}
func (f *fakeChats) AddAssistantReply(id string, replyTo int64, content string) (int64, error) {
	found := false
	for _, m := range f.msgs {
		if m.ID == replyTo && m.ConversationID == id && m.Role == "user" {
			found = true
		}
	}
	if !found {
		return 0, conversation.ErrNotFound
	}
	if f.replies[replyTo] {
		return 0, conversation.ErrAlreadyReplied
	}
	f.replies[replyTo] = true
	mid := int64(len(f.msgs) + 1)
	f.msgs = append(f.msgs, conversation.Message{ID: mid, ConversationID: id, Role: "assistant", Content: content, ReplyTo: &replyTo})
	return mid, nil
}

type recordedUsage struct {
	businessID, userID int64
	eventID            string
	usage              *quota.Usage
}

type fakeQuota struct {
	allowed bool
	err     error
	records []recordedUsage
}

func (f *fakeQuota) Check(context.Context, int64) (quota.Decision, error) {
	return quota.Decision{Allowed: f.allowed, Used: 5, Limit: 5}, f.err
}
func (f *fakeQuota) Record(_ context.Context, bid, uid int64, ev string, u *quota.Usage) error {
	f.records = append(f.records, recordedUsage{bid, uid, ev, u})
	return nil
}

func str(s string) *string { return &s }

type env struct {
	h     *Handler
	mux   *http.ServeMux
	chats *fakeChats
	quota *fakeQuota
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{chats: newFakeChats(), quota: &fakeQuota{allowed: true}}
	biz := fakeBusinesses{
		byslug: map[string]*business.Business{
			"shop":  {ID: 1, OwnerID: 10, Slug: "shop", Name: "Shop", OnagentAppID: str("app-1"), OnagentAPIKey: str("key-1")},
			"other": {ID: 2, OwnerID: 11, Slug: "other", Name: "Other", OnagentAppID: str("app-2"), OnagentAPIKey: str("key-2")},
			"bare":  {ID: 3, OwnerID: 12, Slug: "bare", Name: "Bare"},
		},
		sections: map[int64][]business.Section{
			1: {
				{ID: "hours", Title: "營業時間", Body: "週一到週五 9:00–18:00"},
				{ID: "other", Title: "其他", Body: ""},
			},
		},
	}
	e.h = NewHandler(Config{Businesses: biz, Chats: e.chats, Quota: e.quota, OnagentWSURL: "wss://onagent.test/ws", Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	e.mux = http.NewServeMux()
	e.h.Register(e.mux)
	return e
}

func (e *env) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not an error body: %q", rec.Body.String())
	}
	return out.Error.Code
}

func TestGetBusiness(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/public/businesses/shop", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	chat := out["chat"].(map[string]any)
	if chat["available"] != true || chat["appId"] != "app-1" || chat["apiKey"] != "key-1" || chat["wsUrl"] != "wss://onagent.test/ws" {
		t.Fatalf("chat info: %+v", chat)
	}
	// Nothing internal may leak.
	for _, k := range []string{"ID", "OwnerID", "id", "ownerId", "OnagentAPIKey"} {
		if _, ok := out[k]; ok {
			t.Fatalf("leaked field %q", k)
		}
	}

	rec = e.do("GET", "/public/businesses/bare", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "apiKey") || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("unprovisioned: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.do("GET", "/public/businesses/nope", ""); rec.Code != 404 || errCode(t, rec) != CodeNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body.String())
	}
}

func TestListSections(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/public/businesses/shop/sections", "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ Sections []sectionSummary }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sections) != 1 || out.Sections[0].ID != "hours" || out.Sections[0].Title != "營業時間" {
		t.Fatalf("sections %+v", out.Sections)
	}
	if strings.Contains(rec.Body.String(), "9:00") {
		t.Fatal("list_sections must not leak section bodies")
	}

	// business with no sections saved at all: empty list, not an error.
	rec = e.do("GET", "/public/businesses/bare/sections", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sections":[]`) {
		t.Fatalf("no sections: %d %s", rec.Code, rec.Body.String())
	}

	if rec = e.do("GET", "/public/businesses/nope/sections", ""); rec.Code != 404 || errCode(t, rec) != CodeNotFound {
		t.Fatalf("unknown business: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetSection(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/public/businesses/shop/sections/hours", "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID, Title, Body string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "hours" || out.Title != "營業時間" || !strings.Contains(out.Body, "9:00") {
		t.Fatalf("section %+v", out)
	}

	// unknown id, empty-body section, and unknown business all read as 404.
	if rec = e.do("GET", "/public/businesses/shop/sections/nope", ""); rec.Code != 404 || errCode(t, rec) != CodeNotFound {
		t.Fatalf("unknown section: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.do("GET", "/public/businesses/shop/sections/other", ""); rec.Code != 404 {
		t.Fatalf("empty section: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.do("GET", "/public/businesses/nope/sections/hours", ""); rec.Code != 404 {
		t.Fatalf("unknown business: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatStoresMessageAndRecordsUsage(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"  你們幾點開門？ "}`)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out chatResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ConversationID == "" || out.MessageID == 0 || out.Content != "你們幾點開門？" {
		t.Fatalf("resp %+v", out)
	}
	if len(e.chats.msgs) != 1 || e.chats.msgs[0].Content != "你們幾點開門？" || e.chats.msgs[0].Role != "user" {
		t.Fatalf("stored %+v", e.chats.msgs)
	}
	if len(e.quota.records) != 1 || e.quota.records[0].businessID != 1 || e.quota.records[0].userID != 10 ||
		e.quota.records[0].usage.PromptTokens != 4 || e.quota.records[0].usage.TotalTokens != 4 {
		t.Fatalf("usage %+v", e.quota.records)
	}

	// Continue the same conversation.
	rec = e.do("POST", "/public/businesses/shop/chat", `{"conversationId":"`+out.ConversationID+`","content":"謝謝"}`)
	if rec.Code != 200 || len(e.chats.msgs) != 2 {
		t.Fatalf("continue: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, path, body string
		status           int
		code             string
	}{
		{"empty", "shop", `{"content":"   "}`, 400, CodeInvalidRequest},
		{"bad json", "shop", `{`, 400, CodeInvalidRequest},
		{"unknown field", "shop", `{"content":"hi","x":1}`, 400, CodeInvalidRequest},
		{"too long", "shop", `{"content":"` + strings.Repeat("字", MaxMessageRunes+1) + `"}`, 400, CodeContentTooLong},
		{"huge body", "shop", `{"content":"` + strings.Repeat("a", maxChatBody+10) + `"}`, 413, CodeContentTooLong},
		{"no business", "nope", `{"content":"hi"}`, 404, CodeNotFound},
		{"unprovisioned", "bare", `{"content":"hi"}`, 503, CodeUnavailable},
		{"unknown conversation", "shop", `{"conversationId":"zzz","content":"hi"}`, 404, CodeConversationNotFound},
	}
	for _, c := range cases {
		rec := e.do("POST", "/public/businesses/"+c.path+"/chat", c.body)
		if rec.Code != c.status || errCode(t, rec) != c.code {
			t.Errorf("%s: got %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
	if len(e.chats.msgs) != 0 || len(e.quota.records) != 0 {
		t.Fatal("nothing should have been stored or billed")
	}
	// exactly at the limit is fine
	if rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"`+strings.Repeat("字", MaxMessageRunes)+`"}`); rec.Code != 200 {
		t.Fatalf("at limit: %d", rec.Code)
	}
}

func TestChatConversationBelongsToBusiness(t *testing.T) {
	e := newEnv(t)
	id, _ := e.chats.Create(2) // belongs to "other"
	rec := e.do("POST", "/public/businesses/shop/chat", `{"conversationId":"`+id+`","content":"hi"}`)
	if rec.Code != 404 || errCode(t, rec) != CodeConversationNotFound {
		t.Fatalf("cross-business: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatBlockedWhenOverQuota(t *testing.T) {
	e := newEnv(t)
	e.quota.allowed = false
	rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"hi"}`)
	if rec.Code != 429 || errCode(t, rec) != CodeQuotaExceeded {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if len(e.chats.msgs) != 0 || len(e.chats.convs) != 0 || len(e.quota.records) != 0 {
		t.Fatal("over-quota message must not be stored, nor start a conversation")
	}
}

func TestChatQuotaErrorFailsClosedWithoutLeaking(t *testing.T) {
	e := newEnv(t)
	e.quota.err = errors.New("pq: connection to db-internal-host refused")
	rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"hi"}`)
	if rec.Code != 500 || errCode(t, rec) != CodeInternal || strings.Contains(rec.Body.String(), "db-internal-host") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestChatRateLimits(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 30; i++ {
		if rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"hi"}`); rec.Code != 200 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"hi"}`)
	if rec.Code != 429 || errCode(t, rec) != CodeRateLimited {
		t.Fatalf("ip limit: %d %s", rec.Code, rec.Body.String())
	}

	// per-conversation limit, from a fresh IP each time
	e = newEnv(t)
	id, _ := e.chats.Create(1)
	var last *httptest.ResponseRecorder
	for i := 0; i < 11; i++ {
		req := httptest.NewRequest("POST", "/public/businesses/shop/chat", strings.NewReader(`{"conversationId":"`+id+`","content":"hi"}`))
		req.RemoteAddr = "198.51.100." + string(rune('1'+i)) + ":1"
		last = httptest.NewRecorder()
		e.mux.ServeHTTP(last, req)
	}
	if last.Code != 429 || errCode(t, last) != CodeRateLimited {
		t.Fatalf("conversation limit: %d %s", last.Code, last.Body.String())
	}
}

func TestConversationMessageCap(t *testing.T) {
	e := newEnv(t)
	id, _ := e.chats.Create(1)
	for i := 0; i < MaxMessagesPerConversation; i++ {
		e.chats.AddUserMessage(id, "x")
	}
	rec := e.do("POST", "/public/businesses/shop/chat", `{"conversationId":"`+id+`","content":"hi"}`)
	if rec.Code != 429 || errCode(t, rec) != CodeConversationFull {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestReplyFlow(t *testing.T) {
	e := newEnv(t)
	rec := e.do("POST", "/public/businesses/shop/chat", `{"content":"hello"}`)
	var chat chatResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &chat)
	body := func(conv string, id int64, content string) string {
		b, _ := json.Marshal(replyRequest{ConversationID: conv, MessageID: id, Content: content})
		return string(b)
	}

	// client-claimed usage fields are rejected outright (unknown field)
	if rec := e.do("POST", "/public/businesses/shop/chat/reply", `{"conversationId":"x","messageId":1,"content":"a","usage":{"totalTokens":1}}`); rec.Code != 400 {
		t.Fatalf("client usage accepted: %d", rec.Code)
	}

	rec = e.do("POST", "/public/businesses/shop/chat/reply", body(chat.ConversationID, chat.MessageID, "我們早上九點開門。"))
	if rec.Code != 200 {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body.String())
	}
	last := e.chats.msgs[len(e.chats.msgs)-1]
	if last.Role != "assistant" || last.Content != "我們早上九點開門。" {
		t.Fatalf("stored %+v", last)
	}
	// usage: prompt event + completion event, estimated server-side (9 runes -> 5)
	if len(e.quota.records) != 2 || e.quota.records[1].usage.CompletionTokens != 5 || e.quota.records[1].userID != 10 {
		t.Fatalf("usage %+v", e.quota.records)
	}

	// second reply to the same message is refused and not billed
	rec = e.do("POST", "/public/businesses/shop/chat/reply", body(chat.ConversationID, chat.MessageID, "again"))
	if rec.Code != 409 || errCode(t, rec) != CodeAlreadyReplied || len(e.quota.records) != 2 {
		t.Fatalf("dup: %d %s", rec.Code, rec.Body.String())
	}
	// unknown message id, wrong conversation, other business's slug
	if rec = e.do("POST", "/public/businesses/shop/chat/reply", body(chat.ConversationID, 999, "x")); rec.Code != 404 {
		t.Fatalf("unknown msg: %d", rec.Code)
	}
	if rec = e.do("POST", "/public/businesses/shop/chat/reply", body("nope", chat.MessageID, "x")); rec.Code != 404 {
		t.Fatalf("unknown conv: %d", rec.Code)
	}
	if rec = e.do("POST", "/public/businesses/other/chat/reply", body(chat.ConversationID, chat.MessageID, "x")); rec.Code != 404 {
		t.Fatalf("cross business: %d", rec.Code)
	}
	if rec = e.do("POST", "/public/businesses/shop/chat/reply", body(chat.ConversationID, chat.MessageID, "")); rec.Code != 400 {
		t.Fatalf("empty: %d", rec.Code)
	}
	if rec = e.do("POST", "/public/businesses/shop/chat/reply", body(chat.ConversationID, chat.MessageID, strings.Repeat("字", MaxReplyRunes+1))); rec.Code != 400 {
		t.Fatalf("too long: %d", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	e := newEnv(t)
	root := http.NewServeMux()
	root.Handle("/public/", CORS(func(o string) bool { return o == "http://localhost:5178" })(e.mux))

	req := httptest.NewRequest("OPTIONS", "/public/businesses/shop/chat", nil)
	req.Header.Set("Origin", "http://localhost:5178")
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5178" {
		t.Fatalf("allowed preflight: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("must not allow credentials")
	}

	req = httptest.NewRequest("GET", "/public/businesses/shop", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("evil origin echoed: %q", got)
	}
}

func TestLimiterWindowResets(t *testing.T) {
	l := newLimiter(2, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	if !l.allow("k") || !l.allow("k") || l.allow("k") {
		t.Fatal("limit not enforced")
	}
	now = now.Add(61 * time.Second)
	if !l.allow("k") {
		t.Fatal("window should reset")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:99"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if clientIP(r, false) != "10.0.0.1" {
		t.Fatal("untrusted proxy: must use RemoteAddr")
	}
	if clientIP(r, true) != "2.2.2.2" {
		t.Fatal("trusted proxy: right-most entry")
	}
}
