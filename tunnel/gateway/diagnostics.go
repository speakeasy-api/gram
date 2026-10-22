package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const diagnosticsTimeout = 5 * time.Second

var errDiagnosticsUnsupported = errors.New("diagnostics unsupported")

type diagnosticsResult struct {
	report *wire.DiagnosticsReport
	err    error
}

// pollDiagnostics has one outstanding opener per session, and at most 32 per
// gateway. yamux.OpenStream cannot be cancelled: a timed-out opener retains its
// slot until it finishes. Never close the shared session to cancel diagnostics.
func (g *Gateway) pollDiagnostics(session *yamux.Session, tunnelID, sessionID, token string) {
	failures := 0
	delay := time.Duration(rand.Int64N(int64(time.Second)))
	for {
		timer := time.NewTimer(delay)
		select {
		case <-session.CloseChan():
			timer.Stop()
			return
		case <-timer.C:
		}
		if g.reg.isDraining() {
			return
		}
		delay = wire.DiagnosticsInterval + time.Duration(rand.Int64N(int64(time.Second)))
		if session.NumStreams() >= 224 {
			continue
		}
		attemptedAt := time.Now().UTC()
		select {
		case g.diagnosticSlots <- struct{}{}:
		default:
			// Collector contention is not evidence of an agent failure.
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), diagnosticsTimeout)
		result := make(chan diagnosticsResult, 1)
		go func() {
			defer func() { <-g.diagnosticSlots }()
			report, err := readDiagnostics(ctx, session, token)
			result <- diagnosticsResult{report: report, err: err}
		}()
		select {
		case received := <-result:
			cancel()
			if errors.Is(received.err, errDiagnosticsUnsupported) {
				g.reg.updateDiagnostics(tunnelID, sessionID, nil, "unsupported", attemptedAt)
				g.reconciler.nudge(tunnelID)
				return
			}
			report := received.report
			state := "available"
			if report == nil {
				state = "unavailable"
				failures++
			} else {
				failures = 0
			}
			g.reg.updateDiagnostics(tunnelID, sessionID, report, state, attemptedAt)
		case <-ctx.Done():
			failures++
			cancel()
			g.reg.updateDiagnostics(tunnelID, sessionID, nil, "unavailable", attemptedAt)
			// Do not accumulate opener goroutines on a stalled session.
			select {
			case <-result:
			case <-session.CloseChan():
				return
			}
		case <-session.CloseChan():
			cancel()
			return
		}
		if failures > 0 {
			delay = min(time.Duration(1<<min(failures, 4))*wire.DiagnosticsInterval, 4*time.Minute)
		}
		g.reconciler.nudge(tunnelID)
	}
}

func readDiagnostics(ctx context.Context, session *yamux.Session, token string) (*wire.DiagnosticsReport, error) {
	stream, err := session.OpenStream()
	if err != nil {
		return nil, err
	}
	// Closing even an unacknowledged stream cancels yamux's stream-open timer.
	defer func() { _ = stream.Close() }()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	deadline, _ := ctx.Deadline()
	if err = stream.SetDeadline(deadline); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://tunnel"+wire.ControlStatusPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(wire.HeaderControlToken, token)
	req.Close = true
	if err = req.Write(stream); err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(io.LimitReader(stream, wire.MaxDiagnosticsBytes+4096)), req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, errDiagnosticsUnsupported
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("diagnostics unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, wire.MaxDiagnosticsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > wire.MaxDiagnosticsBytes {
		return nil, errors.New("diagnostic report too large")
	}
	return wire.DecodeDiagnostics(body)
}

func (r *registry) updateDiagnostics(tunnelID, sessionID string, report *wire.DiagnosticsReport, state string, attemptedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.sessions[tunnelID] {
		if entry.id != sessionID {
			continue
		}
		// Replace immutable snapshots, never mutate a pointer returned to Redis.
		status := route.Diagnostics{State: state}
		if entry.connection.Diagnostics != nil {
			status = *entry.connection.Diagnostics
			status.State = state
		}
		status.AttemptedAt = attemptedAt
		if report != nil {
			status.Report = report
			status.ReceivedAt = time.Now().UTC()
		}
		entry.connection.Diagnostics = &status
		return
	}
}
