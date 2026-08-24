package oautherr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The {"errors": [string]} envelope as Datadog emits it, wrapping the RFC 6749
// §5.2 code and description in one string.
func TestParseTokenError_ErrorsStringArray(t *testing.T) {
	t.Parallel()

	got, ok := ParseTokenError([]byte(`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`))
	require.True(t, ok)
	require.Equal(t, RFC6749Error{
		Code:        CodeInvalidGrant,
		Description: "Invalid or expired refresh token or code verifier.",
		URI:         "",
	}, got)
	require.Equal(t, "invalid_grant: Invalid or expired refresh token or code verifier.", got.Error())

	other, ok := ParseTokenError([]byte(`{"errors": ["invalid_client: Missing client secret"]}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidClient, other.Code)
	require.Equal(t, "Missing client secret", other.Description)

	later, ok := ParseTokenError([]byte(`{"errors": ["invalid_request - missing scope", "invalid_grant - dead"]}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, later.Code, "invalid_grant wins regardless of position")
	require.Equal(t, "dead", later.Description)

	first, ok := ParseTokenError([]byte(`{"errors": ["Bad Request", "invalid_scope - x", "invalid_request - y"]}`))
	require.True(t, ok)
	require.Equal(t, CodeInvalidScope, first.Code, "without invalid_grant the first recognized entry wins")
}

// Entries that do not start with an RFC 6749 §5.2 code are arbitrary vendor
// text and never become a code.
func TestParseTokenError_ErrorsStringArrayIgnoresUnregisteredEntries(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"errors": ["Bad Request"]}`,
		`{"errors": ["invalid_grant_extra - x"]}`,
		`{"errors": ["Grant failure: invalid_grant (see docs)"]}`,
		`{"errors": [{"detail": "invalid_grant"}]}`,
		`{"errors": "invalid_grant"}`,
		`{"errors": []}`,
	} {
		_, ok := ParseTokenError([]byte(body))
		require.False(t, ok, body)
	}
}

// The {"error": {"code", "message"}} envelope as Dub emits it: a dead refresh
// grant arrives under code "unauthorized", and only the exact message table
// promotes it to invalid_grant.
func TestParseTokenError_ErrorCodeMessageObjectDeadGrant(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"Refresh token not found.",
		"Refresh token not found",
		"Refresh token expired.",
		"Integration installation not found.",
		"Client ID mismatch.",
	} {
		got, ok := ParseTokenError([]byte(`{"error":{"code":"unauthorized","message":"` + message + `","doc_url":"https://dub.co/docs/api-reference/errors#unauthorized"}}`))
		require.True(t, ok, message)
		require.Equal(t, CodeInvalidGrant, got.Code, message)
		require.Equal(t, message, got.Description, message)
	}

	got, ok := ParseTokenError([]byte(`{"error":{"code":"unauthorized","message":"Refresh token not found."}}`))
	require.True(t, ok)
	require.Equal(t, "invalid_grant: Refresh token not found.", got.Error())
}

// Client authentication failures share the same vendor code but are fixed by
// correcting the client configuration; they surface with the vendor code and
// are never remapped to invalid_grant.
func TestParseTokenError_ErrorCodeMessageObjectNotDeadGrant(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"Missing client_id",
		"OAuth app not found for the provided client_id",
		"Missing client_secret",
		"Invalid client_secret",
		"Refresh token not found for client abc.",
	} {
		got, ok := ParseTokenError([]byte(`{"error":{"code":"unauthorized","message":"` + message + `"}}`))
		require.True(t, ok, message)
		require.Equal(t, "unauthorized", got.Code, message)
		require.Equal(t, message, got.Description, message)
	}

	// The same message under a different code is not in the table either.
	got, ok := ParseTokenError([]byte(`{"error":{"code":"forbidden","message":"Refresh token not found."}}`))
	require.True(t, ok)
	require.Equal(t, "forbidden", got.Code)

	// A code-less object carries nothing usable.
	_, ok = ParseTokenError([]byte(`{"error":{"message":"something"}}`))
	require.False(t, ok)
}

func TestSplitFlattenedError(t *testing.T) {
	t.Parallel()

	code, description, ok := splitFlattenedError("  invalid_grant - Invalid or expired refresh token.  ")
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, code)
	require.Equal(t, "Invalid or expired refresh token.", description)

	code, description, ok = splitFlattenedError("invalid_client: Missing client secret")
	require.True(t, ok)
	require.Equal(t, CodeInvalidClient, code)
	require.Equal(t, "Missing client secret", description)

	code, description, ok = splitFlattenedError("invalid_grant")
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, code)
	require.Empty(t, description)

	code, description, ok = splitFlattenedError("invalid_grant.")
	require.True(t, ok)
	require.Equal(t, CodeInvalidGrant, code)
	require.Empty(t, description)

	_, _, ok = splitFlattenedError("invalid_grant_extra - x")
	require.False(t, ok)

	_, _, ok = splitFlattenedError("Invalid_Grant - x")
	require.False(t, ok, "RFC 6749 §5.2 codes are lowercase")

	_, _, ok = splitFlattenedError("expired_token - x")
	require.False(t, ok, "only the six §5.2 codes are split out of free text")

	_, _, ok = splitFlattenedError("")
	require.False(t, ok)
}
