package remotesessions

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetainableDocument(t *testing.T) {
	t.Parallel()

	require.JSONEq(t, `{"issuer":"https://idp.example.com"}`, string(retainableDocument([]byte(`{"issuer":"https://idp.example.com"}`))))
	require.Nil(t, retainableDocument(nil))
	require.Nil(t, retainableDocument([]byte(`{"name":"a\u0000b"}`)), "Postgres jsonb rejects the NUL escape")
	require.Nil(t, retainableDocument([]byte("{\"name\":\"\xff\"}")), "invalid UTF-8 cannot be stored")
	require.Nil(t, retainableDocument([]byte(`{"pad":"`+strings.Repeat("x", maxRetainedDocumentBytes)+`"}`)), "oversized documents keep only their typed projection")
}

func TestDiscoveryErrorTransient(t *testing.T) {
	t.Parallel()

	for status, want := range map[int]bool{0: true, 429: true, 500: true, 503: true, 404: false, 400: false, 200: false} {
		require.Equal(t, want, (&discoveryError{WellKnownURL: "", Status: status, cause: nil, definitive: false}).transient(), "status %d", status)
	}
	refused := &discoveryError{WellKnownURL: "", Status: 0, cause: &url.Error{Op: "Get", URL: "", Err: errDiscoveryRedirectRefused}, definitive: true}
	require.False(t, refused.transient(), "a refusal Gram made is not an outage")
	require.ErrorIs(t, refused.cause, errDiscoveryRedirectRefused)
	_ = http.StatusOK
}

func TestDiscoveryFailureMessageIsPublicSafe(t *testing.T) {
	t.Parallel()

	dial := &discoveryError{
		WellKnownURL: "https://idp.example.com/.well-known/oauth-authorization-server",
		Status:       0,
		cause:        errors.New("fetch discovery document: dial tcp 10.0.0.1:443: connect: connection refused"),
		definitive:   false,
	}
	msg, transient := discoveryFailureMessage(dial)
	require.Equal(t, "Could not reach OAuth metadata at https://idp.example.com/.well-known/oauth-authorization-server", msg)
	require.True(t, transient)

	msg, transient = discoveryFailureMessage(&untrustedDocumentError{reason: "metadata document at https://idp.example.com advertises no issuer"})
	require.Equal(t, "metadata document at https://idp.example.com advertises no issuer", msg)
	require.False(t, transient)

	msg, transient = discoveryFailureMessage(errors.New("guardian: policy rejected 10.0.0.1"))
	require.Equal(t, "issuer metadata could not be fetched", msg)
	require.False(t, transient)
}

func TestTruncateForMessage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "short", truncateForMessage("short"))
	require.Equal(t, strings.Repeat("a", maxMessageValueBytes)+"…", truncateForMessage(strings.Repeat("a", maxMessageValueBytes)+"tail"))
	require.Equal(t, strings.Repeat("a", maxMessageValueBytes-1)+"…", truncateForMessage(strings.Repeat("a", maxMessageValueBytes-1)+"étail"), "the cut lands on a rune boundary")
}

func TestMergeIssuerMetadataIgnoresNullDocuments(t *testing.T) {
	t.Parallel()

	base := documentFromRaw([]byte(`{"issuer":"https://idp.example.com","authorization_endpoint":"https://idp.example.com/a","token_endpoint":"https://idp.example.com/t"}`))
	require.NotPanics(t, func() {
		merged := mergeIssuerMetadata(base, documentFromRaw([]byte(`null`)))
		require.Equal(t, string(base.raw), string(merged.raw))
	})
	require.NotPanics(t, func() {
		merged := mergeIssuerMetadata(documentFromRaw([]byte(`null`)), base)
		require.Equal(t, "null", string(merged.raw))
	})
}

func TestRawDocumentIssuer(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://idp.example.com", rawDocumentIssuer([]byte(`{"issuer":"https://idp.example.com"}`)))
	require.Empty(t, rawDocumentIssuer([]byte(`{"jwks_uri":"https://idp.example.com/jwks"}`)))
	require.Empty(t, rawDocumentIssuer(nil))
	require.Empty(t, rawDocumentIssuer([]byte(`null`)))
}
