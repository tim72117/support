//go:build integration

package billing

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
	"github.com/tim72117/ai-support/internal/tappay"
)

// HTTP-level tests of /console/billing/*. TapPay is replaced by fakeGateway
// (see billing_integration_test.go), so nothing leaves the process.

const (
	testPartnerKey = "PARTNER-KEY-MUST-NOT-LEAK"
	testAppKey     = "app_key_public"
)

type httpEnv struct {
	*env
	srv      *httptest.Server
	sessions *session.Store
	cookie   *http.Cookie
}

func newHTTPEnv(t *testing.T, enabled bool) *httpEnv {
	t.Helper()
	e := setup(t)
	sessions := session.New(e.gdb, false)

	var svc *Service
	if enabled {
		svc = e.svc
	}
	cfg := tappay.Config{AppID: "12345", AppKey: testAppKey, PartnerKey: testPartnerKey, MerchantID: "m_test"}
	h := NewHandler(svc, sessions, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rec := httptest.NewRecorder()
	if _, err := sessions.CreateSession(rec, e.userID); err != nil {
		t.Fatal(err)
	}
	return &httpEnv{env: e, srv: srv, sessions: sessions, cookie: rec.Result().Cookies()[0]}
}

func (h *httpEnv) do(t *testing.T, method, path string, body any, loggedIn bool) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			rd = strings.NewReader(s)
		} else {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if loggedIn {
		req.AddCookie(h.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(raw))
}

type subReq map[string]any

func subscribeBody(tier string) subReq {
	return subReq{
		"tier": tier, "prime": "prime_x",
		"cardholder": map[string]string{"name": "王小明", "email": "w@example.com", "phoneNumber": "0912345678"},
	}
}

func TestBillingConfigAndPlans(t *testing.T) {
	h := newHTTPEnv(t, true)

	// Public: no login needed (the subscribe page asks before an account exists).
	code, body := h.do(t, "GET", "/console/billing/config", nil, false)
	if code != 200 {
		t.Fatalf("config = %d", code)
	}
	var cfg struct {
		Enabled bool
		AppID   string
		AppKey  string
		Env     string
	}
	_ = json.Unmarshal([]byte(body), &cfg)
	if !cfg.Enabled || cfg.AppID != "12345" || cfg.AppKey != testAppKey || cfg.Env != "sandbox" {
		t.Errorf("config = %s", body)
	}
	if strings.Contains(body, testPartnerKey) || strings.Contains(body, "m_test") {
		t.Fatalf("config leaks a server-side secret: %s", body)
	}

	code, body = h.do(t, "GET", "/console/billing/plans", nil, false)
	var plans []struct {
		Tier   string
		Name   string
		Amount int
	}
	_ = json.Unmarshal([]byte(body), &plans)
	// Prices() lists every sellable plan across both product lines (see
	// prices.go), not just the candidate line this test otherwise exercises
	// — so this asserts the candidate plans are present and correct rather
	// than that they're the only ones.
	byTier := map[string]struct {
		Tier   string
		Name   string
		Amount int
	}{}
	for _, p := range plans {
		byTier[p.Tier] = p
	}
	if code != 200 || len(plans) != 4 || byTier["starter"].Amount != 990 || byTier["campaign"].Amount != 2990 {
		t.Errorf("plans = %d %s", code, body)
	}
}

func TestBillingDisabledWithoutTapPay(t *testing.T) {
	h := newHTTPEnv(t, false)
	if _, body := h.do(t, "GET", "/console/billing/config", nil, false); !strings.Contains(body, `"enabled":false`) {
		t.Errorf("config = %s, want enabled:false", body)
	}
	if code, _ := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true); code != 503 {
		t.Errorf("subscribe without TapPay = %d, want 503", code)
	}
	if code, _ := h.do(t, "POST", "/console/billing/cancel", nil, true); code != 503 {
		t.Errorf("cancel without TapPay = %d, want 503", code)
	}
	if h.gw.primeCall != 0 {
		t.Error("no gateway call may happen when billing is disabled")
	}
}

func TestBillingRoutesRequireLogin(t *testing.T) {
	h := newHTTPEnv(t, true)
	for _, r := range []struct{ method, path string }{
		{"GET", "/console/billing/subscription"},
		{"POST", "/console/billing/subscribe"},
		{"POST", "/console/billing/cancel"},
	} {
		if code, _ := h.do(t, r.method, r.path, subscribeBody("campaign"), false); code != 401 {
			t.Errorf("%s %s without login = %d, want 401", r.method, r.path, code)
		}
	}
	if h.gw.primeCall != 0 {
		t.Error("an unauthenticated request must never reach the gateway")
	}
}

func TestSubscribeOverHTTP(t *testing.T) {
	h := newHTTPEnv(t, true)

	if code, body := h.do(t, "GET", "/console/billing/subscription", nil, true); code != 200 || body != "null" {
		t.Errorf("before subscribing: %d %q, want 200 null", code, body)
	}

	code, body := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true)
	if code != 200 {
		t.Fatalf("subscribe = %d %s", code, body)
	}
	for _, secret := range []string{"ck", "ct", "card_key", "card_token", "cardKey", "cardToken", testPartnerKey} {
		if strings.Contains(body, `"`+secret+`"`) || strings.Contains(body, ":\""+secret+"\"") {
			t.Errorf("response exposes card secret %q: %s", secret, body)
		}
	}
	var p Profile
	_ = json.Unmarshal([]byte(body), &p)
	if p.Tier != "campaign" || p.Status != StatusActive || p.CardLastFour != "4242" {
		t.Errorf("profile = %s", body)
	}
	if h.tier(t) != quota.TierCampaign {
		t.Error("a successful payment must move the owner onto the paid tier")
	}

	// What reached TapPay: the server-side price and the browser's prime.
	if h.gw.lastPrime.Amount != 2990 || h.gw.lastPrime.Prime != "prime_x" || !h.gw.lastPrime.Remember {
		t.Errorf("gateway request = %+v", h.gw.lastPrime)
	}
	if h.gw.lastPrime.Cardholder.Name != "王小明" || h.gw.lastPrime.Cardholder.PhoneNumber != "0912345678" {
		t.Errorf("cardholder = %+v", h.gw.lastPrime.Cardholder)
	}

	// GET /subscription reflects it, still without secrets.
	_, body = h.do(t, "GET", "/console/billing/subscription", nil, true)
	if !strings.Contains(body, `"status":"active"`) || strings.Contains(body, "token") || strings.Contains(strings.ToLower(body), "cardkey") {
		t.Errorf("subscription = %s", body)
	}
}

func TestSubscribeIgnoresClientAmount(t *testing.T) {
	h := newHTTPEnv(t, true)
	body := subscribeBody("campaign")
	body["amount"] = 1 // a tampering attempt
	if code, resp := h.do(t, "POST", "/console/billing/subscribe", body, true); code != 200 {
		t.Fatalf("subscribe = %d %s", code, resp)
	}
	if h.gw.lastPrime.Amount != 2990 {
		t.Fatalf("charged %d; the amount must come from the server price table, never the client", h.gw.lastPrime.Amount)
	}
}

func TestSubscribeRejectsBadRequests(t *testing.T) {
	h := newHTTPEnv(t, true)
	cases := map[string]any{
		"unknown tier":         subscribeBody("ultra"),
		"free is not for sale": subscribeBody("free"),
		"empty tier":           subscribeBody(""),
		"missing prime":        subReq{"tier": "campaign", "prime": ""},
		"malformed json":       "{not json",
	}
	for name, body := range cases {
		code, resp := h.do(t, "POST", "/console/billing/subscribe", body, true)
		if code != 400 {
			t.Errorf("%s: %d %s, want 400", name, code, resp)
		}
	}
	if h.gw.primeCall != 0 {
		t.Error("an invalid request must never reach the gateway")
	}
	if h.tier(t) != quota.TierFree {
		t.Error("tier changed by a rejected request")
	}
}

func TestSubscribeFailureStatuses(t *testing.T) {
	t.Run("decline is 402 and leaves the free tier", func(t *testing.T) {
		h := newHTTPEnv(t, true)
		h.gw.primeRes = declined
		code, body := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true)
		if code != 402 {
			t.Fatalf("%d %s, want 402", code, body)
		}
		if h.tier(t) != quota.TierFree {
			t.Error("tier changed although the card was declined")
		}
	})
	t.Run("gateway timeout is 502 with a do-not-retry message", func(t *testing.T) {
		h := newHTTPEnv(t, true)
		h.gw.primeErr = errors.New("connection reset by peer 10.0.0.1")
		code, body := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true)
		if code != 502 {
			t.Fatalf("%d %s, want 502", code, body)
		}
		if strings.Contains(body, "10.0.0.1") || strings.Contains(body, "connection reset") {
			t.Errorf("internal detail leaked to the browser: %s", body)
		}
		// An immediate retry must not charge again.
		calls := h.gw.primeCall
		if code, _ := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true); code != 409 {
			t.Errorf("retry while unresolved = %d, want 409", code)
		}
		if h.gw.primeCall != calls {
			t.Error("retry reached the gateway while the first outcome is unknown")
		}
	})
	t.Run("already subscribed is 409 and does not charge twice", func(t *testing.T) {
		h := newHTTPEnv(t, true)
		if code, _ := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("starter"), true); code != 200 {
			t.Fatal("setup subscribe failed")
		}
		calls := h.gw.primeCall
		if code, _ := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true); code != 409 {
			t.Errorf("second subscribe = %d, want 409", code)
		}
		if h.gw.primeCall != calls || h.tier(t) != quota.TierStarter {
			t.Error("a second subscribe must not charge or change the tier")
		}
	})
}

func TestCancelOverHTTP(t *testing.T) {
	h := newHTTPEnv(t, true)
	if code, _ := h.do(t, "POST", "/console/billing/cancel", nil, true); code != 404 {
		t.Errorf("cancel with no subscription = %d, want 404", code)
	}
	if code, _ := h.do(t, "POST", "/console/billing/subscribe", subscribeBody("campaign"), true); code != 200 {
		t.Fatal("setup subscribe failed")
	}
	code, body := h.do(t, "POST", "/console/billing/cancel", nil, true)
	if code != 200 || !strings.Contains(body, `"status":"canceled"`) {
		t.Fatalf("cancel = %d %s", code, body)
	}
	if h.tier(t) != quota.TierCampaign {
		t.Error("cancelling keeps the paid tier until the paid period ends")
	}
	if code, _ := h.do(t, "POST", "/console/billing/cancel", nil, true); code != 404 {
		t.Errorf("cancelling twice = %d, want 404", code)
	}
}

func TestStartTrialOverHTTP(t *testing.T) {
	h := newHTTPEnv(t, true)
	code, body := h.do(t, "POST", "/console/billing/start-trial", nil, true)
	if code != 200 || !strings.Contains(body, `"status":"trialing"`) {
		t.Fatalf("start-trial = %d %s", code, body)
	}
	if h.tier(t) != quota.TierCandidateTrial {
		t.Errorf("start-trial did not grant the trial tier: %s", h.tier(t))
	}
	if h.gw.primeCall != 0 {
		t.Error("start-trial must never call the payment gateway")
	}

	if code, _ := h.do(t, "POST", "/console/billing/start-trial", nil, true); code != 409 {
		t.Errorf("second start-trial = %d, want 409", code)
	}
}

func TestStartTrialRequiresLoginAndConfiguredBilling(t *testing.T) {
	h := newHTTPEnv(t, true)
	if code, _ := h.do(t, "POST", "/console/billing/start-trial", nil, false); code != 401 {
		t.Errorf("start-trial without login = %d, want 401", code)
	}

	disabled := newHTTPEnv(t, false)
	if code, _ := disabled.do(t, "POST", "/console/billing/start-trial", nil, true); code != 503 {
		t.Errorf("start-trial without TapPay configured = %d, want 503", code)
	}
}
