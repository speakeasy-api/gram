package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestKitInterruptStateRequiresTerminal(t *testing.T) {
	t.Parallel()
	s := &kitInterruptState{}
	terminal := json.RawMessage(`{"update":{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}}`)
	s.observe(terminal) // Replayed history cannot acknowledge this request.
	s.arm()
	if !s.beginSend() {
		t.Fatal("current request was not admitted")
	}
	if s.outcome() != "unknown" {
		t.Fatal("HTTP admission is not cancellation completion")
	}
	if s.beginSend() {
		t.Fatal("duplicate cancellation")
	}
	s.observe(terminal)
	if s.outcome() != "cancelled" {
		t.Fatal("missing cancelled terminal")
	}
}
func TestKitInterruptNaturalCompletion(t *testing.T) {
	t.Parallel()
	for _, beforeSend := range []bool{true, false} {
		s := &kitInterruptState{}
		s.arm()
		if !beforeSend {
			s.beginSend()
		}
		s.observe(json.RawMessage(`{"update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}`))
		if s.beginSend() {
			t.Fatal("cancelled after terminal")
		}
		if s.outcome() != "completed" {
			t.Fatal("natural completion reported interrupted")
		}
	}
}
func TestKitInterruptWaitOutcomes(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"cancelled", "completed", "unknown", "unexpected"} {
		got, err := waitKitInterrupt(context.Background(), time.Millisecond, func(context.Context) (string, error) { return outcome, nil })
		if got != (outcome == "cancelled") {
			t.Fatalf("%s: wrong interruption result", outcome)
		}
		if (err != nil) != (outcome == "unknown" || outcome == "unexpected") {
			t.Fatalf("%s: wrong error", outcome)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := waitKitInterrupt(ctx, time.Millisecond, func(context.Context) (string, error) { return "pending", nil })
	if got || !errors.Is(err, ErrKitInterruptPending) || !errors.Is(err, context.Canceled) {
		t.Fatalf("timeout: %t %v", got, err)
	}
}
func TestKitInterruptWaitRequiresDurableAcknowledgement(t *testing.T) {
	t.Parallel()
	calls := 0
	got, err := waitKitInterrupt(context.Background(), time.Microsecond, func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "pending", nil
		}
		return "cancelled", nil
	})
	if err != nil || !got || calls != 3 {
		t.Fatalf("%t %v calls=%d", got, err, calls)
	}
}

func TestKitInterruptAmbiguousSendKeepsObserving(t *testing.T) {
	t.Parallel()
	s := &kitInterruptState{}
	s.arm()
	calls := 0
	send := func(context.Context) error { calls++; return errors.New("response lost") }
	s.request(context.Background(), send)
	if s.outcome() != "unknown" {
		t.Fatal("ambiguous send acknowledged")
	}
	s.request(context.Background(), send)
	if calls != 1 {
		t.Fatal("ambiguous cancellation was resent")
	}
	s.observe(json.RawMessage(`{"update":{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}}`))
	if s.outcome() != "cancelled" {
		t.Fatal("late terminal not observed")
	}
}
