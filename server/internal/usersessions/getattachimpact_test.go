package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

func TestGetAttachImpactBetweenPerServerIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	currentID := seedIssuer(t, ctx, ti, "impact-current")
	candidateID := seedIssuer(t, ctx, ti, "impact-candidate")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, currentID, "impact-toolset")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(candidateID.String())})
	require.NoError(t, err)
	require.Equal(t, currentID.String(), impact.CurrentIssuer.ID)
	require.Equal(t, candidateID.String(), impact.CandidateIssuer.ID)
	// Clients belong to the current issuer, but the server keeps advertising
	// its own per-server authorization server.
	require.True(t, impact.ReauthorizationRequired)
	require.False(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactOntoSharedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	currentID := seedIssuer(t, ctx, ti, "impact-endpoint")
	sharedID := createOrganizationIssuer(t, ctx, ti, "impact-shared")
	setIssuerShared(t, ctx, ti, sharedID, "")
	backing := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, currentID, "impact-backing")
	serverID := attachIssuerMCPServer(t, ctx, ti, *authCtx.ProjectID, currentID, backing, "impact-server")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{McpServerID: new(serverID.String()), UserSessionIssuerID: new(sharedID.String())})
	require.NoError(t, err)
	require.True(t, impact.ReauthorizationRequired)
	require.True(t, impact.AuthorizationServerChanges)
	require.Equal(t, string(authserver.ModeShared), impact.CandidateIssuer.AuthorizationServerMode)
	require.Equal(t, testServerURL+authserver.SharedPath(sharedID), *impact.CandidateIssuer.IssuerURL)
}

func TestGetAttachImpactKeepingTheIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-same")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-same-toolset")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())})
	require.NoError(t, err)
	require.False(t, impact.ReauthorizationRequired)
	require.False(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactFirstIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-first")
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: "impact-unattached", Slug: "impact-unattached", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: false,
	})
	require.NoError(t, err)

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())})
	require.NoError(t, err)
	require.Nil(t, impact.CurrentIssuer)
	require.False(t, impact.ReauthorizationRequired, "no client is registered with an issuer yet")
	require.True(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactDetach(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "impact-detach")
	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "impact-meta",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	})
	require.NoError(t, err)

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{MetaMcpServerID: new(meta.ID.String())})
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), impact.CurrentIssuer.ID)
	require.Nil(t, impact.CandidateIssuer)
	require.True(t, impact.ReauthorizationRequired)
	require.True(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactNamesExactlyOneServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(uuid.NewString()), MetaMcpServerID: new(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetAttachImpactRefusesUnknownServerAndHiddenIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-visible")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-visible-toolset")
	sibling := seedSiblingProject(t, ctx, ti, "impact-sibling")

	_, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{McpServerID: new(uuid.NewString()), UserSessionIssuerID: new(issuerID.String())})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Another project's issuer cannot be attached here, so it is not found.
	_, err = ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(sibling.issuerID.String())})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetAttachImpactRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-rbac")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-rbac-toolset")
	payload := &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())}

	writeCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()))
	_, err := ti.service.GetAttachImpact(writeCtx, payload)
	require.NoError(t, err)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.GetAttachImpact(readCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}
