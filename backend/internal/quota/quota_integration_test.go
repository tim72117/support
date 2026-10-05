//go:build integration

package quota_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/tim72117/ai-support/internal/business"
	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/quota"
	"github.com/tim72117/ai-support/internal/session"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/quota
func TestQuotaEndToEnd(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	sessions := session.New(gdb, false)
	user, err := sessions.Register(fmt.Sprintf("quota-%d@example.com", suffix), "password123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM users WHERE id = ?", user.ID) })

	biz, err := business.New(gdb).Create(user.ID, fmt.Sprintf("q-%d", suffix), "Q")
	if err != nil {
		t.Fatal(err)
	}

	svc := quota.New(gdb)

	st, err := svc.StandingFor(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tier != quota.TierFree || st.Used != 0 || st.Limit != quota.FreePlan.MonthlyTokens {
		t.Fatalf("fresh standing = %+v", st)
	}

	// Override the allowance to 100 tokens, spend 60 then 60.
	gdb.Exec("UPDATE subscriptions SET monthly_quota = 100 WHERE user_id = ?", user.ID)
	use := &quota.Usage{PromptTokens: 40, CompletionTokens: 20, TotalTokens: 60}
	for i := 0; i < 2; i++ {
		d, err := svc.Check(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Allowed {
			t.Fatalf("round %d: should still be allowed, got %+v", i, d)
		}
		if err := svc.Record(ctx, biz.ID, user.ID, "evt", use); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := svc.Check(ctx, user.ID); d.Allowed || d.Used != 120 {
		t.Fatalf("over quota expected, got %+v", d)
	}

	// Deleting the business must not erase the ledger.
	if err := business.New(gdb).Delete(biz.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := svc.Check(ctx, user.ID); d.Used != 120 {
		t.Fatalf("usage lost after business delete: %+v", d)
	}

	if err := svc.SetTier(ctx, user.ID, quota.TierStarter); err != nil {
		t.Fatal(err)
	}
	checks, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Severity == "critical" && !c.OK {
			t.Errorf("integrity check failed: %+v", c)
		}
	}
}
