package mcpconnectionattemptstest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func trackerScope() mcpconnectionattempts.Scope {
	return mcpconnectionattempts.ServerScope("org-test", uuid.New(), uuid.New(), mcpconnectionattempts.AppUnverified)
}

// The default is inverted: a branch that forgets to classify shows up.
func TestTracker_ExitWithNoVerdictIsVisible(t *testing.T) {
	t.Parallel()

	rec := New(t)
	func() {
		tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundAuthorize)
		defer tracked.Finish(t.Context())
	}()

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, mcpconnectionattempts.NoVerdictDetail, row.SubReason)
}

func TestTracker_RefuseBeforeAnAttemptIsOneFinalRow(t *testing.T) {
	t.Parallel()

	rec := New(t)
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.Grant("authorization_code"))
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeInvalidRequest, mcpconnectionattempts.Detail("response_type"))
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeUnknownClientID)
	tracked.Finish(t.Context())

	require.Len(t, rec.Rows(), 1, "the first verdict is the only one")
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInvalidRequest)
	require.Equal(t, "authorization_code", row.GrantType)
	require.Equal(t, "response_type", row.SubReason)
	rec.RequireNoUnclassifiedExit()
}

func TestTracker_ContinueIsSilent(t *testing.T) {
	t.Parallel()

	rec := New(t)
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundAuthorize)
	tracked.Continue()
	tracked.Finish(t.Context())

	rec.RequireNothing()
}

// Until the state is spent the page stays usable, so a refusal does not end the attempt.
func TestTracker_RefusalBeforeConsumedIsAStep(t *testing.T) {
	t.Parallel()

	rec := New(t)
	attempt := mcpconnectionattempts.Begin(trackerScope())
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundConsent)
	tracked.Attach(attempt)
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeCustomDomainRequired)
	tracked.Finish(t.Context())

	row := rec.RequireStep(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCustomDomainRequired)
	require.Equal(t, attempt.ID(), row.AttemptID)
	rec.RequireNotEnded()
}

func TestTracker_RefusalAfterConsumedEndsTheAttempt(t *testing.T) {
	t.Parallel()

	rec := New(t)
	attempt := mcpconnectionattempts.Begin(trackerScope())
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundConsent)
	tracked.Attach(attempt)
	tracked.Consumed()
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeCanceled)
	tracked.Finish(t.Context())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
	require.Equal(t, attempt.ID(), row.AttemptID)
}

func TestTracker_NoVerdictAfterConsumedEndsTheAttempt(t *testing.T) {
	t.Parallel()

	rec := New(t)
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundConsent)
	tracked.Attach(mcpconnectionattempts.Begin(trackerScope()))
	tracked.Consumed()
	tracked.Finish(t.Context())

	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeInternalError)
}

func TestTracker_SucceedEndsConnected(t *testing.T) {
	t.Parallel()

	rec := New(t)
	subject := urn.NewUserSubject("user-test")
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepProviderSignIn)
	tracked.Attach(mcpconnectionattempts.Begin(trackerScope()))
	tracked.Consumed()
	tracked.At(mcpconnectionattempts.StepInboundToken)
	tracked.With(mcpconnectionattempts.Subject(&subject))
	tracked.Succeed(t.Context(), mcpconnectionattempts.ResultCodeIssued)
	tracked.Finish(t.Context())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Equal(t, subject.String(), row.SubjectURN)
}

// A consent page reached by old state has no attempt, and still reports on its own row.
func TestTracker_ZeroAttemptStaysOnOnceRows(t *testing.T) {
	t.Parallel()

	rec := New(t)
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundConsent)
	tracked.Attach(mcpconnectionattempts.NoAttempt)
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeExpired)

	rec.RequireEnded(mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeExpired)
}

func TestTracker_ScopeChangesWhatAOnceRowStamps(t *testing.T) {
	t.Parallel()

	rec := New(t)
	scope := mcpconnectionattempts.ServerScope("org-test", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR)
	tracked := rec.Recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundAuthorize)
	tracked.Scope(scope)
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeInvalidRequest)

	require.Equal(t, mcpconnectionattempts.AppDCR.String(), rec.RequireEnded(mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeInvalidRequest).AppKey)
}

// Tests and optional wiring need no guards.
func TestTracker_NilIsSafe(t *testing.T) {
	t.Parallel()

	var recorder *mcpconnectionattempts.Recorder
	tracked := recorder.Track(trackerScope(), mcpconnectionattempts.StepInboundAuthorize)
	require.Nil(t, tracked)
	tracked.Scope(trackerScope())
	tracked.At(mcpconnectionattempts.StepInboundToken)
	tracked.With(mcpconnectionattempts.Detail("x"))
	tracked.Attach(mcpconnectionattempts.NoAttempt)
	tracked.Consumed()
	tracked.Refuse(t.Context(), mcpconnectionattempts.CodeInvalidRequest)
	tracked.Succeed(t.Context(), mcpconnectionattempts.ResultCodeIssued)
	tracked.Continue()
	tracked.Finish(t.Context())
}
