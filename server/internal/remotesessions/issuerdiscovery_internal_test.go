package remotesessions

import (
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
