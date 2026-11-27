package userblock_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func TestGate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "userblock_gate")
	require.NoError(t, err)

	users := usersrepo.New(conn)
	for _, id := range []string{"blocked-user", "active-user"} {
		_, err := users.UpsertUser(ctx, usersrepo.UpsertUserParams{ID: id, Email: id + "@Example.com", DisplayName: id, PhotoUrl: conv.ToPGTextEmpty(""), Admin: false})
		require.NoError(t, err)
	}

	gate := userblock.NewGate(conn)
	require.NoError(t, gate.Check(ctx, logger, "test", "blocked-user", "active-user", ""))

	require.NoError(t, users.SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{Blacklisted: true, ID: "blocked-user"}))
	blocked, err := gate.Blocked(ctx, "blocked-user")
	require.NoError(t, err)
	require.False(t, blocked, "the loaded set serves until it is refreshed")

	userblock.Forget(conn)
	blocked, err = gate.Blocked(ctx, "blocked-user")
	require.NoError(t, err)
	require.True(t, blocked)
	blocked, err = gate.BlockedEmail(ctx, " BLOCKED-USER@example.com ")
	require.NoError(t, err)
	require.True(t, blocked, "emails match case-insensitively")
	blocked, err = gate.Blocked(ctx, "")
	require.NoError(t, err)
	require.False(t, blocked)

	err = userblock.NewGate(conn).Check(ctx, logger, "test", "active-user", "blocked-user")
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code, "a refusal looks like an invalid credential")
	require.ErrorIs(t, err, userblock.ErrBlocked)
	require.NotContains(t, shareable.Error(), "block", "the response never says the user is blocked")

	var nilGate *userblock.Gate
	require.NoError(t, nilGate.Check(ctx, logger, "test", "blocked-user"))
}

func TestGateFailsClosedWithoutALoadedSet(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "userblock_closed")
	require.NoError(t, err)
	conn.Close()

	err = userblock.NewGate(conn).Check(ctx, testenv.NewLogger(t), "test", "any-user")
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
}
