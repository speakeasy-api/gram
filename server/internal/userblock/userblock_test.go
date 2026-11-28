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

func TestCanonicalEmail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		email string
		want  string
	}{
		{name: "lowercases and trims", email: "  Person@Example.COM ", want: "person@example.com"},
		{name: "drops a plus tag on a provider that folds it", email: "person+trial2@outlook.com", want: "person@outlook.com"},
		{name: "keeps a plus tag on other domains", email: "person+trial2@example.com", want: "person+trial2@example.com"},
		{name: "keeps dots outside gmail", email: "first.last@example.com", want: "first.last@example.com"},
		{name: "drops gmail dots", email: "f.i.r.s.t.last@gmail.com", want: "firstlast@gmail.com"},
		{name: "drops gmail dots and plus tag", email: "First.Last+x@Gmail.com", want: "firstlast@gmail.com"},
		{name: "folds googlemail into gmail", email: "first.last@googlemail.com", want: "firstlast@gmail.com"},
		{name: "keeps a local part that is only a plus tag", email: "+tag@gmail.com", want: "+tag@gmail.com"},
		{name: "keeps a gmail local part that is only dots", email: "..@gmail.com", want: "..@gmail.com"},
		{name: "leaves text without a domain", email: "not-an-email", want: "not-an-email"},
		{name: "leaves a missing local part", email: "@example.com", want: "@example.com"},
		{name: "leaves a missing domain", email: "person@", want: "person@"},
		{name: "empty", email: "   ", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, userblock.CanonicalEmail(tc.email))
		})
	}
}

func TestBlockedEmailMatchesAliasesOfABlockedMailbox(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "userblock_alias")
	require.NoError(t, err)

	users := usersrepo.New(conn)
	for id, email := range map[string]string{
		"blocked-gmail":   "first.last+one@gmail.com",
		"blocked-outlook": "person+trial@outlook.com",
		"blocked-company": "person+trial@example.com",
		"active-user":     "someone@example.com",
	} {
		_, err := users.UpsertUser(ctx, usersrepo.UpsertUserParams{ID: id, Email: email, DisplayName: id, PhotoUrl: conv.ToPGTextEmpty(""), Admin: false})
		require.NoError(t, err)
	}
	for _, id := range []string{"blocked-gmail", "blocked-outlook", "blocked-company"} {
		require.NoError(t, users.SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{Blacklisted: true, ID: id}))
	}
	userblock.Forget(conn)

	cases := []struct {
		name    string
		email   string
		blocked bool
	}{
		{name: "the blocked gmail address", email: "first.last+one@gmail.com", blocked: true},
		{name: "gmail with other dots", email: "f.i.r.st.last@gmail.com", blocked: true},
		{name: "gmail without dots", email: "firstlast@gmail.com", blocked: true},
		{name: "gmail with another plus tag", email: "firstlast+two@gmail.com", blocked: true},
		{name: "googlemail spelling", email: "First.Last@googlemail.com", blocked: true},
		{name: "outlook address without the tag", email: "person@outlook.com", blocked: true},
		{name: "outlook address with another tag", email: "Person+other@outlook.com", blocked: true},
		{name: "the blocked company address", email: "Person+Trial@example.com", blocked: true},
		{name: "company address without the tag is another mailbox", email: "person@example.com", blocked: false},
		{name: "company address with another tag is another mailbox", email: "person+other@example.com", blocked: false},
		{name: "dots still matter outside gmail", email: "per.son@example.com", blocked: false},
		{name: "another gmail mailbox", email: "firstlast2@gmail.com", blocked: false},
		{name: "an active user", email: "someone@example.com", blocked: false},
		{name: "empty", email: "", blocked: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cached, err := userblock.NewGate(conn).BlockedEmail(ctx, tc.email)
			require.NoError(t, err)
			require.Equal(t, tc.blocked, cached, "in-memory set")

			uncached, err := userblock.BlockedEmailUncached(ctx, users, tc.email)
			require.NoError(t, err)
			require.Equal(t, tc.blocked, uncached, "direct read")
		})
	}
}
