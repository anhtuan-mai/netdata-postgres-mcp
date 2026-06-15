// SPDX-License-Identifier: GPL-3.0-or-later

package circuitbreaker

import (
	"testing"
	"time"
)

func TestBreaker_StartsClosedAllowsThrough(t *testing.T) {
	cb := New(3, 5*time.Second)
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if cb.State() != Closed {
		t.Fatalf("expected Closed")
	}
}

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	cb := New(3, 5*time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != Closed {
		t.Fatal("should still be closed after 2 failures")
	}
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatal("should be open after 3 failures")
	}
	if err := cb.Allow(); err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := New(2, 10*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatal("should be open")
	}
	time.Sleep(15 * time.Millisecond)
	if cb.State() != HalfOpen {
		t.Fatal("should be half-open after timeout")
	}
	if err := cb.Allow(); err != nil {
		t.Fatalf("half-open should allow: %v", err)
	}
}

func TestBreaker_ClosesAfterHalfOpenSuccess(t *testing.T) {
	cb := New(2, 10*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.State() != Closed {
		t.Fatal("should close after enough half-open successes")
	}
}

func TestBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	cb := New(2, 10*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	// In half-open, a failure reopens
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatal("should reopen on half-open failure")
	}
}

func TestBreaker_Reset(t *testing.T) {
	cb := New(2, 5*time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatal("should be open")
	}
	cb.Reset()
	if cb.State() != Closed {
		t.Fatal("should be closed after reset")
	}
}

func TestBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := New(3, 5*time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordFailure()
	if cb.State() != Closed {
		t.Fatal("should still be closed — success reset the counter")
	}
}
