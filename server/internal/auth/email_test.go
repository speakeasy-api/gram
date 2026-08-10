package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSignupEmail(t *testing.T) {
	t.Parallel()

	t.Run("accepts an ordinary address", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateSignupEmail("someone@example.com"))
	})

	t.Run("accepts a subdomain and plus tag", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateSignupEmail("first.last+tag@mail.corp.example.com"))
	})

	t.Run("rejects an empty value", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateSignupEmail(""))
	})

	t.Run("rejects a missing at sign", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateSignupEmail("someone.example.com"))
	})

	t.Run("rejects two at signs", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateSignupEmail("a@b@example.com"))
	})

	t.Run("rejects an empty local part or domain", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateSignupEmail("@example.com"))
		require.Error(t, validateSignupEmail("someone@"))
	})

	// The value becomes a URL query parameter, so CRLF is the case that
	// actually matters.
	t.Run("rejects whitespace and control characters", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateSignupEmail("some one@example.com"))
		require.Error(t, validateSignupEmail("someone@example.com\r\n"))
		require.Error(t, validateSignupEmail("someone@exa\tmple.com"))
	})

	t.Run("rejects anything over 254 characters", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("a", 244) + "@example.com" // 256
		require.Len(t, long, 256)
		require.Error(t, validateSignupEmail(long))
	})

	t.Run("accepts exactly 254 characters", func(t *testing.T) {
		t.Parallel()
		exact := strings.Repeat("a", 242) + "@example.com" // 254
		require.Len(t, exact, 254)
		require.NoError(t, validateSignupEmail(exact))
	})
}
