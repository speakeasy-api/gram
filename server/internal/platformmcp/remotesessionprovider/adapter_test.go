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
