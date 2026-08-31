package networkaccess

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModePublicOnly, ModeDual, ModePrivateOnly} {
		parsed, err := Parse(string(mode))
		require.NoError(t, err)
		require.Equal(t, mode, parsed)
	}
	_, err := Parse("other")
	require.Error(t, err)

	require.Equal(t, ModePublicOnly, Effective(pgtype.Text{}))
	require.Equal(t, ModePublicOnly, Effective(pgtype.Text{String: "", Valid: true}))
	require.Equal(t, ModeDual, Effective(pgtype.Text{String: string(ModeDual), Valid: true}))
	unknown := Effective(pgtype.Text{String: "future_mode", Valid: true})
	require.Equal(t, ModePrivateOnly, unknown)

	require.False(t, Storage(ModePublicOnly).Valid)
	require.Equal(t, pgtype.Text{String: string(ModePrivateOnly), Valid: true}, Storage(ModePrivateOnly))

	require.True(t, ModePublicOnly.Allows(SurfacePublic))
	require.False(t, ModePublicOnly.Allows(SurfacePrivate))
	require.True(t, ModeDual.Allows(SurfacePublic))
	require.True(t, ModeDual.Allows(SurfacePrivate))
	require.False(t, ModePrivateOnly.Allows(SurfacePublic))
	require.True(t, ModePrivateOnly.Allows(SurfacePrivate))

	type requestedMode string
	requested := requestedMode(ModeDual)
	parsed, err := ParseRequested(&requested, ModePublicOnly)
	require.NoError(t, err)
	require.Equal(t, ModeDual, parsed)
	parsed, err = ParseRequested[requestedMode](nil, ModePrivateOnly)
	require.NoError(t, err)
	require.Equal(t, ModePrivateOnly, parsed)
	invalid := requestedMode("other")
	_, err = ParseRequested(&invalid, ModePublicOnly)
	require.ErrorContains(t, err, "parse requested network access mode")
}
