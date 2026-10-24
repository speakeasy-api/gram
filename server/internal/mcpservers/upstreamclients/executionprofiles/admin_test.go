package executionprofiles

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
)

func (f *fixture) target(execCtx Context) Target {
	return Target{OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant, TriggerID: f.trigger, Context: execCtx}
}

func (f *fixture) setChoice(execCtx Context, serverID uuid.UUID, input ChoiceInput) (*Choice, error) {
	f.t.Helper()
	view, err := Describe(f.t.Context(), f.pool, f.target(execCtx))
	require.NoError(f.t, err)
	input.ExpectedVersion = view.Version
	var previous *Choice
	err = SetChoice(f.t.Context(), f.pool, f.target(execCtx), serverID, input, func(_ context.Context, _ pgx.Tx, replaced *Choice) error {
		previous = replaced
		return nil
	})
	return previous, err
}

func client(id uuid.UUID) ChoiceInput {
	return ChoiceInput{AuthClientID: uuid.NullUUID{UUID: id, Valid: true}, NoAuth: false, Clear: false}
}

func TestSetChoiceFillsAGapWithoutChangingRunningTurns(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	f.authClient(server, typeStaticCredential, uuid.Nil)
	second := f.authClient(server, typeStaticCredential, uuid.Nil)

	first := f.bind(ContextUserless)
	require.Equal(t, []Gap{{MCPServerID: server, Reason: GapSelectionRequired}}, first.Gaps)

	view, err := Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.Len(t, view.Servers, 1)
	require.Len(t, view.Servers[0].Options, 2)
	require.Equal(t, GapSelectionRequired, *view.Servers[0].Gap)

	previous, err := f.setChoice(ContextUserless, server, client(second))
	require.NoError(t, err)
	require.Nil(t, previous)

	next := f.bind(ContextUserless)
	require.Empty(t, next.Gaps)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: second}, f.choices(next.VersionID))
	require.Empty(t, f.choices(first.VersionID), "a running turn's version keeps its choices")

	view, err = Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.Equal(t, upstreamclients.SelectionSourceAdmin, view.Servers[0].Choice.Source)
}

func TestSetChoiceKeepsOtherServersAndCanReturnToAutomatic(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	kept := f.server(pgtype.Bool{Bool: true, Valid: true})
	keptClient := f.authClient(kept, typeStaticCredential, uuid.Nil)
	changed := f.server(pgtype.Bool{Bool: true, Valid: true})
	only := f.authClient(changed, typeStaticCredential, uuid.Nil)
	f.bind(ContextUserBacked)

	_, err := f.setChoice(ContextUserBacked, changed, ChoiceInput{Clear: true})
	require.NoError(t, err)
	view, err := Describe(t.Context(), f.pool, f.target(ContextUserBacked))
	require.NoError(t, err)
	for _, server := range view.Servers {
		if server.MCPServerID == changed {
			require.Equal(t, SourceAutomatic, server.Choice.Source, "the next turn decides it again")
		}
	}

	next := f.bind(ContextUserBacked)
	require.Equal(t, map[uuid.UUID]uuid.UUID{kept: keptClient, changed: only}, f.choices(next.VersionID))
}

func TestSetChoiceRefusesWhatTheContextCannotUse(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	personal := f.authClient(server, typeRemoteSession, f.registration(server))

	_, err := f.setChoice(ContextUserless, server, client(personal))
	require.ErrorIs(t, err, upstreamclients.ErrChoicePersonal)
	view, err := Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.Empty(t, view.Servers[0].Options, "a userless profile is not offered a personal client")

	_, err = f.setChoice(ContextUserBacked, server, client(personal))
	require.NoError(t, err, "a user-backed turn may use the user's own connection")

	_, err = f.setChoice(ContextUserBacked, server, ChoiceInput{NoAuth: true})
	require.ErrorIs(t, err, upstreamclients.ErrChoiceNeedsCredential)
	_, err = f.setChoice(ContextUserBacked, server, client(uuid.New()))
	require.ErrorIs(t, err, upstreamclients.ErrChoiceNotOffered)
	_, err = f.setChoice(ContextUserBacked, uuid.New(), ChoiceInput{Clear: true})
	require.ErrorIs(t, err, ErrServerNotReached)
}

func TestSetChoiceRefusesAChangeDecidedFromAnOldVersion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	f.authClient(server, typeStaticCredential, uuid.Nil)
	second := f.authClient(server, typeStaticCredential, uuid.Nil)
	f.bind(ContextUserless)

	stale := client(second)
	stale.ExpectedVersion = 0
	err := SetChoice(t.Context(), f.pool, f.target(ContextUserless), server, stale, func(context.Context, pgx.Tx, *Choice) error { return nil })
	require.ErrorIs(t, err, ErrVersionChanged)
}

func TestSetChoiceRecordsNoAuthForAServerThatNeedsNone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	open := f.server(pgtype.Bool{Bool: false, Valid: true})
	f.bind(ContextUserless)

	view, err := Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.True(t, view.Servers[0].NoAuthAllowed)

	_, err = f.setChoice(ContextUserless, open, ChoiceInput{NoAuth: true})
	require.NoError(t, err)
	next := f.bind(ContextUserless)
	rows, err := f.q.ListVersionChoicesFixture(t.Context(), next.VersionID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].NoAuth, "the no-auth choice carries into later versions")
}

func TestDescribeReportsAChoiceWhoseClientWasWithdrawn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	f.authClient(server, typeStaticCredential, uuid.Nil)
	chosen := f.authClient(server, typeStaticCredential, uuid.Nil)
	f.bind(ContextUserless)
	_, err := f.setChoice(ContextUserless, server, client(chosen))
	require.NoError(t, err)
	require.NoError(t, f.q.DisableAuthClientFixture(t.Context(), DisableAuthClientFixtureParams{ID: chosen, OrganizationID: f.orgID}))

	view, err := Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.NotNil(t, view.Servers[0].Choice)
	require.NotNil(t, view.Servers[0].Gap, "a disabled client serves nothing")
	require.Equal(t, GapNoClient, *view.Servers[0].Gap)
}

func TestDescribeReportsANoAuthChoiceOnceTheServerNeedsAuthentication(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: false, Valid: true})
	f.bind(ContextUserless)
	_, err := f.setChoice(ContextUserless, server, ChoiceInput{NoAuth: true})
	require.NoError(t, err)
	require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), SetRequiresUpstreamAuthFixtureParams{ID: server, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
	f.authClient(server, typeStaticCredential, uuid.Nil)

	view, err := Describe(t.Context(), f.pool, f.target(ContextUserless))
	require.NoError(t, err)
	require.NotNil(t, view.Servers[0].Choice)
	require.NotNil(t, view.Servers[0].Gap, "a no-auth choice serves nothing once the server needs authentication")
	require.Equal(t, GapSelectionRequired, *view.Servers[0].Gap)
}

func TestSetChoiceReportsTheAutomaticChoiceItReplaces(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	only := f.authClient(server, typeStaticCredential, uuid.Nil)

	previous, err := f.setChoice(ContextUserless, server, ChoiceInput{Clear: true})
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.Equal(t, SourceAutomatic, previous.Source)
	require.Equal(t, uuid.NullUUID{UUID: only, Valid: true}, previous.AuthClientID)
}
