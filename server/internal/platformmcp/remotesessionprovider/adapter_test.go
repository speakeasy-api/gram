package remotesessionprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestValidDescriptorRequiresReviewedHTTPSURLs(t *testing.T) {
	t.Parallel()

	valid := Descriptor{
		ProviderKey:                "fixture",
		RemoteSessionIssuerID:      uuid.New(),
		StreamableHTTPURL:          "https://provider.test/mcp",
		ProviderSetupCompletionURL: "https://gram.test/platform-mcp/provider-setup-complete",
	}
	require.True(t, validDescriptor(valid))

	valid.StreamableHTTPURL = "http://provider.test/mcp"
	require.False(t, validDescriptor(valid))
	valid.StreamableHTTPURL = "https://provider.test/mcp"
	valid.ProviderSetupCompletionURL = "http://gram.test/platform-mcp/provider-setup-complete"
	require.False(t, validDescriptor(valid))
}

func TestPreflightSetupRunsConfiguratorBeforeClientLookup(t *testing.T) {
	t.Parallel()

	configured := false
	configurator := configuratorFunc(func(_ context.Context, _ platformmcp.ProviderSetupRequest, _ Descriptor) error {
		configured = true
		return errors.New("configure fixture client")
	})
	adapter := New(nil, &remotesessions.ChallengeManager{}, Descriptor{
		ProviderKey:                "fixture",
		RemoteSessionIssuerID:      uuid.New(),
		StreamableHTTPURL:          "https://provider.test/mcp",
		ProviderSetupCompletionURL: "https://gram.test/platform-mcp/provider-setup-complete",
	}, configurator)
	err := adapter.PreflightSetup(t.Context(), platformmcp.ProviderSetupRequest{
		UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), MCPSlug: "mcp", ConnectionID: uuid.New(), Generation: uuid.New(),
	})

	require.ErrorContains(t, err, "configure fixture client")
	require.True(t, configured)
}

func TestReadinessResultUsesDefaultOrTestOnlyLifetime(t *testing.T) {
	t.Parallel()

	request := platformmcp.ProviderReadinessProbeRequest{
		UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), ConnectionID: uuid.New(), Generation: uuid.New(),
	}
	defaultAdapter := &Adapter{}
	defaultResult := defaultAdapter.readinessResult(platformmcp.ReadinessReady, "tools_list_ok", request, remotesessions.ResolvedAuthorization{}, "")
	require.WithinDuration(t, time.Now().UTC().Add(readinessLifetime), defaultResult.ExpiresAt, time.Second)

	fixtureAdapter := &Adapter{descriptor: Descriptor{TestOnlyReadinessLifetime: 15 * time.Minute}}
	fixtureResult := fixtureAdapter.readinessResult(platformmcp.ReadinessReady, "tools_list_ok", request, remotesessions.ResolvedAuthorization{}, "")
	require.WithinDuration(t, time.Now().UTC().Add(15*time.Minute), fixtureResult.ExpiresAt, time.Second)
}

func TestPreflightSetupRejectsAlreadyConsumedHandoff(t *testing.T) {
	t.Parallel()

	adapter := &Adapter{}
	err := adapter.PreflightSetup(t.Context(), platformmcp.ProviderSetupRequest{
		UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), MCPSlug: "mcp", ConnectionID: uuid.New(), Generation: uuid.New(), HandoffID: uuid.New(),
	})

	require.ErrorIs(t, err, platformmcp.ErrSetupHandoffInvalid)
}

func TestBoundedReadCloserAllowsExactLimit(t *testing.T) {
	t.Parallel()

	reader := &boundedReadCloser{ReadCloser: io.NopCloser(bytes.NewReader([]byte("1234"))), remaining: 4}
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("1234"), body)
}

func TestBoundedReadCloserRejectsOverflow(t *testing.T) {
	t.Parallel()

	reader := &boundedReadCloser{ReadCloser: io.NopCloser(bytes.NewReader([]byte("12345"))), remaining: 4}
	_, err := io.ReadAll(reader)
	require.ErrorIs(t, err, errResponseTooLarge)
}

func TestNormalizedProbeFailureDistinguishesResponseSizeByStage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		stage        probeStage
		wantState    platformmcp.ReadinessState
		wantEvidence string
	}{
		{name: "initialize", stage: probeStageInitialize, wantState: platformmcp.ReadinessUnsupported, wantEvidence: "initialize_response_too_large"},
		{name: "tools list", stage: probeStageToolsList, wantState: platformmcp.ReadinessDegraded, wantEvidence: "tools_list_response_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			roundTripper := &authorizationRoundTripper{}
			roundTripper.responseTooLarge.Store(true)
			state, evidence := normalizedProbeFailure(errResponseTooLarge, roundTripper, test.stage)
			require.Equal(t, test.wantState, state)
			require.Equal(t, test.wantEvidence, evidence)
		})
	}
}

func TestNormalizedProbeFailureKeepsAuthorizationRejectionPrecedence(t *testing.T) {
	t.Parallel()

	roundTripper := &authorizationRoundTripper{}
	roundTripper.authorizationRejected.Store(true)
	roundTripper.transientResponse.Store(true)
	state, evidence := normalizedProbeFailure(errors.New("private HTTP detail"), roundTripper, probeStageInitialize)
	require.Equal(t, platformmcp.ReadinessUnauthorized, state)
	require.Equal(t, "provider_authorization_rejected", evidence)
}

func TestNormalizedProbeFailureClassifiesHTTPProtocolAndTransportFailures(t *testing.T) {
	t.Parallel()

	response := &authorizationRoundTripper{}
	response.responseReceived.Store(true)
	state, evidence := normalizedProbeFailure(errors.New("private SDK detail"), response, probeStageInitialize)
	require.Equal(t, platformmcp.ReadinessUnsupported, state)
	require.Equal(t, "invalid_mcp_response", evidence)

	state, evidence = normalizedProbeFailure(errors.New("private transport detail"), &authorizationRoundTripper{}, probeStageInitialize)
	require.Equal(t, platformmcp.ReadinessUnreachable, state)
	require.Equal(t, "probe_failed", evidence)

	transient := &authorizationRoundTripper{}
	transient.responseReceived.Store(true)
	transient.transientResponse.Store(true)
	state, evidence = normalizedProbeFailure(errors.New("private HTTP detail"), transient, probeStageInitialize)
	require.Equal(t, platformmcp.ReadinessDegraded, state)
	require.Equal(t, "probe_temporarily_unavailable", evidence)
}

func TestAuthorizationRoundTripperRejectsChunkedOverflow(t *testing.T) {
	t.Parallel()

	rt := &authorizationRoundTripper{
		base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: -1,
				Body:          io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), maxResponseBytes+1))),
				Header:        make(http.Header),
				Request:       nil,
				TLS:           nil,
				Trailer:       nil,
				Uncompressed:  false,
			}, nil
		}),
		authorization: "Bearer test-token",
	}
	response, err := rt.RoundTrip(&http.Request{Header: make(http.Header)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	_, err = io.ReadAll(response.Body)
	require.ErrorIs(t, err, errResponseTooLarge)
	require.True(t, rt.responseTooLarge.Load())
}

type configuratorFunc func(context.Context, platformmcp.ProviderSetupRequest, Descriptor) error

func (f configuratorFunc) ConfigureProviderClient(ctx context.Context, request platformmcp.ProviderSetupRequest, descriptor Descriptor) error {
	return f(ctx, request, descriptor)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// A handoff issued by a connection-less surface reaches setup without a
// connection pair. The request is still identified by its user, so setup must
// proceed rather than be rejected as an invalid handoff.
func TestPreflightSetupAcceptsAConnectionlessRequest(t *testing.T) {
	t.Parallel()

	configured := false
	configurator := configuratorFunc(func(_ context.Context, _ platformmcp.ProviderSetupRequest, _ Descriptor) error {
		configured = true
		return errors.New("configure fixture client")
	})
	adapter := New(nil, &remotesessions.ChallengeManager{}, Descriptor{
		ProviderKey:                "fixture",
		RemoteSessionIssuerID:      uuid.New(),
		StreamableHTTPURL:          "https://provider.test/mcp",
		ProviderSetupCompletionURL: "https://gram.test/platform-mcp/provider-setup-complete",
	}, configurator)
	err := adapter.PreflightSetup(t.Context(), platformmcp.ProviderSetupRequest{
		UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), MCPSlug: "mcp",
	})

	require.ErrorContains(t, err, "configure fixture client", "a connection-less request must reach the configurator")
	require.True(t, configured)
}

// A half-populated pair is an incomplete identity, not a connection-less
// caller, and must not be admitted by the relaxed check.
func TestPreflightSetupRejectsAHalfPopulatedConnection(t *testing.T) {
	t.Parallel()

	adapter := New(nil, &remotesessions.ChallengeManager{}, Descriptor{
		ProviderKey:                "fixture",
		RemoteSessionIssuerID:      uuid.New(),
		StreamableHTTPURL:          "https://provider.test/mcp",
		ProviderSetupCompletionURL: "https://gram.test/platform-mcp/provider-setup-complete",
	}, nil)
	for _, request := range []platformmcp.ProviderSetupRequest{
		{UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), MCPSlug: "mcp", ConnectionID: uuid.New()},
		{UserID: "user", OrganizationID: "organization", ProjectID: uuid.New(), RegistrationID: uuid.New(), UserSessionIssuerID: uuid.New(), MCPSlug: "mcp", Generation: uuid.New()},
	} {
		require.ErrorIs(t, adapter.PreflightSetup(t.Context(), request), platformmcp.ErrSetupHandoffInvalid)
	}
}
