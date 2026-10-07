//go:build integration

package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
	"github.com/tim72117/ai-support/internal/tappay"
)

type fakeGateway struct {
	primeRes  *tappay.Result
	primeErr  error
	tokenRes  *tappay.Result
	tokenErr  error
	primeCall int
	tokenCall int
	lastPrime tappay.PrimeRequest
}

func (f *fakeGateway) PayByPrime(_ context.Context, r tappay.PrimeRequest) (*tappay.Result, error) {
	f.primeCall++
	f.lastPrime = r
	return f.primeRes, f.primeErr
}

func (f *fakeGateway) PayByToken(_ context.Context, r tappay.TokenRequest) (*tappay.Result, error) {
	f.tokenCall++
	return f.tokenRes, f.tokenErr
}

var okResult = &tappay.Result{Status: 0, RecTradeID: "D1", CardKey: "ck", CardToken: "ct", LastFour: "4242"}
var declined = &tappay.Result{Status: 10003, Msg: "declined"}

type env struct {
	svc    *Service
	gw     *fakeGateway
	quota  *quota.Service
	gdb    *gorm.DB
	userID int64
	now    time.Time
}

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/billing
func setup(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	user, err := session.New(gdb, false).Register(fmt.Sprintf("billing-%d@example.com", time.Now().UnixNano()), "password123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM users WHERE id = ?", user.ID) })

	e := &env{gw: &fakeGateway{primeRes: okResult, tokenRes: okResult}, quota: quota.New(gdb), gdb: gdb, userID: user.ID, now: time.Now()}
	e.svc = New(gdb, e.gw, e.quota, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	e.svc.now = func() time.Time { return e.now }
	return e
}

func (e *env) tier(t *testing.T) quota.Tier {
	t.Helper()
	st, err := e.quota.StandingFor(context.Background(), e.userID)
	if err != nil {
		t.Fatal(err)
	}
	return st.Tier
}

func (e *env) subscribe(t *testing.T) (*Profile, error) {
	return e.svc.Subscribe(context.Background(), e.userID, quota.TierCampaign, "prime_x", tappay.Cardholder{})
}

func (e *env) count(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	e.gdb.Model(&paymentRow{}).Where(where, args...).Count(&n)
	return n
}

func TestSubscribeSuccess(t *testing.T) {
	e := setup(t)
	p, err := e.subscribe(t)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusActive || p.CardLastFour != "4242" || e.tier(t) != quota.TierCampaign {
		t.Fatalf("profile=%+v tier=%s", p, e.tier(t))
	}
	if e.gw.lastPrime.Amount != 2990 || !e.gw.lastPrime.Remember {
		t.Errorf("charged %+v; amount must come from the server price table", e.gw.lastPrime)
	}
	if e.count(t, "user_id = ? AND status = 'succeeded' AND kind = 'initial'", e.userID) != 1 {
		t.Error("want exactly one succeeded initial payment")
	}
	if _, err := e.subscribe(t); !errors.Is(err, ErrAlreadySubscribed) {
		t.Errorf("second subscribe = %v, want ErrAlreadySubscribed", err)
	}
}

func TestDeclineLeavesPlanAndAllowsRetry(t *testing.T) {
	e := setup(t)
	e.gw.primeRes = declined
	var d *DeclinedError
	if _, err := e.subscribe(t); !errors.As(err, &d) {
		t.Fatalf("err = %v, want DeclinedError", err)
	}
	if e.tier(t) != quota.TierFree || e.count(t, "user_id = ? AND status = 'failed'", e.userID) != 1 {
		t.Fatal("decline must leave the free tier and record a failed payment")
	}
	e.gw.primeRes = okResult
	if _, err := e.subscribe(t); err != nil {
		t.Fatalf("retry after decline: %v", err)
	}
}

func TestGatewayTimeoutBlocksDoubleCharge(t *testing.T) {
	e := setup(t)
	e.gw.primeErr = errors.New("timeout")
	if _, err := e.subscribe(t); !errors.Is(err, ErrGatewayUnavailable) {
		t.Fatalf("err = %v", err)
	}
	e.gw.primeErr = nil
	calls := e.gw.primeCall
	if _, err := e.subscribe(t); !errors.Is(err, ErrPaymentInProgress) {
		t.Fatalf("second attempt = %v, want ErrPaymentInProgress", err)
	}
	if e.gw.primeCall != calls {
		t.Error("an unresolved payment must stop any further charge request")
	}
}

func TestRenewalChargesOnceAndExtendsPeriod(t *testing.T) {
	e := setup(t)
	if _, err := e.subscribe(t); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := e.svc.RenewDue(ctx); err != nil || e.gw.tokenCall != 0 {
		t.Fatalf("nothing is due yet: err=%v calls=%d", err, e.gw.tokenCall)
	}
	e.now = e.now.AddDate(0, 1, 1)
	for i := 0; i < 2; i++ { // second run must not charge again
		if err := e.svc.RenewDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if e.gw.tokenCall != 1 {
		t.Fatalf("token charges = %d, want 1", e.gw.tokenCall)
	}
	p, _ := e.svc.Get(ctx, e.userID)
	if p.Status != StatusActive || !p.CurrentPeriodEnd.After(e.now) {
		t.Fatalf("after renewal: %+v", p)
	}
}

func TestRepeatedDeclinesDropToFree(t *testing.T) {
	e := setup(t)
	if _, err := e.subscribe(t); err != nil {
		t.Fatal(err)
	}
	e.gw.tokenRes = declined
	ctx := context.Background()
	e.now = e.now.AddDate(0, 1, 1)
	for i := 0; i < maxRenewalAttempts; i++ {
		if err := e.svc.RenewDue(ctx); err != nil {
			t.Fatal(err)
		}
		if i < maxRenewalAttempts-1 {
			if p, _ := e.svc.Get(ctx, e.userID); p.Status != StatusPastDue {
				t.Fatalf("attempt %d: status %s, want past_due", i+1, p.Status)
			}
			e.now = e.now.Add(retryAfterDecline + time.Minute)
		}
	}
	if p, _ := e.svc.Get(ctx, e.userID); p.Status != StatusExpired || e.tier(t) != quota.TierFree {
		t.Fatalf("after %d declines: %+v tier=%s", maxRenewalAttempts, p, e.tier(t))
	}
}

func TestCancelKeepsPlanUntilPeriodEnds(t *testing.T) {
	e := setup(t)
	if _, err := e.subscribe(t); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.svc.Cancel(ctx, e.userID); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour)
	_ = e.svc.RenewDue(ctx)
	if e.tier(t) != quota.TierCampaign || e.gw.tokenCall != 0 {
		t.Fatal("cancelled owner keeps the paid plan within the paid period and is never charged again")
	}
	e.now = e.now.AddDate(0, 1, 1)
	_ = e.svc.RenewDue(ctx)
	if p, _ := e.svc.Get(ctx, e.userID); p.Status != StatusExpired || e.tier(t) != quota.TierFree || e.gw.tokenCall != 0 {
		t.Fatalf("after period end: %+v tier=%s charges=%d", p, e.tier(t), e.gw.tokenCall)
	}
}

func TestStartTrialGrantsAccessWithoutCharging(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	p, err := e.svc.StartTrial(ctx, e.userID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusTrialing || e.tier(t) != quota.TierCandidateTrial {
		t.Fatalf("after start trial: %+v tier=%s", p, e.tier(t))
	}
	if e.gw.primeCall != 0 || e.gw.tokenCall != 0 {
		t.Fatalf("a trial must never touch the gateway: prime=%d token=%d", e.gw.primeCall, e.gw.tokenCall)
	}
	if !p.CurrentPeriodEnd.Equal(e.now.AddDate(0, 0, 7)) {
		t.Fatalf("trial period end = %v, want now+7d", p.CurrentPeriodEnd)
	}

	// A second trial, or subscribing while one is running, is not blocked
	// by a card/payment check (there is none) but by the same
	// already-subscribed guard Subscribe itself uses.
	if _, err := e.svc.StartTrial(ctx, e.userID); !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("second trial = %v, want ErrAlreadySubscribed", err)
	}
}

func TestTrialExpiresToFreeWithoutAnyCharge(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.StartTrial(ctx, e.userID); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.AddDate(0, 0, 6)
	if err := e.svc.RenewDue(ctx); err != nil {
		t.Fatal(err)
	}
	if e.tier(t) != quota.TierCandidateTrial {
		t.Fatalf("trial must still be active before day 7: tier=%s", e.tier(t))
	}

	e.now = e.now.AddDate(0, 0, 2) // past the 7-day mark
	if err := e.svc.RenewDue(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := e.svc.Get(ctx, e.userID)
	if p.Status != StatusExpired || e.tier(t) != quota.TierFree {
		t.Fatalf("after trial end: %+v tier=%s", p, e.tier(t))
	}
	if e.gw.primeCall != 0 || e.gw.tokenCall != 0 {
		t.Fatalf("trial expiry must never charge: prime=%d token=%d", e.gw.primeCall, e.gw.tokenCall)
	}

	// The owner can now subscribe normally (the existing card-collecting
	// flow) to a paid plan, same as anyone else past a cancelled/expired
	// profile.
	if _, err := e.subscribe(t); err != nil {
		t.Fatalf("subscribe after trial expiry: %v", err)
	}
	if e.tier(t) != quota.TierCampaign {
		t.Fatalf("after post-trial subscribe: tier=%s", e.tier(t))
	}
}

func TestCancelTrialDropsToFreeImmediately(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.svc.StartTrial(ctx, e.userID); err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.Cancel(ctx, e.userID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusExpired || e.tier(t) != quota.TierFree {
		t.Fatalf("after cancelling a trial: %+v tier=%s", p, e.tier(t))
	}
	if e.gw.primeCall != 0 || e.gw.tokenCall != 0 {
		t.Fatalf("cancelling a trial must never charge: prime=%d token=%d", e.gw.primeCall, e.gw.tokenCall)
	}
}

func TestAddMonthsClamps(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	if got := addMonths(jan31, 1); got.Month() != time.February || got.Day() != 28 {
		t.Errorf("Jan 31 + 1 = %v", got)
	}
	if got := addMonths(jan31, 2); got.Month() != time.March || got.Day() != 31 {
		t.Errorf("Jan 31 + 2 = %v (must anchor on the original day, not drift)", got)
	}
}
