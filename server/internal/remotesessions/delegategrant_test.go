package remotesessions_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDelegateGrantExactAuthorization(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"new", "renew", "replacement session", "later grant", "wrong person", "wrong source", "wrong project", "permission revoked", "changed assignment"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
			authorizer := agentmanagement.NewAuthorizer(bindingOwnerOnlyEngine{})
			denied := false
			ti.service.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID, write bool) error {
				if denied {
					return oops.C(oops.CodeForbidden)
				}
				predicate := agentmanagement.OwnedAgentAuthorize
				if write {
					predicate = agentmanagement.OwnedAgentSetup
				}
				_, _, err := authorizer.RequireAgentOwnerForUpdate(ctx, tx, id, predicate)
				if err != nil {
					return fmt.Errorf("authorize delegation test: %w", err)
				}
				return nil
			})
			agentProject := uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}
			if scenario == "wrong project" {
				agentProject = uuid.NullUUID{}
			}
			agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, ProjectID: agentProject, Name: "Delegation example"})
			require.NoError(t, err)
			issuer := createRemoteIssuer(t, ctx, ti, "delegation-provider", "")
			config := createUserSessionIssuer(t, ctx, ti.conn, "delegation-config")
			client := createRemoteClient(t, ctx, ti, issuer, config.String(), "delegation-client")
			mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client)
			expected := uuid.Nil
			if scenario != "new" {
				binding, err := ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: mine.ID.String()})
				require.NoError(t, err)
				expected = uuid.MustParse(binding.ID)
				if scenario == "replacement session" {
					_, err := repo.New(ti.conn).SoftDeleteRemoteSessionsByClientID(ctx, mine.RemoteSessionClientID)
					require.NoError(t, err)
				}
				mine = insertRemoteSession(t, ctx, ti.conn, mine.SubjectUrn, config.String(), client)
			}
			grant := remotesessions.RemoteGrant{ConnectionPopup: true, ParentChallengeID: uuid.NewString(), UserSessionIssuerID: config, RemoteSessionClientID: mine.RemoteSessionClientID, Subject: mine.SubjectUrn, RemoteSessionID: mine.ID, RemoteSessionUpdatedAt: mine.UpdatedAt.Time, GrantGeneration: mine.GrantGeneration}
			switch scenario {
			case "later grant":
				insertRemoteSession(t, ctx, ti.conn, mine.SubjectUrn, config.String(), client)
			case "wrong person":
				grant.Subject = urn.NewUserSubject("another-person")
			case "wrong source":
				otherIssuer := createRemoteIssuer(t, ctx, ti, "other-provider", "")
				otherClient := createRemoteClient(t, ctx, ti, otherIssuer, config.String(), "other-client")
				other := insertRemoteSession(t, ctx, ti.conn, mine.SubjectUrn, config.String(), otherClient)
				grant.RemoteSessionID = other.ID
			case "permission revoked":
				denied = true
			case "changed assignment":
				require.NoError(t, ti.service.DetachBinding(ctx, &gen.DetachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), ID: expected.String()}))
			}
			err = ti.service.DelegateGrant(ctx, agent.ID, grant, expected)
			if scenario == "renew" || scenario == "replacement session" {
				require.NoError(t, err)
				bindings, err := ti.service.ListBindings(ctx, &gen.ListBindingsPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String()})
				require.NoError(t, err)
				require.Len(t, bindings.Items, 1)
				require.NotNil(t, bindings.Items[0].RemoteSession)
				require.Equal(t, mine.ID.String(), bindings.Items[0].RemoteSessionID)
			} else {
				require.Error(t, err)
			}
		})
	}
}
