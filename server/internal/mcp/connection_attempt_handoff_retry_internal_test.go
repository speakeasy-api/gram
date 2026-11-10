package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/stretchr/testify/require"
)

func TestFederatedOnwardPreservesResponse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		err  error
		code mcpconnectionattempts.Code
	}{
		{"configuration", fmt.Errorf("handoff: %w", remotesessions.ErrFederatedConfiguration), mcpconnectionattempts.CodeConfigurationRequired},
		{"unavailable", fmt.Errorf("handoff: %w", remotesessions.ErrFederatedUnavailable), mcpconnectionattempts.CodeIDPTransientFailure},
		{"signing", remotesessions.ErrFederatedSigning, mcpconnectionattempts.CodeInternalError},
		{"identity", remotesessions.ErrFederatedIdentity, mcpconnectionattempts.CodeSignInProviderError},
		{"other", errors.New("cache unavailable"), mcpconnectionattempts.CodeInternalError},
		{"redirect", nil, mcpconnectionattempts.Code{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			endpoint := directEndpoint()
			tracker := rec.Recorder.Track(connectionScope(endpoint, mcpconnectionattempts.AppUnverified), mcpconnectionattempts.StepInboundSignIn)
			tracker.Attach(openState().connectionAttempt(endpoint))
			tracker.Consumed()
			got := federatedOnward(t.Context(), tracker, tt.err)
			if tt.err == nil {
				require.NoError(t, got)
			} else {
				require.Same(t, tt.err, got, "the original error must reach the existing response handler")
			}
			tracker.Finish(t.Context())
			if tt.err == nil {
				rec.RequireNothing()
			} else {
				rec.RequireEnded(mcpconnectionattempts.StepInboundSignIn, tt.code)
				rec.RequireNoUnclassifiedExit()
			}
		})
	}
}

func TestRemoteSessionRefusalLeavesAttemptRetryable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		verdict remotesessions.ValidationOutcome
		code    mcpconnectionattempts.Code
	}{
		{remotesessions.ValidationOutcomeRejectedByMember, mcpconnectionattempts.CodeRejectedByMember},
		{remotesessions.ValidationOutcomeInactive, mcpconnectionattempts.CodeProviderRejected},
	} {
		t.Run(string(tt.verdict), func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			service := &Service{connections: rec.Recorder}
			endpoint, state := directEndpoint(), openState()
			session := uuid.New()
			service.recordRemoteSessionVerdict(t.Context(), mcpconnectionattempts.FromRequest(nil), endpoint, state, remotesessions.Client{}, validationTarget{}, session, tt.verdict, probeFailure{}, remotesessionmetrics.ValidationTriggerVerify)
			rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, tt.code)
			rec.RequireNotEnded()
			// The consent page can reconnect a rejected/inactive grant and verify again
			// under the same challenge; only later approval completes the attempt.
			service.recordRemoteSessionVerdict(t.Context(), mcpconnectionattempts.FromRequest(nil), endpoint, state, remotesessions.Client{}, validationTarget{}, uuid.New(), remotesessions.ValidationOutcomeValid, probeFailure{}, remotesessionmetrics.ValidationTriggerVerify)
			rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultAllowed)
			rec.RequireNotEnded()
			rec.RequireOneAttempt()
		})
	}
}
