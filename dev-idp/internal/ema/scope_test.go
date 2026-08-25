package ema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
)

// The two functions read an empty second argument in opposite directions, and
// that is the whole reason they are separate. These cases pin both readings so
// a later "simplification" back into one helper fails loudly.
func TestIntersectScopeTreatsEmptyGrantAsNothing(t *testing.T) {
	t.Parallel()

	require.Empty(t, ema.IntersectScope("chat.admin", ""),
		"an assignment granting no scopes must not act as a wildcard")
	require.Empty(t, ema.IntersectScope("", ""))
	require.Equal(t, "chat.read", ema.IntersectScope("chat.read chat.write", "chat.read"))
	require.Empty(t, ema.IntersectScope("chat.write", "chat.read"))
}

func TestApplyCeilingTreatsEmptyCeilingAsUnbounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, "chat.read chat.write", ema.ApplyCeiling("chat.read chat.write", ""),
		"a trust rule naming no scopes does not forbid all of them")
	require.Equal(t, "chat.read", ema.ApplyCeiling("chat.read chat.write", "chat.read"))
	require.Empty(t, ema.ApplyCeiling("", "chat.read"))
}

func TestIntersectScopePreservesRequestedOrderAndDedupes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "b a", ema.IntersectScope("b a b", "a b c"))
}

func TestValidateResourceSlugRejectsUnreachableSlugs(t *testing.T) {
	t.Parallel()

	require.NoError(t, ema.ValidateResourceSlug("chat"))
	require.NoError(t, ema.ValidateResourceSlug("chat-v2"))

	// Each of these would be stored, echoed back inside a plausible issuer,
	// and then be unreachable at every endpoint it claims.
	for _, slug := range []string{"", "  ", "a/b", "a?b", "a#b", "a b"} {
		require.Error(t, ema.ValidateResourceSlug(slug), "slug %q should be rejected", slug)
	}
}

// A slug that survives validation must round-trip through the issuer it
// produces, since that is the property the validation exists to protect.
func TestValidSlugsRoundTripThroughTheIssuer(t *testing.T) {
	t.Parallel()

	const external = "https://idp.example.com"
	for _, slug := range []string{"chat", "chat-v2", "billing_2"} {
		require.NoError(t, ema.ValidateResourceSlug(slug))
		got, ok := ema.ResourceSlugFromIssuer(external, ema.ResourceASIssuer(external, slug))
		require.True(t, ok, "issuer for %q should parse back", slug)
		require.Equal(t, slug, got)
	}
}
