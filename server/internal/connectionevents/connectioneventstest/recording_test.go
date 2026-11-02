package connectioneventstest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/connectionevents"
)

func TestRecording_AssertsInOneLine(t *testing.T) {
	t.Parallel()

	rec := New(t)
	rec.RequireNothing()

	attempt := connectionevents.Begin(connectionevents.ServerScope("org-test", uuid.New(), uuid.New(), connectionevents.AppDCR))
	rec.Recorder.Step(t.Context(), attempt, connectionevents.StepInboundAuthorize, connectionevents.ResultAccepted)
	rec.RequireNotEnded()
	rec.Recorder.End(t.Context(), attempt, connectionevents.StepInboundConsent, connectionevents.CodeCanceled)

	rec.RequireStep(connectionevents.StepInboundAuthorize, connectionevents.ResultAccepted)
	row := rec.RequireEnded(connectionevents.StepInboundConsent, connectionevents.CodeCanceled)
	require.Equal(t, string(connectionevents.OutcomeDeclined), row.Outcome)
	rec.RequireOneAttempt()
}

func TestRecording_LogsOffRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := NewLogsOff(t)
	scope := connectionevents.ServerScope("org-test", uuid.New(), uuid.New(), connectionevents.AppDCR)
	rec.Recorder.Once(t.Context(), scope, connectionevents.StepMCPRequest, connectionevents.CodeInvalidBearerToken)
	rec.RequireNothing()
}
