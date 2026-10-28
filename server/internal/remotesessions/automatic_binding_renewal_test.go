package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCompleteRemoteLogin_AutomaticExpiredBindingRenewal(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"expired own", "authorization expired", "valid grant", "refreshable", "other owner", "revoked", "older generation", "permission denied", "untrusted callback", "wrong issuer", "concurrent removal", "concurrent reauthentication"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var spy upstreamSpy
			ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "auto-renew", &spy)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			subject := urn.NewUserSubject(auth.UserID)
			fx.parent.Subject = &subject
			agent, err := agentrepo.New(fx.ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: auth.ActiveOrganizationID, OwnerUserID: auth.UserID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Name: "Renewal example"})
			require.NoError(t, err)
			sourceSubject := subject
			if scenario == "other owner" {
				sourceSubject = urn.NewUserSubject("another-owner")
			}
			now := time.Now().Truncate(time.Microsecond)
			expiry := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
			refresh := pgtype.Text{}
			authorizationExpiry := pgtype.Timestamptz{}
			if scenario == "valid grant" || scenario == "authorization expired" {
				expiry.Time = now.Add(time.Hour)
			}
			if scenario == "authorization expired" {
				authorizationExpiry = pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
			}
			if scenario == "refreshable" {
				refresh = pgtype.Text{String: "encrypted-refresh", Valid: true}
			}
			q := repo.New(fx.ti.conn)
			bindingIssuer := fx.userSessionIssuerID
			if scenario == "wrong issuer" {
				bindingIssuer = createUserSessionIssuer(t, ctx, fx.ti.conn, "other-renewal-config")
				require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: fx.clients[0].ID, UserSessionIssuerID: bindingIssuer}))
			}
			source, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: sourceSubject, UserSessionIssuerID: bindingIssuer, RemoteSessionClientID: fx.clients[0].ID, AccessTokenEncrypted: "ciphertext", AccessExpiresAt: expiry, AuthorizationExpiresAt: authorizationExpiry, RefreshTokenEncrypted: refresh, Scopes: []string{"tools:read"}})
			require.NoError(t, err)
			_, err = testrepo.New(fx.ti.conn).InsertAttachmentFromSourceFixture(ctx, testrepo.InsertAttachmentFromSourceFixtureParams{Project: *auth.ProjectID, Organization: auth.ActiveOrganizationID, Agent: agent.ID, Requesting: bindingIssuer, Client: source.RemoteSessionClientID, Source: source.ID})
			require.NoError(t, err)
			lookup := repo.ListPrincipalRemoteSessionBindingsParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: bindingIssuer}
			before, err := q.ListPrincipalRemoteSessionBindings(ctx, lookup)
			require.NoError(t, err)
			require.Len(t, before, 1)
			if scenario == "revoked" {
				_, err = testrepo.New(fx.ti.conn).RevokeAgentAttachmentsFixture(ctx, testrepo.RevokeAgentAttachmentsFixtureParams{ProjectID: *auth.ProjectID, PrincipalID: agent.ID})
				require.NoError(t, err)
			}
			if scenario == "older generation" {
				_, err = testrepo.New(fx.ti.conn).MutateAttachmentSupersededGrantGenerationFixture(ctx, source.ID)
				require.NoError(t, err)
			}
			authorizer := agentmanagement.NewAuthorizer(bindingOwnerOnlyEngine{})
			fx.ti.service.SetBindingAuthorizer(func(callCtx context.Context, tx pgx.Tx, id uuid.UUID, write bool) error {
				if scenario == "permission denied" {
					return oops.C(oops.CodeForbidden)
				}
				_, _, err := authorizer.RequireAgentOwnerForUpdate(callCtx, tx, id, agentmanagement.OwnedAgentSetup)
				if err != nil {
					return fmt.Errorf("authorize renewal test: %w", err)
				}
				if scenario == "concurrent removal" {
					_, err = testrepo.New(fx.ti.conn).MutateAttachmentDetachedBindingFixture(callCtx, testrepo.MutateAttachmentDetachedBindingFixtureParams{Project: *auth.ProjectID, Agent: agent.ID})
				}
				if scenario == "concurrent reauthentication" {
					_, err = testrepo.New(fx.ti.conn).MutateAttachmentSupersededGrantGenerationFixture(callCtx, source.ID)
				}
				if err != nil {
					return fmt.Errorf("mutate concurrent renewal fixture: %w", err)
				}
				return nil
			})
			fx.mgr.SetAutomaticBindingRenewal(fx.ti.service, func(context.Context, remotesessions.RemoteLoginState) (bool, error) {
				return scenario != "untrusted callback", nil
			})
			authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
			require.NoError(t, err)
			parsed, err := url.Parse(authURL)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code=fake-code&state="+url.QueryEscape(parsed.Query().Get("state")), nil)
			result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
			require.NoError(t, err)
			require.NotNil(t, result.Grant)
			after, err := q.ListPrincipalRemoteSessionBindings(ctx, lookup)
			require.NoError(t, err)
			if scenario == "concurrent removal" || scenario == "revoked" {
				require.Empty(t, after)
				return
			}
			require.Len(t, after, 1)
			require.Equal(t, before[0].ID, after[0].ID)
			if scenario == "expired own" || scenario == "authorization expired" {
				require.Equal(t, result.Grant.GrantGeneration, after[0].GrantGeneration)
				require.Greater(t, after[0].GrantGeneration, before[0].GrantGeneration)
			} else {
				require.Equal(t, before[0].GrantGeneration, after[0].GrantGeneration)
			}
		})
	}
}
