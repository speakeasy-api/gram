package oautherr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTokenError_RFC6749Literal(t *testing.T) {
	t.Parallel()

	got, ok := ParseTokenError([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token.","error_uri":"https://as.example/errors"}`))
	require.True(t, ok)
	require.Equal(t, RFC6749Error{
		Code:        CodeInvalidGrant,
		Description: "Unknown or invalid refresh token.",
		URI:         "https://as.example/errors",
	}, got)
	require.Equal(t, "invalid_grant: Unknown or invalid refresh token.", got.Error())

	codeOnly, ok := ParseTokenError([]byte(`{"error":"invalid_grant"}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, codeOnly.Code)
	require.Empty(t, codeOnly.Description)
	require.Equal(t, CodeInvalidGrant, codeOnly.Error())

	uriOnly, ok := ParseTokenError([]byte(`{"error":"invalid_scope","error_uri":"https://as.example/scopes"}`))
	require.True(t, ok)
	require.Equal(t, "invalid_scope: https://as.example/scopes", uriOnly.Error())
}

// RFC 6749 §8.5 permits extension error codes, so the literal "error" member
// is taken verbatim: nothing is split out of it, even text that starts with a
// registered code.
func TestParseTokenError_RFC6749ErrorMemberIsVerbatim(t *testing.T) {
	t.Parallel()

	extension, ok := ParseTokenError([]byte(`{"error":"temporarily_unavailable","error_description":"try again"}`))
	require.True(t, ok)
	require.Equal(t, "temporarily_unavailable", extension.Code)
	require.Equal(t, "try again", extension.Description)

	padded, ok := ParseTokenError([]byte(`{"error":"invalid_grant - refresh token revoked"}`))
	require.True(t, ok)
	require.Equal(t, "invalid_grant - refresh token revoked", padded.Code)

	trimmed, ok := ParseTokenError([]byte(`{"error":"  invalid_client  "}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidClient, trimmed.Code)
}

// Only invalid_grant tells a client its grant is permanently dead, so it wins
// whichever parser produced it; otherwise the RFC reading wins.
func TestParseTokenError_PrefersInvalidGrantAcrossParsers(t *testing.T) {
	t.Parallel()

	vendorWins, ok := ParseTokenError([]byte(`{"error":"unauthorized","errors":["invalid_grant - dead"]}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, vendorWins.Code)
	require.Equal(t, "dead", vendorWins.Description)

	rfcWins, ok := ParseTokenError([]byte(`{"error":"invalid_client","errors":["invalid_request - x"]}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidClient, rfcWins.Code, "without invalid_grant the RFC reading wins")
}

func TestParseTokenError_MalformedBodies(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		``,
		`not json`,
		`[]`,
		`{"error":42}`,
		`{"error":null}`,
		`{"error":""}`,
		`{"error":"   "}`,
		`{"error_description":"orphan"}`,
	} {
		got, ok := ParseTokenError([]byte(body))
		require.False(t, ok, body)
		require.Equal(t, RFC6749Error{Code: "", Description: "", URI: ""}, got, body)
	}
}

// A malformed member or vendor extension next to a valid RFC error must not
// discard the RFC members.
func TestParseTokenError_RFCMembersSurviveMalformedSiblings(t *testing.T) {
	t.Parallel()

	got, ok := ParseTokenError([]byte(`{"error":"invalid_grant","error_description":"Token has been revoked.","errors":[{"detail":"x"}],"error_uri":7}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, got.Code)
	require.Equal(t, "Token has been revoked.", got.Description)
	require.Empty(t, got.URI)
}
