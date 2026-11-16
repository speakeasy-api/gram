package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrKitInterruptPending means cancellation has not been authoritatively confirmed.
// The caller must not publish a stopped frame or cancel the owning projector.
var ErrKitInterruptPending = errors.New("kit cancellation is not confirmed; the owning turn may still be running")

// kitInterruptState observes only the current prompt, never startup replay.
// All terminal updates still pass through the normal transcript projector.
type kitInterruptState struct {
	mu                               sync.Mutex
	armed, sent, terminal, cancelled bool
}

func (s *kitInterruptState) arm() { s.mu.Lock(); defer s.mu.Unlock(); s.armed = true }
func (s *kitInterruptState) observe(raw json.RawMessage) {
	var envelope struct {
		Update struct {
			Kind       string `json:"sessionUpdate"`
			State      string `json:"state"`
			StopReason string `json:"stopReason"`
		} `json:"update"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return
	}
	u := envelope.Update
	if u.Kind != "state_update" || u.State != "idle" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.armed {
		return
	}
	s.terminal = true
	s.cancelled = s.sent && u.StopReason == "cancelled"
}

// beginSend suppresses duplicates and natural-completion races. Setting sent
// before the HTTP request allows its terminal to arrive before HTTP 202.
func (s *kitInterruptState) beginSend() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.armed || s.sent || s.terminal {
		return false
	}
	s.sent = true
	return true
}
func (s *kitInterruptState) outcome() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent && s.cancelled {
		return "cancelled"
	}
	if s.sent && !s.terminal {
		return "unknown"
	}
	return "completed"
}

// waitKitInterrupt never interprets transport admission as successful stopping.
// Timeout leaves the durable command intact; a later terminal may still settle it.
func waitKitInterrupt(ctx context.Context, interval time.Duration, read func(context.Context) (string, error)) (bool, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		outcome, err := read(ctx)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrKitInterruptPending, err)
		}
		switch outcome {
		case "cancelled":
			return true, nil
		case "completed":
			return false, nil
		case "pending":
		default:
			return false, ErrKitInterruptPending
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("%w: %w", ErrKitInterruptPending, ctx.Err())
		case <-ticker.C:
		}
	}
}

// request sends at most once on the owner. A lost HTTP response is ambiguous:
// neither success nor failure is terminal evidence. Keep projecting, do not
// resend, and let terminal/settlement/flush determine the durable outcome.
func (s *kitInterruptState) request(ctx context.Context, send func(context.Context) error) {
	if !s.beginSend() {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = send(bounded)
}
