package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestIssuanceProfilesForClientsAndWorkloads(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	shared := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared token")
	associate(t, ctx, ti.conn, ti.organizationID, server, shared)
	personal := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "subject")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	own, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeRemoteSession, Name: "Own account", ProjectID: new(ti.projectID.String()),
		RemoteSessionClientID: new(personal.String()), McpServerIds: []string{server.id.String()},
	})
	require.NoError(t, err)

	issuerID := uuid.New()
	require.NoError(t, selectionbackfill.New(ti.conn).SeedUserSessionIssuerFixture(ctx, selectionbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, OrganizationID: pgtype.Text{String: ti.organizationID, Valid: true}, Slug: "usi-" + issuerID.String()[:8]}))
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{ClientID: "headless", ClientName: "Headless", RedirectUris: []string{}, TokenEndpointAuthMethod: "none", UserSessionIssuerID: issuerID})
	require.NoError(t, err)
	workloadIssuer, err := testrepo.New(ti.conn).CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{OrganizationID: ti.organizationID, Name: "CI", Issuer: "https://ci.example.com", JwksUri: "https://ci.example.com/jwks"})
	require.NoError(t, err)

	created := auditCount(t, ti, audit.ActionIssuanceSelectionProfileCreate)
	profile, err := ti.service.SetIssuanceProfile(ctx, &gen.SetIssuanceProfilePayload{
		UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), Name: "Reporting client",
		Choices: []*types.IssuanceProfileChoice{{McpServerID: server.id.String(), AuthClientID: new(own.ID)}},
	})
	require.NoError(t, err)
	require.Len(t, profile.Choices, 1)
	require.Equal(t, own.ID, *profile.Choices[0].AuthClientID, "a client's sessions may use the person's own connection")
	require.Equal(t, created+1, auditCount(t, ti, audit.ActionIssuanceSelectionProfileCreate))

	updated, err := ti.service.SetIssuanceProfile(ctx, &gen.SetIssuanceProfilePayload{
		UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), Name: "Reporting client",
		Choices: []*types.IssuanceProfileChoice{{McpServerID: server.id.String(), AuthClientID: new(shared.String())}},
	})
	require.NoError(t, err)
	require.Equal(t, profile.ID, updated.ID, "a caller has one profile per sign-in")
	require.Equal(t, shared.String(), *updated.Choices[0].AuthClientID)

	_, err = ti.service.SetIssuanceProfile(ctx, &gen.SetIssuanceProfilePayload{
		UserSessionIssuerID: issuerID.String(), WorkloadIssuerID: new(workloadIssuer.String()), WorkloadSubject: new("repo:example"), Name: "CI",
		Choices: []*types.IssuanceProfileChoice{{McpServerID: server.id.String(), AuthClientID: new(own.ID)}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	workloadProfile, err := ti.service.SetIssuanceProfile(ctx, &gen.SetIssuanceProfilePayload{
		UserSessionIssuerID: issuerID.String(), WorkloadIssuerID: new(workloadIssuer.String()), WorkloadSubject: new("repo:example"), Name: "CI",
		Choices: []*types.IssuanceProfileChoice{{McpServerID: server.id.String(), AuthClientID: new(shared.String())}},
	})
	require.NoError(t, err)

	invalid := map[string]*gen.SetIssuanceProfilePayload{
		"both callers":       {UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), WorkloadIssuerID: new(workloadIssuer.String()), WorkloadSubject: new("x"), Name: "x"},
		"no caller":          {UserSessionIssuerID: issuerID.String(), Name: "x"},
		"subject only":       {UserSessionIssuerID: issuerID.String(), WorkloadSubject: new("x"), Name: "x"},
		"no-auth where auth": {UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), Name: "x", Choices: []*types.IssuanceProfileChoice{{McpServerID: server.id.String()}}},
		"empty choice":       {UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), Name: "x", Choices: []*types.IssuanceProfileChoice{nil}},
	}
	for _, payload := range invalid {
		_, err := ti.service.SetIssuanceProfile(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	listed, err := ti.service.ListIssuanceProfiles(ctx, &gen.ListIssuanceProfilesPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	require.NoError(t, ti.service.DeleteIssuanceProfile(ctx, &gen.DeleteIssuanceProfilePayload{ID: workloadProfile.ID}))
	listed, err = ti.service.ListIssuanceProfiles(ctx, &gen.ListIssuanceProfilesPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)

	readOnly := withOrgGrants(t, ctx, authz.ScopeOrgRead)
	_, err = ti.service.SetIssuanceProfile(readOnly, &gen.SetIssuanceProfilePayload{UserSessionIssuerID: issuerID.String(), UserSessionClientID: new(client.ID.String()), Name: "x"})
	requireOopsCode(t, err, oops.CodeForbidden)
}
