package mcpconnectionattemptstest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

func TestRecording_AssertsInOneLine(t *testing.T) {
	t.Parallel()

	rec := New(t)
	rec.RequireNothing()

	attempt := mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope("org-test", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR))
	rec.Recorder.Step(t.Context(), attempt, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted)
	rec.RequireNotEnded()
	rec.Recorder.End(t.Context(), attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)

	rec.RequireStep(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
	require.Equal(t, string(mcpconnectionattempts.OutcomeDeclined), row.Outcome)
	rec.RequireOneAttempt()
}

func TestRecording_LogsOffRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := NewLogsOff(t)
	scope := mcpconnectionattempts.ServerScope("org-test", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR)
	rec.Recorder.Once(t.Context(), scope, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	rec.RequireNothing()
}
