package networkingress_test

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
)

func TestProductionNamer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	namer := networkingress.ProductionNamer{}
	require.Equal(t, "speakeasy", must(namer.Hostname(ctx, "speakeasy")))
	require.True(t, must(namer.OwnsHostname(ctx, "speakeasy")))
	require.False(t, must(namer.OwnsHostname(ctx, "local-ab12-speakeasy")), "production never manages a local stack's service")
	_, err := namer.Hostname(ctx, "local-ab12-speakeasy")
	require.ErrorIs(t, err, networkingress.ErrReservedHostname, "production reserves the local- prefix")
	require.Equal(t, "svc:speakeasy", must(namer.ServiceName(ctx, "speakeasy")))
	require.Equal(t, "Speakeasy private MCP ingress", must(namer.ServiceComment(ctx)))
	require.Equal(t, "speakeasy-gw-0", must(namer.MachineName(ctx, 0)))
	ingressID := uuid.MustParse("1a2b3c4d-0000-4000-8000-000000000000")
	require.Equal(t, "Speakeasy gateway 1a2b3c4d slot 2", must(namer.AuthKeyDescription(ctx, ingressID, 2)))
}

func TestLocalNamer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	namer, err := networkingress.NewLocalNamer("ab12")
	require.NoError(t, err)
	require.Equal(t, "local-ab12-speakeasy", must(namer.Hostname(ctx, "speakeasy")))
	require.Equal(t, "local-ab12-speakeasy", must(namer.Hostname(ctx, "local-ab12-speakeasy")), "an owned hostname is not prefixed twice")
	require.Equal(t, "svc:local-ab12-speakeasy", must(namer.ServiceName(ctx, "local-ab12-speakeasy")))
	_, err = namer.ServiceName(ctx, "speakeasy")
	require.Error(t, err, "a service the stack does not own cannot even be named")
	require.Equal(t, "Speakeasy local development (local-ab12)", must(namer.ServiceComment(ctx)))
	require.Equal(t, "speakeasy-gw-local-ab12-3", must(namer.MachineName(ctx, 3)))
	require.Equal(t, "Speakeasy local ab12 1a2b3c4d slot 1", must(namer.AuthKeyDescription(ctx, uuid.MustParse("1a2b3c4d-0000-4000-8000-000000000000"), 1)))

	require.True(t, must(namer.OwnsHostname(ctx, "local-ab12-speakeasy")))
	require.False(t, must(namer.OwnsHostname(ctx, "speakeasy")), "production's hostnames are never owned")
	require.False(t, must(namer.OwnsHostname(ctx, "local-ab12")), "the bare qualifier is not a hostname this stack created")
	require.False(t, must(namer.OwnsHostname(ctx, "local-ab123-speakeasy")), "another stack's qualifier is not a prefix match")

	for _, devID := range []string{"", "AB12", "ab-12", "ab12ab12a", "ab 1"} {
		_, err := networkingress.NewLocalNamer(devID)
		require.Error(t, err, "development id %q", devID)
	}
}

func TestNewNamer(t *testing.T) {
	t.Parallel()

	namer, err := networkingress.NewNamer("prod", "")
	require.NoError(t, err)
	require.IsType(t, networkingress.ProductionNamer{}, namer)

	namer, err = networkingress.NewNamer("local", "ab12")
	require.NoError(t, err)
	require.IsType(t, networkingress.LocalNamer{}, namer)

	for _, environment := range []string{"dev", "prod", ""} {
		_, err := networkingress.NewNamer(environment, "ab12")
		require.ErrorIs(t, err, networkingress.ErrDevIDOutsideLocal, "environment %q", environment)
	}
}

// authKeyDescriptionPattern is what Tailscale accepts for an auth key
// description: letters, digits and spaces, at most 50 characters.
var authKeyDescriptionPattern = regexp.MustCompile(`^[A-Za-z0-9 ]{1,50}$`)

func TestAuthKeyDescriptionsAreAcceptedByTailscale(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	// The longest development id and slot give the longest descriptions.
	local, err := networkingress.NewLocalNamer("abcd1234")
	require.NoError(t, err)
	for _, namer := range []networkingress.Namer{networkingress.ProductionNamer{}, local} {
		for _, slot := range []int32{0, 1000} {
			description := must(namer.AuthKeyDescription(ctx, uuid.New(), slot))
			require.Regexp(t, authKeyDescriptionPattern, description)
		}
	}
}

// must unwraps a Namer result whose implementation cannot fail.
func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
