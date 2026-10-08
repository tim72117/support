//go:build integration

package console

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Owner settings beyond the basics in console_integration_test.go: the look
// of a business (name, tagline, mascot, colour), partial updates, and the
// content editor's saved state.

type bizFull struct {
	ID         int64
	OwnerID    int64
	Slug       string
	Name       string
	Tagline    string
	Mascot     string
	ThemeColor string
	Layout     string
}

func (c *client) createFull(slug string, extra map[string]string) (int, bizFull, string) {
	body := map[string]string{"slug": slug, "name": "測試服務"}
	for k, v := range extra {
		body[k] = v
	}
	code, raw, _ := c.do("POST", "/console/businesses", body)
	var b bizFull
	_ = json.Unmarshal([]byte(raw), &b)
	return code, b, raw
}

func TestCreateBusinessWithLook(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())

	code, b, raw := c.createFull(uniqueSlug(), map[string]string{"name": "  選民服務  ", "tagline": " 24 小時回覆 ", "mascot": "bear", "themeColor": "#4ECDC4", "layout": "split"})
	if code != 200 || b.Name != "選民服務" || b.Tagline != "24 小時回覆" || b.Mascot != "bear" || b.ThemeColor != "#4ECDC4" || b.Layout != "split" {
		t.Fatalf("create with look: %d %s", code, raw)
	}

	// Defaults when the look is omitted.
	_, d, raw := c.createFull(uniqueSlug(), nil)
	if d.Mascot != "fox" || d.ThemeColor != "#FF8A5B" || d.Tagline != "" || d.Layout != "center" {
		t.Errorf("defaults: %s", raw)
	}

	// The look survives a round trip through GET and the list.
	_, got, _ := func() (int, bizFull, string) {
		code, body, _ := c.do("GET", fmt.Sprintf("/console/businesses/%d", b.ID), nil)
		var x bizFull
		_ = json.Unmarshal([]byte(body), &x)
		return code, x, body
	}()
	if got.Mascot != "bear" || got.ThemeColor != "#4ECDC4" || got.Tagline != "24 小時回覆" || got.Layout != "split" {
		t.Errorf("GET lost the look: %+v", got)
	}
}

func TestCreateBusinessRejectsBadLook(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())

	for name, extra := range map[string]map[string]string{
		"unknown mascot":   {"mascot": "dragon"},
		"color without #":  {"themeColor": "FF8A5B"},
		"short color":      {"themeColor": "#FFF"},
		"css injection":    {"themeColor": "red;background:url(x)"},
		"non-hex color":    {"themeColor": "#GGGGGG"},
		"overlong tagline": {"tagline": strings.Repeat("字", 81)},
		"overlong name":    {"name": strings.Repeat("字", 61)},
		"unknown layout":   {"layout": "sidebar"},
	} {
		if code, _, raw := c.createFull(uniqueSlug(), extra); code != 400 {
			t.Errorf("%s: %d %s, want 400", name, code, raw)
		}
	}
	if code, _, raw := c.createFull(strings.Repeat("a", 41), nil); code != 400 {
		t.Errorf("41-character slug: %d %s, want 400", code, raw)
	}
	if code, _, raw := c.createFull(strings.Repeat("a", 40)+"", nil); code != 200 {
		t.Errorf("40-character slug should be fine: %d %s", code, raw)
	}
}

func TestUpdateBusinessLook(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())
	slug := uniqueSlug()
	_, b, _ := c.createFull(slug, map[string]string{"name": "原名", "tagline": "原簡介", "mascot": "cat", "themeColor": "#8E7DFF"})
	path := fmt.Sprintf("/console/businesses/%d", b.ID)

	// Partial: only the name changes, everything else stays.
	code, body, _ := c.do("PATCH", path, map[string]string{"name": "  新名字  "})
	var u bizFull
	_ = json.Unmarshal([]byte(body), &u)
	if code != 200 || u.Name != "新名字" || u.Tagline != "原簡介" || u.Mascot != "cat" || u.ThemeColor != "#8E7DFF" || u.Layout != "center" {
		t.Fatalf("partial patch: %d %s", code, body)
	}

	// Clearing the tagline is allowed (empty string is a value, not "absent").
	_, body, _ = c.do("PATCH", path, map[string]string{"tagline": ""})
	_ = json.Unmarshal([]byte(body), &u)
	if u.Tagline != "" || u.Name != "新名字" {
		t.Errorf("clearing the tagline: %s", body)
	}

	// Everything at once.
	_, body, _ = c.do("PATCH", path, map[string]string{"name": "三", "tagline": "四", "mascot": "bird", "themeColor": "#4C9EFF", "layout": "split"})
	_ = json.Unmarshal([]byte(body), &u)
	if u.Name != "三" || u.Tagline != "四" || u.Mascot != "bird" || u.ThemeColor != "#4C9EFF" || u.Layout != "split" {
		t.Errorf("full patch: %s", body)
	}

	// The slug is the public URL; it cannot be changed through this call.
	_, body, _ = c.do("PATCH", path, map[string]string{"slug": "hijacked", "name": "仍然是三"})
	_ = json.Unmarshal([]byte(body), &u)
	if u.Slug != slug {
		t.Errorf("slug changed to %q; links already shared would break", u.Slug)
	}

	// Rejections leave everything as it was.
	for name, in := range map[string]map[string]string{
		"empty name":   {"name": "   "},
		"bad mascot":   {"mascot": "robot"},
		"bad color":    {"themeColor": "blue"},
		"long tagline": {"tagline": strings.Repeat("a", 81)},
		"bad layout":   {"layout": "sidebar"},
	} {
		if code, body, _ := c.do("PATCH", path, in); code != 400 {
			t.Errorf("%s: %d %s, want 400", name, code, body)
		}
	}
	if code, _, _ := c.do("PATCH", path, "{bad"); code != 400 {
		t.Errorf("malformed body = %d, want 400", code)
	}
	_, body, _ = c.do("GET", path, nil)
	_ = json.Unmarshal([]byte(body), &u)
	if u.Name != "仍然是三" || u.Mascot != "bird" {
		t.Errorf("a rejected patch changed the business: %s", body)
	}
}

func TestUpdateBusinessIsOwnerOnly(t *testing.T) {
	s := newServer(t, true)
	alice, mallory := s.newClient(), s.newClient()
	alice.register(uniqueEmail())
	mallory.register(uniqueEmail())
	_, b, _ := alice.createFull(uniqueSlug(), map[string]string{"name": "Alice"})
	path := fmt.Sprintf("/console/businesses/%d", b.ID)

	code, _, _ := mallory.do("PATCH", path, map[string]string{"name": "pwned"})
	if code != 404 {
		t.Errorf("PATCH as another user = %d, want 404", code)
	}
	if code, _, _ := s.newClient().do("PATCH", path, map[string]string{"name": "pwned"}); code != 401 {
		t.Errorf("PATCH without login = %d, want 401", code)
	}
	if _, body, _ := alice.do("GET", path, nil); !strings.Contains(body, "Alice") {
		t.Errorf("name changed by a non-owner: %s", body)
	}
}

func TestContentKeepsEditorState(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())
	_, b, _ := c.createFull(uniqueSlug(), nil)
	path := fmt.Sprintf("/console/businesses/%d/content", b.ID)

	sections := `[{"id":"hours","title":"營業時間","body":"每天 9:00–18:00"},{"id":"faq","title":"常見問題","body":""}]`
	put := fmt.Sprintf(`{"content":"【營業時間】\n每天 9:00–18:00","sections":%s}`, sections)
	if code, body, _ := c.do("PUT", path, put); code != 204 {
		t.Fatalf("put: %d %s", code, body)
	}

	_, body, _ := c.do("GET", path, nil)
	var got struct {
		Content  string
		Sections json.RawMessage
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Content, "營業時間") {
		t.Errorf("content = %q", got.Content)
	}
	var a, e any
	_ = json.Unmarshal(got.Sections, &a)
	_ = json.Unmarshal([]byte(sections), &e)
	if fmt.Sprint(a) != fmt.Sprint(e) {
		t.Errorf("sections must round-trip unchanged:\n got  %s\n want %s", got.Sections, sections)
	}

	// PUT replaces both: leaving sections out clears the stale editor state.
	if code, _, _ := c.do("PUT", path, `{"content":"只有純文字"}`); code != 204 {
		t.Fatal("second put failed")
	}
	_, body, _ = c.do("GET", path, nil)
	if !strings.Contains(body, `"Sections":null`) || !strings.Contains(body, "只有純文字") {
		t.Errorf("after a put without sections: %s", body)
	}
	if code, _, _ := c.do("PUT", path, `{"content":"x","sections":null}`); code != 204 {
		t.Error("explicit null sections should be accepted")
	}

	// A fresh business reports null, not an error.
	_, b2, _ := c.createFull(uniqueSlug(), nil)
	if _, body, _ := c.do("GET", fmt.Sprintf("/console/businesses/%d/content", b2.ID), nil); !strings.Contains(body, `"Sections":null`) {
		t.Errorf("fresh content = %s", body)
	}
}

func TestContentRejectsOversizedSections(t *testing.T) {
	s := newServer(t, true)
	c := s.newClient()
	c.register(uniqueEmail())
	_, b, _ := c.createFull(uniqueSlug(), nil)
	path := fmt.Sprintf("/console/businesses/%d/content", b.ID)

	big := fmt.Sprintf(`{"content":"x","sections":["%s"]}`, strings.Repeat("a", 70<<10))
	if code, _, _ := c.do("PUT", path, big); code != 400 {
		t.Errorf("oversized sections = %d, want 400", code)
	}
	if _, body, _ := c.do("GET", path, nil); strings.Contains(body, strings.Repeat("a", 100)) {
		t.Error("rejected sections must not be stored")
	}
}
