package remotesessionprovider

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
