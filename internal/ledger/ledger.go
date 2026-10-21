// Package ledger stores points entries.
//
// In production the Store is backed by Postgres (table ledger_entries).
// MemoryStore is the stand-in used for local runs and tests.
package ledger

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Kind string

const (
	KindAward    Kind = "award"
	KindReversal Kind = "reversal"
)

// Entry is one line of a member's points ledger.
type Entry struct {
	MemberID  string
	PaymentID string
	Kind      Kind
	Points    int64  // positive for awards, negative for reversals
	Month     string // settlement month of the payment, "2006-01" in UTC
}

// ErrUnavailable is returned when the ledger cannot be reached. Retrying may succeed.
var ErrUnavailable = errors.New("ledger: unavailable")

type Store interface {
	TryAward(ctx context.Context, e Entry, monthlyCap int64) (int64, error)
	TryReverse(ctx context.Context, paymentID, memberID string) (bool, error)
	Balance(ctx context.Context, memberID string) (int64, error)
}

type Option func(*MemoryStore)

// WithLatency delays every call, approximating a database round trip.
func WithLatency(d time.Duration) Option {
	return func(s *MemoryStore) { s.latency = d }
}

// WithFaults installs a hook run at the start of every call (after the
// round trip). A non-nil error is returned to the caller and the call has no effect.
func WithFaults(f func(op string) error) Option {
	return func(s *MemoryStore) { s.faults = f }
}

type MemoryStore struct {
	latency  time.Duration
	faults   func(op string) error

	mu       sync.Mutex
	entries  []Entry
	memberMu map[string]*sync.Mutex
}

func NewMemoryStore(opts ...Option) *MemoryStore {
	s := &MemoryStore{memberMu: make(map[string]*sync.Mutex)}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *MemoryStore) roundTrip(ctx context.Context, op string) error {
	if s.latency > 0 {
		select {
		case <-time.After(s.latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.faults != nil {
		return s.faults(op)
	}
	return nil
}

func (s *MemoryStore) memberLock(memberID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.memberMu[memberID]; !ok {
		s.memberMu[memberID] = &sync.Mutex{}
	}
	return s.memberMu[memberID]
}

func (s *MemoryStore) TryAward(ctx context.Context, e Entry, monthlyCap int64) (int64, error) {
	if err := s.roundTrip(ctx, "try_award"); err != nil {
		return 0, err
	}

	ml := s.memberLock(e.MemberID)
	ml.Lock()
	defer ml.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ex := range s.entries {
		if ex.PaymentID == e.PaymentID && ex.Kind == KindAward {
			return 0, nil
		}
	}

	var used int64
	for _, ex := range s.entries {
		if ex.MemberID == e.MemberID && ex.Month == e.Month && ex.Kind == KindAward {
			used += ex.Points
		}
	}
	if room := monthlyCap - used; e.Points > room {
		e.Points = max(room, 0)
	}
	s.entries = append(s.entries, e)
	return e.Points, nil
}

func (s *MemoryStore) TryReverse(ctx context.Context, paymentID, memberID string) (bool, error) {
	if err := s.roundTrip(ctx, "try_reverse"); err != nil {
		return false, err
	}

	ml := s.memberLock(memberID)
	ml.Lock()
	defer ml.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	var award Entry
	var found bool
	for _, ex := range s.entries {
		if ex.PaymentID == paymentID && ex.Kind == KindAward {
			award = ex
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}

	for _, ex := range s.entries {
		if ex.PaymentID == paymentID && ex.Kind == KindReversal {
			return true, nil
		}
	}

	s.entries = append(s.entries, Entry{
		MemberID:  memberID,
		PaymentID: paymentID,
		Kind:      KindReversal,
		Points:    -award.Points,
		Month:     award.Month,
	})
	return true, nil
}

func (s *MemoryStore) Append(ctx context.Context, e Entry) error {
	if err := s.roundTrip(ctx, "append"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

func (s *MemoryStore) Balance(ctx context.Context, memberID string) (int64, error) {
	if err := s.roundTrip(ctx, "balance"); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, e := range s.entries {
		if e.MemberID == memberID {
			total += e.Points
		}
	}
	return total, nil
}
