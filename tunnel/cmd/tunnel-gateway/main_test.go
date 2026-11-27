package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestRouteStoreUsesOperationsEndpoint(t *testing.T) {
	ops := miniredis.RunT(t)
	t.Setenv("TUNNEL_REDIS_ADDR", "")
	t.Setenv("GRAM_REDIS_OPS_ADDR", ops.Addr())
	// A leftover next address from the instance switch is ignored.
	t.Setenv("GRAM_REDIS_OPS_NEXT_ADDR", "127.0.0.1:1")
	t.Setenv("GRAM_REDIS_OPS_PASSWORD", "")
	t.Setenv("GRAM_REDIS_OPS_AUTH_MODE", "none")
	t.Setenv("GRAM_REDIS_OPS_CA_CERT", "")

	store, err := buildRouteStore(t.Context(), slog.Default())
	require.NoError(t, err)
	require.NoError(t, store.Publish(t.Context(), "test-tunnel", "gateway:8091", time.Minute))

	addresses, err := store.Candidates(t.Context(), "test-tunnel")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway:8091"}, addresses)
}

func TestRouteStoreUsesSessionsCredentialsWhenRepointed(t *testing.T) {
	sessions := miniredis.RunT(t)
	sessions.RequireAuth("sessions-password")
	t.Setenv("TUNNEL_REDIS_ADDR", sessions.Addr())
	t.Setenv("TUNNEL_REDIS_PASSWORD", "sessions-password")
	t.Setenv("GRAM_REDIS_OPS_ADDR", sessions.Addr())
	t.Setenv("GRAM_REDIS_OPS_AUTH_MODE", "iam")
	t.Setenv("GRAM_REDIS_OPS_CA_CERT", "stale Valkey CA")

	store, err := buildRouteStore(t.Context(), slog.Default())
	require.NoError(t, err)
	require.NoError(t, store.Publish(t.Context(), "test-tunnel", "gateway:8091", time.Minute))
}

type staticEligibility bool

func (e staticEligibility) CarriesOAuth(context.Context, string) (bool, error) {
	return bool(e), nil
}

func TestAuthRouteMirrorIsOffWithoutAuthAddress(t *testing.T) {
	t.Setenv("GRAM_REDIS_AUTH_ADDR", "")
	t.Setenv("GRAM_REDIS_AUTH_NEXT_ADDR", "")

	mirror, err := buildAuthRouteMirror(t.Context(), slog.Default(), staticEligibility(true))
	require.NoError(t, err)
	require.Nil(t, mirror)
}

func TestAuthRouteMirrorWritesToAuthEndpoint(t *testing.T) {
	auth := miniredis.RunT(t)
	t.Setenv("GRAM_REDIS_AUTH_ADDR", auth.Addr())
	t.Setenv("GRAM_REDIS_AUTH_AUTH_MODE", "none")
	t.Setenv("GRAM_REDIS_AUTH_PASSWORD", "")
	t.Setenv("GRAM_REDIS_AUTH_CA_CERT", "")
	t.Setenv("GRAM_REDIS_AUTH_NEXT_ADDR", "")

	mirror, err := buildAuthRouteMirror(t.Context(), slog.Default(), staticEligibility(true))
	require.NoError(t, err)
	require.NotNil(t, mirror)
	require.NoError(t, mirror.Routes.Publish(t.Context(), "test-tunnel", "gateway:8091", time.Minute))

	require.True(t, auth.Exists("auth_tunnel_routes:test-tunnel"))
	require.False(t, auth.Exists("tunnel_routes:test-tunnel"))
}

func TestParseMaxSessionsDefaultsOnEmptyOrZero(t *testing.T) {
	empty, err := parseMaxSessions("")
	require.NoError(t, err)
	require.Zero(t, empty)

	zero, err := parseMaxSessions("0")
	require.NoError(t, err)
	require.Zero(t, zero)
}

func TestParseMaxSessionsAcceptsPositiveValue(t *testing.T) {
	maxSessions, err := parseMaxSessions("42")
	require.NoError(t, err)
	require.Equal(t, 42, maxSessions)
}

func TestParseMaxSessionsRejectsInvalidValue(t *testing.T) {
	_, err := parseMaxSessions("not-a-number")
	require.Error(t, err)

	_, err = parseMaxSessions("-1")
	require.Error(t, err)
}

func TestParsePartitionAcceptsKnownPartitions(t *testing.T) {
	for _, want := range []string{"us", "eu"} {
		got, err := parsePartition(want)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestParsePartitionRejectsMissingOrUnknown(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "missing", raw: "", wantErr: "GRAM_PARTITION is required: set it to one of us, eu"},
		{name: "unknown", raw: "apac", wantErr: `unknown partition "apac"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePartition(tc.raw)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
