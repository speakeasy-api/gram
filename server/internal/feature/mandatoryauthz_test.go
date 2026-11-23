package feature_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

type failingFlags struct{ feature.InMemory }

func (f *failingFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return true, errors.New("flag definitions unavailable")
}

func TestMandatoryAuthzShadow(t *testing.T) {
	t.Parallel()

	flags := new(feature.InMemory)
	require.False(t, feature.MandatoryAuthzShadow(t.Context(), nil, "org_a"))
	require.False(t, feature.MandatoryAuthzShadow(t.Context(), flags, "org_a"))

	flags.SetFlag(feature.FlagMandatoryAuthzShadow, "org_a", true)
	require.True(t, feature.MandatoryAuthzShadow(t.Context(), flags, "org_a"))
	require.False(t, feature.MandatoryAuthzShadow(t.Context(), flags, "org_b"))
	require.False(t, feature.MandatoryAuthzShadow(t.Context(), flags, ""))
	require.False(t, feature.MandatoryAuthzShadow(t.Context(), new(failingFlags), "org_a"), "a failed lookup keeps it off")
}
