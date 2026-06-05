package testkit

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

// DefaultEventTimeout is the default ceiling for WaitForEvent /
// WaitForCondition calls. Override with the TESTKIT_TIMEOUT environment
// variable (Go duration string, e.g. "30s") to slow CI runs.
var DefaultEventTimeout = func() time.Duration {
	if s := os.Getenv("TESTKIT_TIMEOUT"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
	}
	return 5 * time.Second
}()

// WaitForEvent blocks until an event of type T satisfying pred has
// been recorded by the harness, or until timeout elapses. It returns
// the matching event or fails the test with a list of observed event
// types for diagnostics. Pass a nil predicate to match any event of
// type T.
func WaitForEvent[T protocol.Event](t *testing.T, h *Harness, pred func(T) bool, timeout time.Duration) T {
	t.Helper()
	var zero T
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		for _, ev := range h.EventsSnapshot() {
			typed, ok := ev.(T)
			if !ok {
				continue
			}
			if pred == nil || pred(typed) {
				return typed
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitForEvent: timeout %s waiting for %T; observed events: %s",
		timeout, zero, eventTypeList(h))
	return zero
}

// WaitForCondition blocks until cond returns true, or until timeout.
// Useful for asserting on UI-side state changes that aren't captured
// in protocol events directly.
func WaitForCondition(t *testing.T, cond func() bool, timeout time.Duration, label string) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForCondition: timeout %s waiting for %q", timeout, label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// FindEvent returns the last recorded event of type T, or false.
func FindEvent[T protocol.Event](h *Harness) (T, bool) {
	var zero T
	events := h.EventsSnapshot()
	for i := len(events) - 1; i >= 0; i-- {
		if typed, ok := events[i].(T); ok {
			return typed, true
		}
	}
	return zero, false
}

// LastEvent is an alias for FindEvent reading the most recent matching event.
func LastEvent[T protocol.Event](h *Harness) (T, bool) { return FindEvent[T](h) }

// FirstEvent returns the first recorded event of type T, or false.
func FirstEvent[T protocol.Event](h *Harness) (T, bool) {
	var zero T
	for _, ev := range h.EventsSnapshot() {
		if typed, ok := ev.(T); ok {
			return typed, true
		}
	}
	return zero, false
}

// CountEvents returns how many recorded events match type T.
func CountEvents[T protocol.Event](h *Harness) int {
	count := 0
	for _, ev := range h.EventsSnapshot() {
		if _, ok := ev.(T); ok {
			count++
		}
	}
	return count
}

// WaitForEventCount blocks until at least n events of type T have been
// recorded, or until timeout elapses.
func WaitForEventCount[T protocol.Event](t *testing.T, h *Harness, n int, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if CountEvents[T](h) >= n {
			return
		}
		if time.Now().After(deadline) {
			var zero T
			t.Fatalf("waitForEventCount: timeout %s waiting for %d×%T; observed: %s",
				timeout, n, zero, eventTypeList(h))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// AssertNoEvent fails the test if any event of type T appears within
// the given window. Useful for negative assertions ("nothing happens").
func AssertNoEvent[T protocol.Event](t *testing.T, h *Harness, window time.Duration) {
	t.Helper()
	if window <= 0 {
		window = 50 * time.Millisecond
	}
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if _, ok := FindEvent[T](h); ok {
			var zero T
			t.Fatalf("assertNoEvent: unexpected %T appeared", zero)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// AssertEventOrder verifies that events of the given types appear in
// the recorded stream in the specified order (others may be interleaved).
func AssertEventOrder(t *testing.T, h *Harness, types ...reflect.Type) {
	t.Helper()
	events := h.EventsSnapshot()
	idx := 0
	for _, ev := range events {
		if idx >= len(types) {
			return
		}
		if reflect.TypeOf(ev) == types[idx] {
			idx++
		}
	}
	if idx < len(types) {
		t.Fatalf("assertEventOrder: missing event %v at position %d; observed: %s",
			types[idx], idx, eventTypeList(h))
	}
}

func eventTypeList(h *Harness) string {
	events := h.EventsSnapshot()
	if len(events) == 0 {
		return "(none)"
	}
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = reflect.TypeOf(ev).String()
	}
	return fmt.Sprintf("%v", out)
}
