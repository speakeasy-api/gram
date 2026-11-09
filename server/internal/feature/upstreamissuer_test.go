package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestUpstreamIssuerFromAssociations(t *testing.T) {
	t.Parallel()

	flags := new(feature.InMemory)
	require.False(t, feature.UpstreamIssuerFromAssociations(t.Context(), nil, "org_a"))
	require.False(t, feature.UpstreamIssuerFromAssociations(t.Context(), flags, "org_a"))

	flags.SetFlag(feature.FlagUpstreamIssuerFromAssociations, "org_a", true)
	require.True(t, feature.UpstreamIssuerFromAssociations(t.Context(), flags, "org_a"))
	require.False(t, feature.UpstreamIssuerFromAssociations(t.Context(), flags, "org_b"))
	require.False(t, feature.UpstreamIssuerFromAssociations(t.Context(), flags, ""))
	require.False(t, feature.UpstreamIssuerFromAssociations(t.Context(), new(failingFlags), "org_a"), "a failed lookup keeps the column")
}
