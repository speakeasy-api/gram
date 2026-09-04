package platformmcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogProbeFailureDistinguishesResponseSizeByStage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		stage        catalogProbeStage
		wantState    ReadinessState
		wantEvidence string
	}{
		{name: "initialize", stage: catalogProbeStageInitialize, wantState: ReadinessUnsupported, wantEvidence: "initialize_response_too_large"},
		{name: "tools list", stage: catalogProbeStageToolsList, wantState: ReadinessDegraded, wantEvidence: "tools_list_response_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			roundTripper := &catalogAuthorizationRoundTripper{}
			roundTripper.tooLarge.Store(true)
			state, evidence := catalogProbeFailure(errCatalogProbeResponseTooLarge, roundTripper, test.stage)
			require.Equal(t, test.wantState, state)
			require.Equal(t, test.wantEvidence, evidence)
		})
	}
}

func TestCatalogProbeFailurePrecedenceAndRedirect(t *testing.T) {
	t.Parallel()

	unauthorized := &catalogAuthorizationRoundTripper{}
	unauthorized.unauthorized.Store(true)
	unauthorized.transient.Store(true)
	state, evidence := catalogProbeFailure(errors.New("private HTTP detail"), unauthorized, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnauthorized, state)
	require.Equal(t, "upstream_authorization_rejected", evidence)

	redirected := &catalogAuthorizationRoundTripper{}
	redirected.responded.Store(true)
	redirected.redirected.Store(true)
	state, evidence = catalogProbeFailure(errors.New("private HTTP detail"), redirected, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnsupported, state)
	require.Equal(t, "redirect_rejected", evidence)
}

func TestCatalogProbeFailureClassifiesHTTPProtocolAndTransportFailures(t *testing.T) {
	t.Parallel()

	response := &catalogAuthorizationRoundTripper{}
	response.responded.Store(true)
	state, evidence := catalogProbeFailure(errors.New("private SDK detail"), response, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnsupported, state)
	require.Equal(t, "invalid_mcp_response", evidence)

	state, evidence = catalogProbeFailure(errors.New("private transport detail"), &catalogAuthorizationRoundTripper{}, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnreachable, state)
	require.Equal(t, "probe_failed", evidence)

	transient := &catalogAuthorizationRoundTripper{}
	transient.responded.Store(true)
	transient.transient.Store(true)
	state, evidence = catalogProbeFailure(errors.New("private HTTP detail"), transient, catalogProbeStageInitialize)
	require.Equal(t, ReadinessDegraded, state)
	require.Equal(t, "probe_temporarily_unavailable", evidence)
}
