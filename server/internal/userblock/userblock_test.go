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

func TestBlockedEmail_NormalizesGmailAliases(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "userblock_gmail_aliases")
	require.NoError(t, err)

	users := usersrepo.New(conn)
	seeded := []struct {
		id    string
		email string
	}{
		{id: "blocked-plain-gmail", email: "alice@gmail.com"},
		{id: "blocked-googlemail-alias", email: "B.o.b+orig@googlemail.com"},
		{id: "blocked-non-gmail", email: "carol.smith+work@example.com"},
	}
	for _, u := range seeded {
		_, err := users.UpsertUser(ctx, usersrepo.UpsertUserParams{
			ID:          u.id,
			Email:       u.email,
			DisplayName: u.id,
			PhotoUrl:    conv.ToPGTextEmpty(""),
			Admin:       false,
		})
		require.NoError(t, err)
		require.NoError(t, users.SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
			Blacklisted: true,
			ID:          u.id,
		}))
	}
	userblock.Forget(conn)

	cases := []struct {
		name    string
		email   string
		blocked bool
	}{
		{name: "exact gmail", email: "alice@gmail.com", blocked: true},
		{name: "dotted gmail", email: "a.l.i.c.e@gmail.com", blocked: true},
		{name: "plus-tagged gmail", email: "alice+tag@gmail.com", blocked: true},
		{name: "dotted plus googlemail uppercase whitespace", email: "  A.L.I.C.E+puppet@GoogleMail.COM  ", blocked: true},
		{name: "googlemail blocked matches plain gmail", email: "bob@gmail.com", blocked: true},
		{name: "googlemail blocked matches other alias", email: "b.ob+new@gmail.com", blocked: true},
		{name: "gmail subdomain does not match", email: "alice@sub.gmail.com", blocked: false},
		{name: "different gmail user does not match", email: "dave@gmail.com", blocked: false},
		{name: "non-gmail exact match case-insensitive", email: "CAROL.SMITH+WORK@Example.com", blocked: true},
		{name: "non-gmail dots stripped does not match", email: "carolsmith+work@example.com", blocked: false},
		{name: "non-gmail plus stripped does not match", email: "carol.smith@example.com", blocked: false},
	}

	type directDBTX struct {
		usersrepo.DBTX
	}

	gate := userblock.NewGate(conn)
	direct := directDBTX{DBTX: conn}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotGate, err := gate.BlockedEmail(ctx, tc.email)
			require.NoError(t, err)
			require.Equal(t, tc.blocked, gotGate, "in-memory gate mismatch for %q", tc.email)

			gotDirect, err := userblock.BlockedEmailIn(ctx, direct, tc.email)
			require.NoError(t, err)
			require.Equal(t, tc.blocked, gotDirect, "direct SQL check mismatch for %q", tc.email)
		})
	}
}
