package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestBindingRefreshRequiresExactPopupGrant(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "unrelated sign-in", "wrong callback person", "wrong callback client", "grant overwritten", "attachment replaced", "permission revoked", "cancelled", "wrong endpoint", "expired attempt"} {
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
					return fmt.Errorf("authorize refresh fixture: %w", err)
				}
				return nil
			})
			attempts := remotesessions.NewBindingRefreshAttempts(testenv.NewLogger(t), ti.redisCache)
			ti.service.SetBindingRefreshAttempts(attempts)
			agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, Name: "Popup refresh agent"})
			require.NoError(t, err)
			issuer := createRemoteIssuer(t, ctx, ti, "refresh-provider", "")
			config := createUserSessionIssuer(t, ctx, ti.conn, "refresh-config")
			client := createRemoteClient(t, ctx, ti, issuer, config.String(), "refresh-client")
			mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(auth.UserID), config.String(), client)
			binding, err := ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: mine.ID.String()})
			require.NoError(t, err)
			start := &gen.BeginBindingRefreshPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), BindingID: binding.ID}
			attempt, err := ti.service.BeginBindingRefresh(ctx, start)
			require.NoError(t, err)
			complete := &gen.CompleteBindingRefreshPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), AttemptID: attempt.AttemptID}
			if scenario == "wrong endpoint" {
				require.Error(t, attempts.Start(ctx, attempt.AttemptID, auth.ActiveOrganizationID, uuid.New(), config))
			} else {
				require.NoError(t, attempts.Start(ctx, attempt.AttemptID, auth.ActiveOrganizationID, *auth.ProjectID, config))
				require.Error(t, attempts.Start(ctx, attempt.AttemptID, auth.ActiveOrganizationID, *auth.ProjectID, config), "one attempt cannot start two popups")
			}
			pendingClient, err := attempts.PendingClient(ctx, attempt.AttemptID, mine.SubjectUrn)
			require.NoError(t, err)
			if scenario == "wrong endpoint" {
				require.Equal(t, uuid.Nil, pendingClient)
			} else {
				require.Equal(t, mine.RemoteSessionClientID, pendingClient)
			}
			renewed := insertRemoteSession(t, ctx, ti.conn, mine.SubjectUrn, config.String(), client)
			grant := remotesessions.RemoteGrant{ParentChallengeID: attempt.AttemptID, UserSessionIssuerID: config, RemoteSessionClientID: renewed.RemoteSessionClientID, Subject: renewed.SubjectUrn, RemoteSessionID: renewed.ID, RemoteSessionUpdatedAt: renewed.UpdatedAt.Time, GrantGeneration: renewed.GrantGeneration}
			switch scenario {
			case "unrelated sign-in":
				grant.ParentChallengeID = uuid.NewString()
			case "wrong callback person":
				grant.Subject = urn.NewUserSubject("other-person")
			case "wrong callback client":
				grant.RemoteSessionClientID = uuid.New()
			}
			if scenario != "cancelled" {
				require.NoError(t, attempts.Record(ctx, grant.ParentChallengeID, grant))
			}
			switch scenario {
			case "expired attempt":
				key := "bindingRefresh:" + attempt.AttemptID + ":"
				require.NoError(t, ti.redisCache.Expire(ctx, key, -time.Second))
				var expired map[string]any
				require.Error(t, ti.redisCache.Get(ctx, key, &expired))
			case "grant overwritten":
				later := insertRemoteSession(t, ctx, ti.conn, mine.SubjectUrn, config.String(), client)
				require.Greater(t, later.GrantGeneration, renewed.GrantGeneration)
			case "attachment replaced":
				require.NoError(t, ti.service.DetachBinding(ctx, &gen.DetachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), ID: binding.ID}))
				replacement, err := ti.service.AttachBinding(ctx, &gen.AttachBindingPayload{PrincipalID: agent.ID.String(), UserSessionIssuerID: config.String(), RemoteSessionID: renewed.ID.String()})
				require.NoError(t, err)
				binding = replacement
			case "permission revoked":
				denied = true
			}
			result, err := ti.service.CompleteBindingRefresh(ctx, complete)
			switch scenario {
			case "grant overwritten", "attachment replaced", "permission revoked", "expired attempt":
				require.Error(t, err)
			case "success":
				require.NoError(t, err)
				require.True(t, result.Completed)
				require.NotNil(t, result.Binding)
				require.NotEqual(t, binding.ID, result.Binding.ID)
			default:
				require.NoError(t, err)
				require.False(t, result.Completed)
			}
			denied = false
			rows, err := repo.New(ti.conn).ListPrincipalRemoteSessionBindings(ctx, repo.ListPrincipalRemoteSessionBindingsParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: config})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			if scenario == "success" {
				require.Equal(t, renewed.GrantGeneration, rows[0].GrantGeneration)
			} else {
				require.Equal(t, binding.ID, rows[0].ID.String(), "failed completion preserves the original assignment")
			}
			if scenario == "permission revoked" {
				retried, err := ti.service.CompleteBindingRefresh(ctx, complete)
				require.NoError(t, err)
				require.True(t, retried.Completed, "unauthorized completion must not consume the receipt")
			} else {
				require.NoError(t, attempts.Record(ctx, grant.ParentChallengeID, grant), "late callback cannot resurrect a consumed receipt")
				_, err := ti.service.CompleteBindingRefresh(ctx, complete)
				require.Error(t, err, "receipt is one-time")
			}
		})
	}
}
