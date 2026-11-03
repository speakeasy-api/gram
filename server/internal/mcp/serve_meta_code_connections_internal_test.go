package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestConnectionRouteFailure(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "m"}
	for _, tc := range []struct {
		name         string
		err          error
		want         string
		instructions string
		canConnect   bool
	}{
		{name: "outage with no connection behind it", err: metaMemberClientCredentialError(member, remotesessions.ErrRemoteSessionUnavailable), want: "unreachable", instructions: "This server can't get its connection details right now. Retry shortly. If it keeps happening, ask your administrator.", canConnect: true},
		{name: "not connected", err: memberAuthFailure(member, memberDial{anonymous: true}), want: "reauth_required", instructions: "Call tools.connect(server=...) to reconnect, then verify again in a fresh execution.", canConnect: true},
		{name: "not connected, and tools.connect isn't offered", err: memberAuthFailure(member, memberDial{anonymous: true}), want: "reauth_required", instructions: "This server needs a new sign-in, which can't be started from here. Ask your administrator.", canConnect: false},
		{name: "grant gone", err: remotesessions.ErrNoValidToken, want: "reauth_required", instructions: "Call tools.connect(server=...) to reconnect, then verify again in a fresh execution.", canConnect: true},
		{name: "misconfigured", err: metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialMisconfigured), want: "unknown", instructions: "Connection verification could not complete; try again later.", canConnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, instructions := connectionRouteFailure(tc.err, tc.canConnect)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.instructions, instructions)
		})
	}
}

func TestConnectionRouteFailureSelfCredentialOffersNoConnect(t *testing.T) {
	t.Parallel()

	got, instructions := connectionRouteFailure(metaMemberClientCredentialError(metaMember{slug: "m"}, remotesessions.ErrClientCredentialUnavailable), true)
	require.Equal(t, "unreachable", got)
	require.NotContains(t, instructions, "tools.connect")
}

func TestCodeVerificationLimiterFailureIsNotAThrottle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_code_verify_limiter")
	require.NoError(t, err)
	client, err := TestInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	limiter := ratelimit.New(ratelimit.NewRedisStore(client), "code-verify-test", ratelimit.PerMinute(1))
	service := &Service{db: pool, logger: testenv.NewLogger(t), validationLimiter: limiter}
	backend := &metaCodeBackend{service: service}
	gate := &metaGateContext{userID: "code-verify-test"}
	member := metaMember{slug: "limited", serverID: uuid.New()}
	allowed, err := limiter.Allow(ctx, "code:"+gate.userID+":"+member.serverID.String())
	require.NoError(t, err)
	require.True(t, allowed.Allowed)
	state := backend.verifyConnection(ctx, gate, []metaMember{member}, member.slug)
	require.Equal(t, "unknown", state.Authentication)
	require.Nil(t, state.CheckedAt)
	require.Contains(t, state.Instructions, "rate limited")

	// An unavailable limiter must allow routing, just like consent verification.
	// The nonexistent server then fails its lookup, proving the throttle did not
	// stop verification, without contacting any upstream.
	unavailable := redis.NewClient(client.Options())
	require.NoError(t, unavailable.Close())
	service.validationLimiter = ratelimit.New(ratelimit.NewRedisStore(unavailable), "code-verify-test", ratelimit.PerMinute(1))
	state = backend.verifyConnection(ctx, gate, []metaMember{member}, member.slug)
	require.NotNil(t, state.CheckedAt)
	require.NotContains(t, state.Instructions, "rate limited")
}
