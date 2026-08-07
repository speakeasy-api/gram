package remotesessionprovider

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
