package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestShadowDecisionRequiresConfirmationAndExplicitAllowAudience(t *testing.T) {
	t.Parallel()

	_, err := (*ShadowDecisionService)(nil).Decide(t.Context(), Principal{}, DecideShadowMCPAccessInput{})
	var decisionErr *ShadowDecisionError
	require.ErrorAs(t, err, &decisionErr)
	require.Equal(t, "confirmation_required", decisionErr.Code)

	service := &ShadowDecisionService{}
	_, err = service.Decide(t.Context(), Principal{UserID: "user", OrganizationID: "organization"}, DecideShadowMCPAccessInput{Confirmed: true})
	require.ErrorAs(t, err, &decisionErr)
	require.Equal(t, "feature_unavailable", decisionErr.Code)
}

func TestNormalizeShadowAudienceReferencesIsCanonical(t *testing.T) {
	t.Parallel()

	actual, err := normalizeShadowAudienceReferences([]string{" second ", "first", "first"})
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, actual)

	_, err = normalizeShadowAudienceReferences([]string{""})
	require.ErrorIs(t, err, ErrShadowDecisionInvalid)
}

func TestShadowDecisionVersionChangesWithLockedState(t *testing.T) {
	t.Parallel()

	codec, err := newShadowDecisionVersionCodec("decision-version-key")
	require.NoError(t, err)
	state := mcpapproval.DecisionVersionState{RequestID: uuid.New(), Status: "requested"}
	version, err := codec.Encode(state)
	require.NoError(t, err)
	require.True(t, codec.Match(version, state))

	state.Status = "approved"
	require.False(t, codec.Match(version, state))
	state.Status = "requested"
	state.LatestDecisionID = uuid.New()
	require.False(t, codec.Match(version, state))
}

func TestShadowDecisionReceiptIsClosed(t *testing.T) {
	t.Parallel()

	require.True(t, validShadowDecisionReceipt(ShadowDecisionReceiptResult{Decision: "deny", Audiences: []ShadowDecisionAudience{}, ResultCategory: "decided"}))
	require.False(t, validShadowDecisionReceipt(ShadowDecisionReceiptResult{Decision: "denied", Audiences: []ShadowDecisionAudience{}, ResultCategory: "decided"}))
	require.False(t, validShadowDecisionReceipt(ShadowDecisionReceiptResult{Decision: "allow", Audiences: nil, ResultCategory: "decided"}))
}

func TestMapShadowDecisionCoreErrorPreservesCallerErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "bad request", err: oops.E(oops.CodeBadRequest, nil, "invalid decision"), code: "invalid_request"},
		{name: "invalid", err: oops.E(oops.CodeInvalid, nil, "invalid principal"), code: "invalid_request"},
		{name: "not found", err: oops.E(oops.CodeNotFound, nil, "request not found"), code: "not_found"},
		{name: "unexpected", err: oops.E(oops.CodeUnexpected, nil, "database unavailable"), code: "feature_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var decisionErr *ShadowDecisionError
			require.ErrorAs(t, mapShadowDecisionCoreError(test.err), &decisionErr)
			require.Equal(t, test.code, decisionErr.Code)
		})
	}

	existing := shadowDecisionInvalid("already mapped")
	require.ErrorIs(t, mapShadowDecisionCoreError(existing), ErrShadowDecisionInvalid)
}
