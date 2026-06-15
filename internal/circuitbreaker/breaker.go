// SPDX-License-Identifier: GPL-3.0-or-later

package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type Breaker struct {
	mu           sync.Mutex
	state        State
	failures     int
	threshold    int
	resetTimeout time.Duration
	lastFailure  time.Time
	halfOpenMax  int
	halfOpenOK   int
}

func New(threshold int, resetTimeout time.Duration) *Breaker {
	return &Breaker{
		threshold:    threshold,
		resetTimeout: resetTimeout,
		halfOpenMax:  2,
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentState()
}

func (b *Breaker) currentState() State {
	if b.state == Open && time.Since(b.lastFailure) > b.resetTimeout {
		b.state = HalfOpen
		b.halfOpenOK = 0
	}
	return b.state
}

func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.currentState() {
	case Closed:
		return nil
	case Open:
		return ErrCircuitOpen
	case HalfOpen:
		return nil
	}
	return nil
}

func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.currentState()
	switch state {
	case HalfOpen:
		b.halfOpenOK++
		if b.halfOpenOK >= b.halfOpenMax {
			b.state = Closed
			b.failures = 0
		}
	case Closed:
		b.failures = 0
	}
}

func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.currentState()
	b.failures++
	b.lastFailure = time.Now()

	if state == HalfOpen {
		b.state = Open
		return
	}

	if b.failures >= b.threshold {
		b.state = Open
	}
}

func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = Closed
	b.failures = 0
}
