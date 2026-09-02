package networkaccess

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestAdmissionFinalizerZeroValueFailsClosed(t *testing.T) {
	t.Parallel()

	var finalize AdmissionFinalizer
	require.ErrorContains(t, finalize.Finalize(context.Background(), nil), "finalizer is unavailable")
}

func TestMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModePublicOnly, ModeDual, ModePrivateOnly} {
		parsed, err := Parse(string(mode))
		require.NoError(t, err)
		require.Equal(t, mode, parsed)
	}
	_, err := Parse("other")
	require.Error(t, err)

	mode, err := Effective(pgtype.Text{})
	require.NoError(t, err)
	require.Equal(t, ModePublicOnly, mode)
	mode, err = Effective(pgtype.Text{String: "", Valid: true})
	require.NoError(t, err)
	require.Equal(t, ModePublicOnly, mode)
	mode, err = Effective(pgtype.Text{String: string(ModeDual), Valid: true})
	require.NoError(t, err)
	require.Equal(t, ModeDual, mode)
	_, err = Effective(pgtype.Text{String: "future_mode", Valid: true})
	require.ErrorContains(t, err, "parse persisted network access mode")
	require.Equal(t, ModePublicOnly, EffectiveForView(pgtype.Text{String: "future_mode", Valid: true}))

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
	parsed, err := ParseRequested(&requested, pgtype.Text{String: "future_mode", Valid: true})
	require.NoError(t, err)
	require.Equal(t, ModeDual, parsed)
	parsed, err = ParseRequested[requestedMode](nil, pgtype.Text{String: string(ModePrivateOnly), Valid: true})
	require.NoError(t, err)
	require.Equal(t, ModePrivateOnly, parsed)
	_, err = ParseRequested[requestedMode](nil, pgtype.Text{String: "future_mode", Valid: true})
	require.ErrorContains(t, err, "parse persisted network access mode")
	publicOnly := requestedMode(ModePublicOnly)
	parsed, err = ParseRequested(&publicOnly, pgtype.Text{String: "future_mode", Valid: true})
	require.NoError(t, err)
	require.Equal(t, ModePublicOnly, parsed)
	invalid := requestedMode("other")
	_, err = ParseRequested(&invalid, pgtype.Text{})
	require.ErrorContains(t, err, "parse requested network access mode")
}
