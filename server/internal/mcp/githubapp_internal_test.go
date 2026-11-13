package mcp

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/authz"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDecideGitHubApp(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	live := upstreamclientsrepo.GetAuthClientSelectionRow{
		GithubApp: true, AuthClientID: validUUID(id), ClientID: validUUID(id),
		ClientType:    pgtype.Text{String: authClientTypeServiceAccount, Valid: true},
		ClientEnabled: pgtype.Bool{Bool: true, Valid: true}, Associated: true,
	}
	res := decideAuthClient(live)
	require.Equal(t, authClientResolved, res.outcome)
	require.True(t, res.headersOnly())
	require.False(t, res.choice.clientID.Valid, "GitHub does not fabricate an OAuth registration")
	for _, tc := range []struct {
		name   string
		mutate func(*upstreamclientsrepo.GetAuthClientSelectionRow)
	}{
		{"stale headers", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }},
		{"disabled", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientEnabled.Bool = false }},
		{"detached", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Associated = false }},
		{"deleted", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientDeleted.Bool = true }},
		{"foreign", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.ClientID.Valid = false }},
		{"oauth registration", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationID = validUUID(uuid.New()) }},
		{"tunnel", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }},
		{"missing auxiliary header", func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.CredentialHeadersUncovered = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := live
			tc.mutate(&row)
			require.Equal(t, authClientGap, decideAuthClient(row).outcome)
		})
	}
}

func TestGitHubSelectionMissing(t *testing.T) {
	t.Parallel()
	require.True(t, githubSelectionMissing(nil))
	require.True(t, githubSelectionMissing(&authClientResolution{outcome: authClientLegacy}))
	require.False(t, githubSelectionMissing(&authClientResolution{outcome: authClientResolved, clientID: uuid.New(), githubApp: true}))
	require.False(t, githubSelectionMissing(&authClientResolution{outcome: authClientGap}))
}

func TestGitHubAppAuthorizationRechecksRevokedPermission(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}
	admissions := 0
	service := &Service{authz: authz.NewEngine(testenv.NewLogger(t), nil,
		func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
			AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
				admissions++
				return authz.PrincipalCredentialAdmission{
					OwnerUserID: "user_owner", Credential: grants, Agent: grants, Owner: grants,
					Authorizer: authz.RepresentedUserPolicy{UserID: "user_owner", Policy: grants}, LegacyAuthCeiling: false,
				}, nil
			},
		})}
	orgID := "org_test"
	agentID, clientID := uuid.New(), uuid.New()
	ctx := admittedAgentContext(t, orgID, agentID)
	permit := authClientPermitRequest{OrganizationID: orgID, ProjectID: uuid.New(), ServerID: uuid.New(), ClientID: clientID, ClientType: authClientTypeServiceAccount, Subject: urn.NewAgentSubject(agentID)}
	res := authClientResolution{outcome: authClientResolved, clientID: clientID, clientType: authClientTypeServiceAccount, githubApp: true}
	require.NoError(t, service.authorizeGitHubApp(ctx, permit, res))
	grants = []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource)}
	require.ErrorContains(t, service.authorizeGitHubApp(ctx, permit, res), "no longer authorized")
	require.Equal(t, 2, admissions, "renewal must read current authorization, not a captured decision")
}
