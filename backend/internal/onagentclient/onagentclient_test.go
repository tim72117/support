package onagentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

type recorded struct {
	Method, Path, Auth, ContentType string
	Body                            map[string]any
}

func fakeOnagent(t *testing.T, status int, respBody string) (*httptest.Server, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type")}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&rec.Body)
		}
		reqs = append(reqs, rec)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestDisabledClient(t *testing.T) {
	var nilClient *Client
	for _, c := range []*Client{nilClient, New(Config{}), New(Config{BaseURL: "http://x"})} {
		if c.Enabled() {
			t.Fatal("should be disabled")
		}
		if err := c.CreateApp(context.Background(), "a"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("CreateApp err = %v", err)
		}
		if _, err := c.IssueKey(context.Background(), "a"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("IssueKey err = %v", err)
		}
		if err := c.PushContent(context.Background(), "a", "n", "c"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("PushContent err = %v", err)
		}
		if c.WSURL() != "" {
			t.Fatal("WSURL should be empty")
		}
	}
}

func TestCreateAppRequestShape(t *testing.T) {
	srv, reqs := fakeOnagent(t, 201, `{"appId":"x"}`)
	c := New(Config{BaseURL: srv.URL + "/", Token: "tok-123"})
	if err := c.CreateApp(context.Background(), "aisupport-shop-ab12cd34"); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if r.Method != "POST" || r.Path != "/console/apps" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
	if r.Auth != "Bearer tok-123" || r.ContentType != "application/json" {
		t.Fatalf("headers: %+v", r)
	}
	if r.Body["appId"] != "aisupport-shop-ab12cd34" || r.Body["public"] != false {
		t.Fatalf("body: %+v", r.Body)
	}
}

func TestIssueKeyAndOrigins(t *testing.T) {
	srv, reqs := fakeOnagent(t, 200, `{"appId":"app1","apiKey":"secret-key"}`)
	c := New(Config{BaseURL: srv.URL, Token: "t", AllowedOrigins: []string{"http://localhost:5178", "https://x.example"}})
	key, err := c.IssueKey(context.Background(), "app1")
	if err != nil || key != "secret-key" {
		t.Fatalf("key=%q err=%v", key, err)
	}
	if err := c.SetOrigins(context.Background(), "app1"); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[0]; got.Method != "POST" || got.Path != "/console/apps/app1/key" {
		t.Fatalf("issue: %+v", got)
	}
	got := (*reqs)[1]
	if got.Method != "PUT" || got.Path != "/console/apps/app1/origin" {
		t.Fatalf("origin: %+v", got)
	}
	origins, _ := got.Body["origins"].([]any)
	if len(origins) != 2 || origins[0] != "http://localhost:5178" {
		t.Fatalf("origins body: %+v", got.Body)
	}
}

func TestPushContentSendsThought(t *testing.T) {
	srv, reqs := fakeOnagent(t, 200, `{}`)
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	if err := c.PushContent(context.Background(), "app1", "晨光烘焙坊", "週一公休"); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if r.Method != "PUT" || r.Path != "/console/apps/app1/thought" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
	th, _ := r.Body["thought"].(string)
	// The thought now only briefs the AI to use list_sections/read_section —
	// it must mention the business name and the two tools, but must NOT
	// carry the owner's actual content text (that only lives behind the
	// tools now, see PushContent's doc comment).
	if !strings.Contains(th, "晨光烘焙坊") {
		t.Fatalf("thought missing business name: %q", th)
	}
	if !strings.Contains(th, "list_sections") || !strings.Contains(th, "read_section") {
		t.Fatalf("thought missing tool names: %q", th)
	}
	if strings.Contains(th, "週一公休") {
		t.Fatalf("thought must not embed content text anymore: %q", th)
	}
}

func TestBuildThoughtMentionsEmptyContent(t *testing.T) {
	th := BuildThought("晨光烘焙坊", "")
	if !strings.Contains(th, "尚未填寫") {
		t.Fatalf("thought should flag empty content: %q", th)
	}
	th2 := BuildThought("晨光烘焙坊", "something")
	if strings.Contains(th2, "尚未填寫") {
		t.Fatalf("thought should not flag empty content when there is some: %q", th2)
	}
}

func TestPushContentTooLong(t *testing.T) {
	srv, reqs := fakeOnagent(t, 200, `{}`)
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	if err := c.PushContent(context.Background(), "a", "n", strings.Repeat("字", MaxContentRunes+1)); err == nil {
		t.Fatal("want error")
	}
	if len(*reqs) != 0 {
		t.Fatal("must not call onagent")
	}
}

func TestStatusErrorDoesNotLeakRequest(t *testing.T) {
	srv, _ := fakeOnagent(t, 400, "toolschema: appId \"x\" already exists\n")
	c := New(Config{BaseURL: srv.URL, Token: "tok-secret"})
	err := c.CreateApp(context.Background(), "x")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 400 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "tok-secret") {
		t.Fatal("token leaked into error")
	}
}

func TestWSURLAndAppID(t *testing.T) {
	if got := New(Config{BaseURL: "https://onagent.example.com/", Token: "t"}).WSURL(); got != "wss://onagent.example.com/ws" {
		t.Fatalf("wss: %q", got)
	}
	if got := New(Config{BaseURL: "http://localhost:8080", Token: "t"}).WSURL(); got != "ws://localhost:8080/ws" {
		t.Fatalf("ws: %q", got)
	}
	id := New(Config{BaseURL: "http://x", Token: "t"}).NewAppID("chenguang-bakery")
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`).MatchString(id) || !strings.HasPrefix(id, "aisupport-chenguang-bakery-") {
		t.Fatalf("app id %q", id)
	}
}
