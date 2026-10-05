// Package billing charges business owners through TapPay and keeps their
// quota tier in step with what they have paid for.
//
// Division of labour with internal/quota: quota owns "what plan is this
// owner on and how much have they used" (subscriptions/usage_events);
// billing owns "how do we collect money for it" (billing_profiles/payments)
// and calls quota.SetTierTx to change the plan. The two share no tables.
//
// TapPay has no subscription scheduler, so recurring charges are driven by
// RenewDue / Run below, using the card_key/card_token saved on the first
// charge.
package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/tappay"
)

const (
	StatusActive   = "active"
	StatusPastDue  = "past_due"
	StatusCanceled = "canceled" // user cancelled; paid access runs to current_period_end
	StatusExpired  = "expired"  // access ended; back on the free tier

	// maxRenewalAttempts is how many declined renewal charges are tolerated
	// before the owner is dropped back to the free tier.
	maxRenewalAttempts = 3
	retryAfterDecline  = 24 * time.Hour
)

var (
	ErrUnknownPlan        = errors.New("unknown plan")
	ErrInvalidRequest     = errors.New("invalid request")
	ErrAlreadySubscribed  = errors.New("already subscribed")
	ErrPaymentInProgress  = errors.New("a payment is already in progress")
	ErrGatewayUnavailable = errors.New("payment gateway unavailable; outcome unknown")
	ErrActivationFailed   = errors.New("payment succeeded but activating the plan failed")
	ErrNoSubscription     = errors.New("no active subscription")
)

// DeclinedError means TapPay answered and refused the charge.
type DeclinedError struct {
	Status int
	Msg    string
}

func (e *DeclinedError) Error() string {
	return fmt.Sprintf("payment declined (status %d): %s", e.Status, e.Msg)
}

// Gateway is the slice of the TapPay client this package needs.
type Gateway interface {
	PayByPrime(ctx context.Context, r tappay.PrimeRequest) (*tappay.Result, error)
	PayByToken(ctx context.Context, r tappay.TokenRequest) (*tappay.Result, error)
}

// Profile is the owner-facing view of a billing profile. Card secrets are
// never part of it.
type Profile struct {
	Tier             string    `json:"tier"`
	Status           string    `json:"status"`
	CardLastFour     string    `json:"cardLastFour"`
	CurrentPeriodEnd time.Time `json:"currentPeriodEnd"`
	FailedAttempts   int       `json:"failedAttempts"`
}

type profileRow struct {
	UserID           int64      `gorm:"column:user_id;primaryKey"`
	Tier             string     `gorm:"column:tier"`
	Status           string     `gorm:"column:status"`
	CardKey          string     `gorm:"column:card_key"`
	CardToken        string     `gorm:"column:card_token"`
	CardLastFour     string     `gorm:"column:card_last_four"`
	AnchorAt         time.Time  `gorm:"column:anchor_at"`
	PeriodsPaid      int        `gorm:"column:periods_paid"`
	CurrentPeriodEnd time.Time  `gorm:"column:current_period_end"`
	NextChargeAt     *time.Time `gorm:"column:next_charge_at"`
	FailedAttempts   int        `gorm:"column:failed_attempts"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (profileRow) TableName() string { return "billing_profiles" }

func (r profileRow) view() *Profile {
	return &Profile{
		Tier: r.Tier, Status: r.Status, CardLastFour: r.CardLastFour,
		CurrentPeriodEnd: r.CurrentPeriodEnd, FailedAttempts: r.FailedAttempts,
	}
}

type paymentRow struct {
	ID                int64     `gorm:"column:id;primaryKey"`
	UserID            int64     `gorm:"column:user_id"`
	Kind              string    `gorm:"column:kind"`
	Tier              string    `gorm:"column:tier"`
	OrderNumber       string    `gorm:"column:order_number"`
	Amount            int       `gorm:"column:amount"`
	Status            string    `gorm:"column:status"`
	RecTradeID        string    `gorm:"column:rec_trade_id"`
	BankTransactionID string    `gorm:"column:bank_transaction_id"`
	GatewayStatus     *int      `gorm:"column:gateway_status"`
	Message           string    `gorm:"column:message"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (paymentRow) TableName() string { return "payments" }

type Service struct {
	db    *gorm.DB
	gw    Gateway
	quota *quota.Service
	log   *slog.Logger
	now   func() time.Time
}

func New(db *gorm.DB, gw Gateway, q *quota.Service, log *slog.Logger) *Service {
	return &Service{db: db, gw: gw, quota: q, log: log, now: time.Now}
}

// Subscribe charges the first period with the one-time prime the browser got
// from TapPay's hosted card fields, saves the card for renewals, and moves
// the owner onto tier.
//
// Failure modes callers must tell apart:
//   - *DeclinedError: nothing was charged; the owner may try again.
//   - ErrGatewayUnavailable: TapPay did not answer, so whether the card was
//     charged is UNKNOWN. The payment row is left 'pending', which blocks
//     further charges for this owner until it is reconciled by hand against
//     the TapPay portal — deliberately, to avoid charging twice.
//   - ErrActivationFailed: the card WAS charged (and recorded) but moving
//     the owner onto the plan failed; needs manual repair.
func (s *Service) Subscribe(ctx context.Context, userID int64, tier quota.Tier, prime string, holder tappay.Cardholder) (*Profile, error) {
	price, ok := PriceFor(tier)
	if !ok {
		return nil, ErrUnknownPlan
	}
	if prime == "" {
		return nil, fmt.Errorf("%w: missing prime", ErrInvalidRequest)
	}

	now := s.now()
	var cur profileRow
	switch err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&cur).Error; {
	case err == nil:
		blocked := cur.Status == StatusActive || cur.Status == StatusPastDue ||
			(cur.Status == StatusCanceled && cur.CurrentPeriodEnd.After(now))
		if blocked {
			return nil, ErrAlreadySubscribed
		}
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("billing: load profile: %w", err)
	}

	pay, err := s.startPayment(ctx, userID, "initial", tier, newOrderNumber("S", userID), price.Amount)
	if err != nil {
		return nil, err
	}

	res, err := s.gw.PayByPrime(ctx, tappay.PrimeRequest{
		Prime: prime, OrderNumber: pay.OrderNumber, Amount: price.Amount,
		Details: price.Name, Cardholder: holder, Remember: true,
	})
	if err != nil {
		s.log.Error("tappay pay-by-prime: outcome unknown, payment left pending", "order", pay.OrderNumber, "err", err)
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnavailable, err)
	}
	if !res.Success() {
		if err := s.finishPayment(ctx, s.db, pay, res); err != nil {
			s.log.Error("record declined payment", "order", pay.OrderNumber, "err", err)
		}
		// A reply that is not a decline but still not final (3DS redirect) is
		// not supported yet; it must not be mistaken for a clean failure.
		if res.PaymentURL != "" {
			s.log.Error("tappay returned a 3DS payment_url although 3DS was not requested", "order", pay.OrderNumber)
			return nil, fmt.Errorf("%w: unexpected 3DS redirect", ErrGatewayUnavailable)
		}
		return nil, &DeclinedError{Status: res.Status, Msg: res.Msg}
	}

	// Money has moved. Record that first, on its own, so the fact survives
	// even if the larger activation transaction below fails.
	if err := s.finishPayment(ctx, s.db, pay, res); err != nil {
		s.log.Error("CHARGED but could not record payment", "order", pay.OrderNumber, "rec_trade_id", res.RecTradeID, "err", err)
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}

	row := profileRow{
		UserID: userID, Tier: string(tier), Status: StatusActive,
		CardKey: res.CardKey, CardToken: res.CardToken, CardLastFour: res.LastFour,
		AnchorAt: now, PeriodsPaid: 1, CurrentPeriodEnd: addMonths(now, 1),
		NextChargeAt: new(addMonths(now, 1)), UpdatedAt: now,
	}
	if res.CardToken == "" {
		// Charged fine but TapPay gave no token (card not rememberable): the
		// first period is paid, there is just nothing to renew with.
		s.log.Warn("no card token returned; subscription will not auto-renew", "order", pay.OrderNumber)
		row.NextChargeAt = nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return err
		}
		return s.quota.SetTierTx(ctx, tx, userID, tier)
	})
	if err != nil {
		s.log.Error("CHARGED but could not activate plan", "order", pay.OrderNumber, "rec_trade_id", res.RecTradeID, "err", err)
		return nil, fmt.Errorf("%w: %v", ErrActivationFailed, err)
	}
	return row.view(), nil
}

// Cancel stops future renewals. The owner keeps the paid tier until the end
// of the period already paid for; RenewDue downgrades them after that.
func (s *Service) Cancel(ctx context.Context, userID int64) (*Profile, error) {
	res := s.db.WithContext(ctx).Model(&profileRow{}).
		Where("user_id = ? AND status IN ?", userID, []string{StatusActive, StatusPastDue}).
		Updates(map[string]any{"status": StatusCanceled, "next_charge_at": nil, "updated_at": s.now()})
	if res.Error != nil {
		return nil, fmt.Errorf("billing: cancel: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNoSubscription
	}
	return s.Get(ctx, userID)
}

// Get returns the owner's billing profile, or ErrNoSubscription.
func (s *Service) Get(ctx context.Context, userID int64) (*Profile, error) {
	var row profileRow
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoSubscription
		}
		return nil, fmt.Errorf("billing: load profile: %w", err)
	}
	return row.view(), nil
}

// RenewDue charges every profile whose next_charge_at has passed, and drops
// to the free tier any cancelled profile whose paid period has ended.
// Safe to run from several processes at once: each profile is claimed by an
// atomic update of next_charge_at before any money is requested.
func (s *Service) RenewDue(ctx context.Context) error {
	now := s.now()
	var due []profileRow
	if err := s.db.WithContext(ctx).
		Where("status IN ? AND next_charge_at <= ? AND card_token <> ''", []string{StatusActive, StatusPastDue}, now).
		Order("next_charge_at").Limit(50).Find(&due).Error; err != nil {
		return fmt.Errorf("billing: find due: %w", err)
	}
	for _, p := range due {
		if err := s.renewOne(ctx, p); err != nil {
			s.log.Error("renewal", "user", p.UserID, "err", err)
		}
	}

	var ended []profileRow
	if err := s.db.WithContext(ctx).
		Where("status = ? AND current_period_end <= ?", StatusCanceled, now).
		Limit(50).Find(&ended).Error; err != nil {
		return fmt.Errorf("billing: find ended: %w", err)
	}
	for _, p := range ended {
		if err := s.expire(ctx, p.UserID); err != nil {
			s.log.Error("expire canceled", "user", p.UserID, "err", err)
		}
	}
	return nil
}

func (s *Service) renewOne(ctx context.Context, p profileRow) error {
	// Claim: whoever moves next_charge_at away from the value they read owns
	// this renewal attempt.
	claim := s.db.WithContext(ctx).Model(&profileRow{}).
		Where("user_id = ? AND next_charge_at = ?", p.UserID, p.NextChargeAt).
		Update("next_charge_at", s.now().Add(time.Hour))
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil // another worker has it
	}

	tier := quota.Tier(p.Tier)
	price, ok := PriceFor(tier)
	if !ok {
		return fmt.Errorf("profile on unpriced tier %q", p.Tier)
	}
	order := fmt.Sprintf("R%d-%d-%d", p.UserID, p.CurrentPeriodEnd.Unix(), p.FailedAttempts)
	pay, err := s.startPayment(ctx, p.UserID, "renewal", tier, order, price.Amount)
	if err != nil {
		if errors.Is(err, ErrPaymentInProgress) {
			s.log.Warn("renewal skipped: an earlier payment for this user is still unresolved", "user", p.UserID)
			return nil
		}
		return err
	}

	res, err := s.gw.PayByToken(ctx, tappay.TokenRequest{
		CardKey: p.CardKey, CardToken: p.CardToken, OrderNumber: order,
		Amount: price.Amount, Details: price.Name,
	})
	if err != nil {
		s.log.Error("tappay pay-by-token: outcome unknown, payment left pending", "order", order, "err", err)
		return fmt.Errorf("%w: %v", ErrGatewayUnavailable, err)
	}
	if err := s.finishPayment(ctx, s.db, pay, res); err != nil {
		s.log.Error("could not record renewal payment result", "order", order, "status", res.Status, "err", err)
		return err
	}

	if res.Success() {
		periods := p.PeriodsPaid + 1
		end := addMonths(p.AnchorAt, periods)
		return s.db.WithContext(ctx).Model(&profileRow{}).Where("user_id = ?", p.UserID).Updates(map[string]any{
			"status": StatusActive, "periods_paid": periods, "current_period_end": end,
			"next_charge_at": end, "failed_attempts": 0, "updated_at": s.now(),
		}).Error
	}

	attempts := p.FailedAttempts + 1
	if attempts >= maxRenewalAttempts {
		return s.expire(ctx, p.UserID)
	}
	return s.db.WithContext(ctx).Model(&profileRow{}).Where("user_id = ?", p.UserID).Updates(map[string]any{
		"status": StatusPastDue, "failed_attempts": attempts,
		"next_charge_at": s.now().Add(retryAfterDecline), "updated_at": s.now(),
	}).Error
}

// expire ends paid access: profile -> expired, quota tier -> free.
func (s *Service) expire(ctx context.Context, userID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&profileRow{}).Where("user_id = ?", userID).Updates(map[string]any{
			"status": StatusExpired, "next_charge_at": nil, "updated_at": s.now(),
		}).Error; err != nil {
			return err
		}
		return s.quota.SetTierTx(ctx, tx, userID, quota.DefaultTier)
	})
}

// Run calls RenewDue every interval until ctx is cancelled.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.RenewDue(ctx); err != nil {
			s.log.Error("renew due", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// startPayment records a 'pending' payment before any request is sent to
// TapPay. The unique indexes on payments turn a concurrent second attempt
// into ErrPaymentInProgress.
func (s *Service) startPayment(ctx context.Context, userID int64, kind string, tier quota.Tier, order string, amount int) (*paymentRow, error) {
	row := &paymentRow{
		UserID: userID, Kind: kind, Tier: string(tier), OrderNumber: order,
		Amount: amount, Status: "pending", UpdatedAt: s.now(),
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrPaymentInProgress
		}
		return nil, fmt.Errorf("billing: record pending payment: %w", err)
	}
	return row, nil
}

func (s *Service) finishPayment(ctx context.Context, db *gorm.DB, pay *paymentRow, res *tappay.Result) error {
	status := "failed"
	if res.Success() {
		status = "succeeded"
	}
	if res.PaymentURL != "" {
		return nil // unresolved 3DS: leave pending
	}
	return db.WithContext(ctx).Model(&paymentRow{}).Where("id = ?", pay.ID).Updates(map[string]any{
		"status": status, "rec_trade_id": res.RecTradeID, "bank_transaction_id": res.BankTransactionID,
		"gateway_status": res.Status, "message": res.Msg, "updated_at": s.now(),
	}).Error
}

func newOrderNumber(prefix string, userID int64) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s%d-%s", prefix, userID, hex.EncodeToString(b[:]))
}

// addMonths adds n calendar months, clamping to the target month's last day
// (Jan 31 + 1 month = Feb 28), the same rule internal/quota uses for its
// usage periods.
func addMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}
