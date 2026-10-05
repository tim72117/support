// Package tappay is a thin client for TapPay's server-side payment API.
//
// Flow this package supports: the browser collects card details inside
// TapPay's own hosted fields (TPDirect SDK) and gets back a one-time
// "prime"; the backend exchanges that prime for a charge (PayByPrime), and
// for later recurring charges uses the card_key/card_token TapPay returned
// on the first charge (PayByToken). Card numbers never reach this process.
//
// TapPay has no subscription scheduler of its own — recurring billing is
// the caller's job (see internal/billing).
//
// NOTE: endpoint paths and field names below follow TapPay's documented
// Pay by Prime / Pay by Token / Refund APIs but have only been exercised
// against a fake server in tests, not against the real sandbox. Re-check
// them against TapPay's current docs when the sandbox keys arrive.
package tappay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	SandboxBaseURL    = "https://sandbox.tappaysdk.com"
	ProductionBaseURL = "https://prod.tappaysdk.com"
)

// Config holds TapPay credentials. AppID/AppKey are the browser-side pair
// (safe to expose to the page); PartnerKey is server-only and must never be
// logged or sent to a client.
type Config struct {
	AppID      string
	AppKey     string
	PartnerKey string
	MerchantID string
	Production bool
}

// Configured reports whether the server-side credentials needed to charge
// are present.
func (c Config) Configured() bool {
	return c.PartnerKey != "" && c.MerchantID != ""
}

// Env is the name the TPDirect.setupSDK call expects for its third argument.
func (c Config) Env() string {
	if c.Production {
		return "production"
	}
	return "sandbox"
}

type Client struct {
	cfg     Config
	baseURL string
	http    *http.Client
}

func New(cfg Config) *Client {
	base := SandboxBaseURL
	if cfg.Production {
		base = ProductionBaseURL
	}
	return &Client{cfg: cfg, baseURL: base, http: &http.Client{Timeout: 30 * time.Second}}
}

// Cardholder is the contact info TapPay records with a charge.
type Cardholder struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

type PrimeRequest struct {
	Prime       string
	OrderNumber string
	Amount      int // TWD, whole dollars
	Details     string
	Cardholder  Cardholder
	// Remember asks TapPay to return a card_key/card_token for later
	// PayByToken charges.
	Remember bool
}

type TokenRequest struct {
	CardKey     string
	CardToken   string
	OrderNumber string
	Amount      int
	Details     string
}

// Result is TapPay's answer to a charge. A declined card or any other
// business-level failure comes back as a Result with Status != 0 (and a nil
// error) — only transport/HTTP/decoding problems are returned as errors,
// since those leave the charge's outcome unknown and need different handling.
type Result struct {
	Status            int
	Msg               string
	RecTradeID        string
	BankTransactionID string
	OrderNumber       string
	Amount            int
	// PaymentURL is set when TapPay wants the cardholder redirected for 3D
	// Secure. This package never requests 3DS, so a non-empty value is
	// unexpected and callers should treat it as unresolved.
	PaymentURL string
	CardKey    string
	CardToken  string
	LastFour   string
}

func (r *Result) Success() bool { return r.Status == 0 && r.PaymentURL == "" }

type chargeResponse struct {
	Status            int    `json:"status"`
	Msg               string `json:"msg"`
	RecTradeID        string `json:"rec_trade_id"`
	BankTransactionID string `json:"bank_transaction_id"`
	OrderNumber       string `json:"order_number"`
	Amount            int    `json:"amount"`
	PaymentURL        string `json:"payment_url"`
	CardSecret        struct {
		CardKey   string `json:"card_key"`
		CardToken string `json:"card_token"`
	} `json:"card_secret"`
	CardInfo struct {
		LastFour string `json:"last_four"`
	} `json:"card_info"`
}

func (c *Client) PayByPrime(ctx context.Context, r PrimeRequest) (*Result, error) {
	body := map[string]any{
		"prime":               r.Prime,
		"partner_key":         c.cfg.PartnerKey,
		"merchant_id":         c.cfg.MerchantID,
		"order_number":        r.OrderNumber,
		"amount":              r.Amount,
		"currency":            "TWD",
		"details":             r.Details,
		"cardholder":          r.Cardholder,
		"remember":            r.Remember,
		"three_domain_secure": false,
	}
	return c.charge(ctx, "/tpc/payment/pay-by-prime", body)
}

func (c *Client) PayByToken(ctx context.Context, r TokenRequest) (*Result, error) {
	body := map[string]any{
		"card_key":     r.CardKey,
		"card_token":   r.CardToken,
		"partner_key":  c.cfg.PartnerKey,
		"merchant_id":  c.cfg.MerchantID,
		"order_number": r.OrderNumber,
		"amount":       r.Amount,
		"currency":     "TWD",
		"details":      r.Details,
	}
	return c.charge(ctx, "/tpc/payment/pay-by-token", body)
}

func (c *Client) charge(ctx context.Context, path string, body map[string]any) (*Result, error) {
	var resp chargeResponse
	if err := c.post(ctx, path, body, &resp); err != nil {
		return nil, err
	}
	return &Result{
		Status:            resp.Status,
		Msg:               resp.Msg,
		RecTradeID:        resp.RecTradeID,
		BankTransactionID: resp.BankTransactionID,
		OrderNumber:       resp.OrderNumber,
		Amount:            resp.Amount,
		PaymentURL:        resp.PaymentURL,
		CardKey:           resp.CardSecret.CardKey,
		CardToken:         resp.CardSecret.CardToken,
		LastFour:          resp.CardInfo.LastFour,
	}, nil
}

// Refund refunds amount (whole TWD) of a previous charge. amount 0 asks
// TapPay for a full refund.
func (c *Client) Refund(ctx context.Context, recTradeID string, amount int) error {
	body := map[string]any{
		"partner_key":  c.cfg.PartnerKey,
		"rec_trade_id": recTradeID,
	}
	if amount > 0 {
		body["amount"] = amount
	}
	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := c.post(ctx, "/tpc/transaction/refund", body, &resp); err != nil {
		return err
	}
	if resp.Status != 0 {
		return fmt.Errorf("tappay: refund rejected (status %d): %s", resp.Status, resp.Msg)
	}
	return nil
}

// post sends body as JSON. The partner key goes in the x-api-key header as
// TapPay requires; error messages deliberately never include the request
// body, which carries credentials and card tokens.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("tappay: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("tappay: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.PartnerKey)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tappay: %s: %w", path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("tappay: %s: read response: %w", path, err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("tappay: %s: HTTP %d", path, res.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("tappay: %s: decode response: %w", path, err)
	}
	return nil
}
