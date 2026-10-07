//go:build integration

package reservation_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tim72117/ai-support/internal/db"
	"github.com/tim72117/ai-support/internal/reservation"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/reservation
func TestCreate(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	store := reservation.New(gdb)
	suffix := time.Now().UnixNano()
	contact := fmt.Sprintf("lead-%d@example.com", suffix)

	id, err := store.Create(reservation.Input{
		ProductLine: reservation.LineCandidate,
		Tier:        "starter",
		Name:        "王小明",
		Contact:     contact,
		Message:     "想了解參選起步方案的細節。",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a non-zero id")
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM reservations WHERE id = ?", id) })

	var row reservation.Reservation
	if err := gdb.First(&row, id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.ProductLine != reservation.LineCandidate || row.Tier != "starter" || row.Name != "王小明" || row.Contact != contact {
		t.Fatalf("stored row mismatch: %+v", row)
	}

	// invalid product line
	if _, err := store.Create(reservation.Input{ProductLine: "nope", Name: "x", Contact: "x@x.com"}); err != reservation.ErrInvalidProductLine {
		t.Fatalf("invalid product line: got %v", err)
	}
	// missing name
	if _, err := store.Create(reservation.Input{ProductLine: reservation.LineBusiness, Name: "", Contact: "x@x.com"}); err != reservation.ErrNameRequired {
		t.Fatalf("missing name: got %v", err)
	}
	// missing contact
	if _, err := store.Create(reservation.Input{ProductLine: reservation.LineBusiness, Name: "x", Contact: ""}); err != reservation.ErrContactRequired {
		t.Fatalf("missing contact: got %v", err)
	}
	// too long
	if _, err := store.Create(reservation.Input{
		ProductLine: reservation.LineBusiness, Name: strings.Repeat("a", reservation.MaxNameRunes+1), Contact: "x@x.com",
	}); err != reservation.ErrTooLong {
		t.Fatalf("too long name: got %v", err)
	}

	// no message / no tier is fine (both optional)
	id2, err := store.Create(reservation.Input{ProductLine: reservation.LineBusiness, Name: "Jane", Contact: "0912345678"})
	if err != nil {
		t.Fatalf("create minimal: %v", err)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM reservations WHERE id = ?", id2) })
}
