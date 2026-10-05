package tappay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(Config{PartnerKey: "pk_test", MerchantID: "m_test"})
	c.baseURL = srv.URL
	return c
}

func TestPayByPrimeSuccess(t *testing.T) {
	var got map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tpc/payment/pay-by-prime" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "pk_test" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"status":0,"msg":"Success","rec_trade_id":"D1","bank_transaction_id":"B1",
			"order_number":"O1","amount":990,
			"card_secret":{"card_key":"ck","card_token":"ct"},"card_info":{"last_four":"4242"}}`))
	})

	res, err := c.PayByPrime(context.Background(), PrimeRequest{
		Prime: "prime_x", OrderNumber: "O1", Amount: 990, Details: "plan", Remember: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success() || res.RecTradeID != "D1" || res.CardKey != "ck" || res.CardToken != "ct" || res.LastFour != "4242" {
		t.Errorf("unexpected result: %+v", res)
	}
	if got["prime"] != "prime_x" || got["merchant_id"] != "m_test" || got["currency"] != "TWD" ||
		got["remember"] != true || got["three_domain_secure"] != false || got["amount"] != float64(990) {
		t.Errorf("unexpected request body: %v", got)
	}
}

func TestDeclinedCardIsResultNotError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":10003,"msg":"Card declined"}`))
	})
	res, err := c.PayByToken(context.Background(), TokenRequest{CardKey: "k", CardToken: "t", OrderNumber: "O2", Amount: 1})
	if err != nil {
		t.Fatalf("a decline must not be a transport error: %v", err)
	}
	if res.Success() || res.Status != 10003 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestPaymentURLIsNotSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":0,"payment_url":"https://3ds.example/x"}`))
	})
	res, err := c.PayByPrime(context.Background(), PrimeRequest{Prime: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success() {
		t.Error("a response still waiting on 3DS must not count as success")
	}
}

func TestHTTPErrorNeverLeaksCredentials(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom pk_test", http.StatusInternalServerError)
	})
	_, err := c.PayByPrime(context.Background(), PrimeRequest{Prime: "p"})
	if err == nil {
		t.Fatal("want error on HTTP 500")
	}
	if strings.Contains(err.Error(), "pk_test") {
		t.Errorf("error leaks partner key: %v", err)
	}
}

func TestRefund(t *testing.T) {
	var got map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"status":0,"msg":"Success"}`))
	})
	if err := c.Refund(context.Background(), "D1", 0); err != nil {
		t.Fatal(err)
	}
	if _, has := got["amount"]; has || got["rec_trade_id"] != "D1" {
		t.Errorf("unexpected refund body: %v", got)
	}
}

func TestConfigEnv(t *testing.T) {
	if (Config{}).Env() != "sandbox" || (Config{Production: true}).Env() != "production" {
		t.Error("Env() mismatch")
	}
	if (Config{PartnerKey: "a"}).Configured() || !(Config{PartnerKey: "a", MerchantID: "b"}).Configured() {
		t.Error("Configured() mismatch")
	}
}
