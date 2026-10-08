//go:build integration

package public_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/console"
	"github.com/tim72117/ai-support/internal/conversation"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/public"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/reservation"
	"github.com/tim72117/ai-support/internal/session"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/public
//
// This no longer provisions anything on onagent: every business shares one
// fixed onagent app/key (ONAGENT_APP_ID/ONAGENT_APP_KEY in a real
// deployment, provisioned by hand with the onagent CLI), so there is no
// console-side app/key/thought push to fake or verify here anymore — the
// fixed app id/key below stand in for whatever a real deployment configures.
func TestPublicChatEndToEnd(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UnixNano()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	const sharedAppID, sharedAppKey = "shared-app", "shared-key"

	sessions := session.New(gdb, false)
	owner, err := sessions.Register(fmt.Sprintf("pub-%d@example.com", suffix), "password123")
	if err != nil {
		t.Fatal(err)
	}
	other, err := sessions.Register(fmt.Sprintf("pub-other-%d@example.com", suffix), "password123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		gdb.Exec("DELETE FROM users WHERE id IN (?, ?)", owner.ID, other.ID)
	})

	biz := business.New(gdb)
	quotaSvc := quota.New(gdb)
	chats := conversation.New(gdb)

	ch := console.NewHandler(biz, sessions, quotaSvc, log)
	ch.Chats = chats
	consoleMux := http.NewServeMux()
	ch.Register(consoleMux)

	cookieFor := func(uid int64) *http.Cookie {
		rec := httptest.NewRecorder()
		if _, err := sessions.CreateSession(rec, uid); err != nil {
			t.Fatal(err)
		}
		return rec.Result().Cookies()[0]
	}
	consoleDo := func(uid int64, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(cookieFor(uid))
		rec := httptest.NewRecorder()
		consoleMux.ServeHTTP(rec, req)
		return rec
	}

	// 1. owner creates a business: the onagent app is provisioned.
	slug := fmt.Sprintf("it-%d", suffix)
	rec := consoleDo(owner.ID, "POST", "/console/businesses", fmt.Sprintf(`{"slug":%q,"name":"整合測試店"}`, slug))
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID int64 }
	json.Unmarshal(rec.Body.Bytes(), &created)
	t.Cleanup(func() { gdb.Exec("DELETE FROM businesses WHERE id = ?", created.ID) })

	// 2. saving content: no onagent push happens anywhere anymore (every
	// business shares one fixed app — there is nothing per-business to
	// provision or sync). The content is only reachable through the
	// sections tool-backing API below.
	sectionsBody := `[{"id":"hours","title":"營業時間","body":"週一公休，其餘 9-18 點。"},{"id":"other","title":"其他","body":""}]`
	rec = consoleDo(owner.ID, "PUT", fmt.Sprintf("/console/businesses/%d/content", created.ID),
		fmt.Sprintf(`{"Content":"週一公休，其餘 9-18 點。","Sections":%s}`, sectionsBody))
	if rec.Code != 204 {
		t.Fatalf("put content: %d %s", rec.Code, rec.Body.String())
	}

	// 3. public API. OnagentAppID/OnagentAppKey/OnagentWSURL stand in for
	// what a real deployment reads from ONAGENT_APP_ID/ONAGENT_APP_KEY/
	// ONAGENT_WS_URL — the same fixed values for every business.
	pub := public.NewHandler(public.Config{
		Businesses: biz, Chats: chats, Quota: quotaSvc,
		OnagentWSURL: "ws://onagent.test/ws", OnagentAppID: sharedAppID, OnagentAppKey: sharedAppKey,
		Log: log,
	})
	pubMux := http.NewServeMux()
	pub.Register(pubMux)
	pubDo := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.9:1"
		rec := httptest.NewRecorder()
		pubMux.ServeHTTP(rec, req)
		return rec
	}

	rec = pubDo("GET", "/public/businesses/"+slug, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), sharedAppKey) || !strings.Contains(rec.Body.String(), `"wsUrl":"ws://`) {
		t.Fatalf("get business: %d %s", rec.Code, rec.Body.String())
	}

	// 2b. list_sections/read_section tool-backing API: only the non-empty
	// "hours" section shows up (the empty "other" section was filtered out),
	// list_sections omits the body, and read_section returns it in full.
	rec = pubDo("GET", "/public/businesses/"+slug+"/sections", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "週一公休") || !strings.Contains(rec.Body.String(), "營業時間") {
		t.Fatalf("list sections: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"id":"other"`) {
		t.Fatalf("empty section must not be listed: %s", rec.Body.String())
	}
	rec = pubDo("GET", "/public/businesses/"+slug+"/sections/hours", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "週一公休") {
		t.Fatalf("read section: %d %s", rec.Code, rec.Body.String())
	}
	if rec = pubDo("GET", "/public/businesses/"+slug+"/sections/nope", ""); rec.Code != 404 {
		t.Fatalf("unknown section: %d %s", rec.Code, rec.Body.String())
	}
	if rec = pubDo("GET", "/public/businesses/"+slug+"/sections/other", ""); rec.Code != 404 {
		t.Fatalf("empty section read: %d %s", rec.Code, rec.Body.String())
	}

	rec = pubDo("POST", "/public/businesses/"+slug+"/chat", `{"content":"幾點開門？"}`)
	if rec.Code != 200 {
		t.Fatalf("chat: %d %s", rec.Code, rec.Body.String())
	}
	var chat struct {
		ConversationID string
		MessageID      int64
		Content        string
	}
	json.Unmarshal(rec.Body.Bytes(), &chat)
	if chat.ConversationID == "" || chat.MessageID == 0 || chat.Content != "幾點開門？" {
		t.Fatalf("chat resp %+v", chat)
	}
	rec = pubDo("POST", "/public/businesses/"+slug+"/chat/reply",
		fmt.Sprintf(`{"conversationId":%q,"messageId":%d,"content":"9 點到 18 點。"}`, chat.ConversationID, chat.MessageID))
	if rec.Code != 200 {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body.String())
	}
	if rec = pubDo("POST", "/public/businesses/"+slug+"/chat/reply",
		fmt.Sprintf(`{"conversationId":%q,"messageId":%d,"content":"x"}`, chat.ConversationID, chat.MessageID)); rec.Code != 409 {
		t.Fatalf("second reply: %d", rec.Code)
	}

	// usage landed in the owner's ledger (estimated: 5 runes->3, 9 runes->5)
	st, err := quotaSvc.StandingFor(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Used != 8 {
		t.Fatalf("used = %d, want 8", st.Used)
	}

	// 4. owner reads the transcript; another owner gets 404s.
	rec = consoleDo(owner.ID, "GET", fmt.Sprintf("/console/businesses/%d/conversations", created.ID), "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), chat.ConversationID) {
		t.Fatalf("list conversations: %d %s", rec.Code, rec.Body.String())
	}
	rec = consoleDo(owner.ID, "GET", fmt.Sprintf("/console/businesses/%d/conversations/%s", created.ID, chat.ConversationID), "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "幾點開門") || !strings.Contains(rec.Body.String(), "9 點到 18 點") {
		t.Fatalf("get conversation: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{
		fmt.Sprintf("/console/businesses/%d/conversations", created.ID),
		fmt.Sprintf("/console/businesses/%d/conversations/%s", created.ID, chat.ConversationID),
	} {
		if rec = consoleDo(other.ID, "GET", p, ""); rec.Code != 404 {
			t.Fatalf("other owner %s: %d", p, rec.Code)
		}
	}
	// a conversation of someone else's business under my own business id is 404 too
	otherBiz, err := biz.Create(other.ID, fmt.Sprintf("it-o-%d", suffix), "Other")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM businesses WHERE id = ?", otherBiz.ID) })
	if rec = consoleDo(other.ID, "GET", fmt.Sprintf("/console/businesses/%d/conversations/%s", otherBiz.ID, chat.ConversationID), ""); rec.Code != 404 {
		t.Fatalf("foreign conversation via own business: %d", rec.Code)
	}

	// 5. exhausted quota blocks chat and stores nothing.
	gdb.Exec("UPDATE subscriptions SET monthly_quota = 5 WHERE user_id = ?", owner.ID)
	rec = pubDo("POST", "/public/businesses/"+slug+"/chat", `{"conversationId":"`+chat.ConversationID+`","content":"還有嗎"}`)
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "quota_exceeded") {
		t.Fatalf("over quota: %d %s", rec.Code, rec.Body.String())
	}
	msgs, _ := chats.Messages(chat.ConversationID)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}

	// 6. deleting the business removes the transcript but not the usage ledger.
	if rec = consoleDo(owner.ID, "DELETE", fmt.Sprintf("/console/businesses/%d", created.ID), ""); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := chats.Get(chat.ConversationID); err == nil {
		t.Fatal("conversation should be gone with its business")
	}
	if st, _ := quotaSvc.StandingFor(t.Context(), owner.ID); st.Used != 8 {
		t.Fatalf("ledger changed after delete: %d", st.Used)
	}
}

// Covers the marketing site's anonymous "book a demo / talk to sales" lead
// form: a normal submission, each required-field error, and the per-IP rate
// limit (set to 10/min in NewHandler).
func TestPublicReservations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := reservation.New(gdb)
	pub := public.NewHandler(public.Config{Reservations: store, Log: log})
	mux := http.NewServeMux()
	pub.Register(mux)

	var ids []int64
	t.Cleanup(func() {
		for _, id := range ids {
			gdb.Exec("DELETE FROM reservations WHERE id = ?", id)
		}
	})

	doFrom := func(ip, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/public/reservations", strings.NewReader(body))
		req.RemoteAddr = ip + ":1"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	suffix := time.Now().UnixNano()
	contact := fmt.Sprintf("lead-%d@example.com", suffix)

	// 1. a normal submission succeeds and is stored.
	rec := doFrom("198.51.100.1", fmt.Sprintf(
		`{"productLine":"candidate","tier":"starter","name":"王小明","contact":%q,"message":"想了解細節"}`, contact))
	if rec.Code != 200 {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID int64 }
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == 0 {
		t.Fatalf("no id in response: %s", rec.Body.String())
	}
	ids = append(ids, created.ID)

	// 2. missing contact (and missing name) are rejected as invalid_request.
	if rec = doFrom("198.51.100.2", `{"productLine":"business","name":"Jane"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("missing contact: %d %s", rec.Code, rec.Body.String())
	}
	if rec = doFrom("198.51.100.2", `{"productLine":"business","contact":"x@x.com"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("missing name: %d %s", rec.Code, rec.Body.String())
	}

	// 3. invalid product line is also invalid_request.
	if rec = doFrom("198.51.100.2", `{"productLine":"nope","name":"x","contact":"x@x.com"}`); rec.Code != 400 {
		t.Fatalf("invalid product line: %d %s", rec.Code, rec.Body.String())
	}

	// 4. rate limit: ipReserve allows 10/min; the 11th request from the same
	// IP within the window is rejected even though earlier ones in this test
	// already consumed some of that IP's budget.
	limited := "198.51.100.3"
	var lastCode int
	for i := 0; i < 11; i++ {
		rec = doFrom(limited, fmt.Sprintf(`{"productLine":"business","name":"x","contact":"x%d@x.com"}`, i))
		lastCode = rec.Code
		if rec.Code == 200 {
			var r struct{ ID int64 }
			json.Unmarshal(rec.Body.Bytes(), &r)
			ids = append(ids, r.ID)
		}
	}
	if lastCode != 429 {
		t.Fatalf("expected the 11th request to be rate limited, got %d", lastCode)
	}
}

// TestCreateBusinessSurvivesOnagentFailure (the old "retry provisioning"
// test) no longer applies: there is nothing to provision per business —
// every business shares the one onagent app configured once via
// ONAGENT_APP_ID/ONAGENT_APP_KEY — so createBusiness has no onagent call
// left that could fail, and the POST .../onagent-sync retry route is gone
// too. See TestPublicChatEndToEnd above for the current create/save flow.
