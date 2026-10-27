package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDelegateGrantPreservesAnotherEditorsAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
	// Both callers have agent editing permission; source ownership must still apply.
	ti.service.SetBindingAuthorizer(func(context.Context, pgx.Tx, uuid.UUID, bool) error { return nil })
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Delegation example"})
	require.NoError(t, err)
	issuer := createRemoteIssuer(t, ctx, ti, "occupied-provider", "")
	config := createUserSessionIssuer(t, ctx, ti.conn, "occupied-config")
	client := createRemoteClient(t, ctx, ti, issuer, config.String(), "occupied-client")
	mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client)
	otherID := "other-editor-" + uuid.NewString()
	seedUser(t, ctx, ti.conn, otherID, "other@example.com", "Other editor")
	_, err = testrepo.New(ti.conn).CreateAttachmentMembershipFixture(ctx, testrepo.CreateAttachmentMembershipFixtureParams{OrganizationID: auth.ActiveOrganizationID, UserID: pgtype.Text{String: otherID, Valid: true}})
	require.NoError(t, err)
	otherAuth := *auth
	otherAuth.UserID = otherID
	otherCtx := contextvalues.WithValidatedGramSession(ctx, &otherAuth, false)
	theirs := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(otherID), config.String(), client)
	binding, err := ti.service.AttachBinding(otherCtx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: theirs.ID.String()})
	require.NoError(t, err)
	grant := remotesessions.RemoteGrant{Subject: mine.SubjectUrn, UserSessionIssuerID: config, RemoteSessionClientID: mine.RemoteSessionClientID, RemoteSessionID: mine.ID, GrantGeneration: mine.GrantGeneration}
	requireOopsCode(t, ti.service.DelegateGrant(ctx, agent.ID, grant, uuid.Nil), oops.CodeConflict)
	requireOopsCode(t, ti.service.DelegateGrant(ctx, agent.ID, grant, uuid.MustParse(binding.ID)), oops.CodeConflict)
	listed, err := ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, binding.ID, listed.Items[0].ID)
	require.Equal(t, theirs.ID.String(), listed.Items[0].RemoteSessionID)
	require.NotNil(t, listed.Items[0].RemoteSession)
}
