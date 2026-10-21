package ledger_test

import (
	"context"
	"sync"
	"testing"

	"github.com/rentpoints/pointsd/internal/ledger"
)

func TestTryAward_Idempotent(t *testing.T) {
	store := ledger.NewMemoryStore()
	e := ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 1000, Month: "2026-10"}
	store.TryAward(context.Background(), e, 10_000)
	store.TryAward(context.Background(), e, 10_000)

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 1000 {
		t.Fatalf("balance = %d, want 1000 (duplicate award credited twice)", got)
	}
}

func TestTryAward_AppliesMonthlyCap(t *testing.T) {
	store := ledger.NewMemoryStore()
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 9000, Month: "2026-10"}, 10_000)
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_2", Kind: ledger.KindAward, Points: 3000, Month: "2026-10"}, 10_000)

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 10_000 {
		t.Fatalf("balance = %d, want 10000 (monthly cap not enforced)", got)
	}
}

func TestTryAward_ConcurrentSamePaymentAwardedOnce(t *testing.T) {
	store := ledger.NewMemoryStore()
	e := ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 2000, Month: "2026-10"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.TryAward(context.Background(), e, 10_000)
		}()
	}
	wg.Wait()

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 2000 {
		t.Fatalf("balance = %d, want 2000 (concurrent duplicate award)", got)
	}
}

func TestTryReverse_UsesActualAwardedPoints(t *testing.T) {
	store := ledger.NewMemoryStore()
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 9500, Month: "2026-10"}, 10_000)
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_2", Kind: ledger.KindAward, Points: 1000, Month: "2026-10"}, 10_000)

	store.TryReverse(context.Background(), "pay_2", "mbr_1")

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 9500 {
		t.Fatalf("balance = %d, want 9500 (reversal must deduct only the 500 actually awarded)", got)
	}
}

func TestTryReverse_ReversalInheritsAwardMonth(t *testing.T) {
	store := ledger.NewMemoryStore()
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 1000, Month: "2026-10"}, 10_000)

	store.TryReverse(context.Background(), "pay_1", "mbr_1")

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 0 {
		t.Fatalf("balance = %d, want 0 (reversal should net out the award)", got)
	}
}

func TestTryReverse_UnknownPayment(t *testing.T) {
	store := ledger.NewMemoryStore()
	ok, err := store.TryReverse(context.Background(), "pay_ghost", "mbr_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("TryReverse returned true for a payment that was never awarded")
	}
}

func TestTryReverse_Idempotent(t *testing.T) {
	store := ledger.NewMemoryStore()
	store.TryAward(context.Background(), ledger.Entry{MemberID: "mbr_1", PaymentID: "pay_1", Kind: ledger.KindAward, Points: 1000, Month: "2026-10"}, 10_000)
	store.TryReverse(context.Background(), "pay_1", "mbr_1")
	store.TryReverse(context.Background(), "pay_1", "mbr_1")

	got, _ := store.Balance(context.Background(), "mbr_1")
	if got != 0 {
		t.Fatalf("balance = %d, want 0 (duplicate reversal applied twice)", got)
	}
}
