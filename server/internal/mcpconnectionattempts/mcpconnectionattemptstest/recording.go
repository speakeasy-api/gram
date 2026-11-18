// Package mcpconnectionattemptstest lets a flow's test assert what was recorded in
// one line.
package mcpconnectionattemptstest

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// LogsOn is a logs check for an organization with logs on. Pass it to
// mcpconnectionattempts.NewRecorder in tests that write to real ClickHouse.
func LogsOn(context.Context, string) (bool, error) {
	return true, nil
}

func logsOff(context.Context, string) (bool, error) {
	return false, nil
}

// Recording captures rows in memory and fails the test if an attempt ends twice.
type Recording struct {
	// Recorder is what to hand to the code under test.
	Recorder *mcpconnectionattempts.Recorder

	t    *testing.T
	mu   sync.Mutex
	rows []mcpconnectionattempts.Row
}

// New returns an empty recording for an organization with logs on.
func New(t *testing.T) *Recording {
	t.Helper()
	return newRecording(t, LogsOn)
}

// NewLogsOff returns a recording for an organization with logs off. A flow's
// test can run with it and assert RequireNothing.
func NewLogsOff(t *testing.T) *Recording {
	t.Helper()
	return newRecording(t, logsOff)
}

func newRecording(t *testing.T, logsEnabled func(context.Context, string) (bool, error)) *Recording {
	t.Helper()

	rec := &Recording{Recorder: nil, t: t, mu: sync.Mutex{}, rows: nil}
	rec.Recorder = mcpconnectionattempts.NewCapturing(func(row mcpconnectionattempts.Row) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.rows = append(rec.rows, row)
	}, logsEnabled)
	t.Cleanup(rec.requireOneEndingPerAttempt)
	return rec
}

// Rows returns what was recorded, in order.
func (r *Recording) Rows() []mcpconnectionattempts.Row {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mcpconnectionattempts.Row(nil), r.rows...)
}

// RequireStep asserts a non-final row for this step and what happened.
func (r *Recording) RequireStep(step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened) mcpconnectionattempts.Row {
	r.t.Helper()
	return r.require(step, what, false)
}

// RequireEnded asserts the final row for this step and what happened.
func (r *Recording) RequireEnded(step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened) mcpconnectionattempts.Row {
	r.t.Helper()
	return r.require(step, what, true)
}

// RequireNotEnded asserts no attempt has a final row.
func (r *Recording) RequireNotEnded() {
	r.t.Helper()
	for _, row := range r.Rows() {
		require.False(r.t, row.IsFinal, "attempt ended at %s with %s %s", row.Step, row.StepResult, row.Reason)
	}
}

// RequireNoUnclassifiedExit asserts every tracked exit said what happened.
func (r *Recording) RequireNoUnclassifiedExit() {
	r.t.Helper()
	for _, row := range r.Rows() {
		require.NotEqual(r.t, mcpconnectionattempts.NoVerdictDetail, row.SubReason, "a request left %s without a verdict", row.Step)
	}
}

// RequireNothing asserts nothing was recorded.
func (r *Recording) RequireNothing() {
	r.t.Helper()
	require.Empty(r.t, r.Rows())
}

// RequireOneAttempt asserts every row shares one attempt and its fixed fields.
func (r *Recording) RequireOneAttempt() {
	r.t.Helper()
	rows := r.Rows()
	require.NotEmpty(r.t, rows)
	for _, row := range rows {
		require.Equal(r.t, rows[0].AttemptID, row.AttemptID)
		require.True(r.t, rows[0].AttemptStartedAt.Equal(row.AttemptStartedAt))
		require.Equal(r.t, rows[0].OrganizationID, row.OrganizationID)
		require.Equal(r.t, rows[0].ProjectID, row.ProjectID)
		require.Equal(r.t, rows[0].ServerKind, row.ServerKind)
		require.Equal(r.t, rows[0].ServerID, row.ServerID)
		require.Equal(r.t, rows[0].AppKey, row.AppKey)
	}
}

func (r *Recording) require(step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, final bool) mcpconnectionattempts.Row {
	r.t.Helper()

	result, reason := "", ""
	switch happened := what.(type) {
	case mcpconnectionattempts.Result:
		result = happened.String()
	case mcpconnectionattempts.Code:
		reason = happened.String()
	}
	rows := r.Rows()
	for _, row := range rows {
		matches := row.Step == step.String() && row.IsFinal == final && row.Reason == reason
		if matches && (result == "" || row.StepResult == result) {
			return row
		}
	}
	require.Failf(r.t, "not recorded", "want step %s result %q reason %q final %t, got %+v", step, result, reason, final, rows)
	var none mcpconnectionattempts.Row
	return none
}

func (r *Recording) requireOneEndingPerAttempt() {
	r.t.Helper()
	ended := map[string]bool{}
	for _, row := range r.Rows() {
		if !row.IsFinal {
			continue
		}
		id := row.AttemptID.String()
		require.False(r.t, ended[id], "attempt %s ended twice", id)
		ended[id] = true
	}
}
