package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	cimdgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func seedTrustedRemoteSessionIssuerTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string, projectID uuid.NullUUID, organizationID pgtype.Text) uuid.UUID {
	t.Helper()

	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         projectID,
		OrganizationID:                    organizationID,
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		AuthorizationEndpoint:             pgtype.Text{String: "https://" + slug + ".example.com/authorize", Valid: true},
		TokenEndpoint:                     pgtype.Text{String: "https://" + slug + ".example.com/token", Valid: true},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

func TestOrganizationUserSessionIssuerTrustedRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	organizationID := pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true}
	organizationTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-org-target", uuid.NullUUID{}, organizationID)
	organizationTargetID := organizationTarget.String()
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-org-user-issuer",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &organizationTargetID,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *created.TrustedRemoteSessionIssuerID)

	mode := "interactive"
	unchanged, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                 created.ID,
		AuthnChallengeMode: &mode,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *unchanged.TrustedRemoteSessionIssuerID, "omitting the trust field must retain the link")

	organizationTargetURN := "urn:uuid:" + organizationTargetID
	normalized, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &organizationTargetURN,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *normalized.TrustedRemoteSessionIssuerID, "accepted UUID forms are persisted canonically")

	globalTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-global-target", uuid.NullUUID{}, pgtype.Text{})
	globalTargetID := globalTarget.String()
	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &globalTargetID,
	})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *updated.TrustedRemoteSessionIssuerID)

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *loaded.TrustedRemoteSessionIssuerID)

	clearValue := ""
	cleared, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &clearValue,
	})
	require.NoError(t, err)
	require.Nil(t, cleared.TrustedRemoteSessionIssuerID)

	projectTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-project-target", uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, organizationID)
	projectTargetID := projectTarget.String()
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &projectTargetID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	otherOrganizationID := "org-" + uuid.NewString()
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrganizationID,
		Name:        "Other Organization",
		Slug:        "other-organization-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{String: otherOrganizationID, Valid: true},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	foreignTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-foreign-target", uuid.NullUUID{}, pgtype.Text{String: otherOrganizationID, Valid: true})
	foreignTargetID := foreignTarget.String()
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &foreignTargetID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	badID := "not-a-uuid"
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &badID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationUserSessionIssuersCRUDAndListIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "shared-slug",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.Empty(t, first.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, first.OrganizationID)

	second, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "shared-slug",
		AuthnChallengeMode:   "interactive",
		SessionDurationHours: 12,
	})
	require.NoError(t, err, "organization slugs are not unique identifiers")
	createAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, createAudit.OrganizationID)
	require.False(t, createAudit.ProjectID.Valid)

	_, err = ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "project-only",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	mode := "interactive"
	duration := 48
	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            first.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          &duration,
		ClientIDMetadataAdmissionMode: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "interactive", updated.AuthnChallengeMode)
	require.Equal(t, 48, updated.SessionDurationHours)
	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, updateAudit.OrganizationID)
	require.False(t, updateAudit.ProjectID.Valid)

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: first.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, updated, loaded)

	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: second.ID, SessionToken: nil}))
	deleteAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, deleteAudit.OrganizationID)
	require.False(t, deleteAudit.ProjectID.Valid)
	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: second.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestProjectIssuerMutationsRejectOrganizationOwnedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "organization-owned",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	mode := "interactive"
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &projectgen.UpdateUserSessionIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ProjectSlugInput:              nil,
		ID:                            created.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.DeleteUserSessionIssuer(ctx, &projectgen.DeleteUserSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
}

func TestOrganizationUserSessionIssuerRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-rbac",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.GetIssuer(readCtx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	_, err = ti.service.ListIssuers(readCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	_, err = ti.service.GetIssuerDeletePreflight(readCtx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)

	_, err = ti.service.CreateIssuer(readCtx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "forbidden-create",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	mode := "interactive"
	_, err = ti.service.UpdateIssuer(readCtx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            created.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteIssuer(readCtx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestOrganizationUserSessionIssuerCimdClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-cimd",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	const clientURI = "https://org-cimd.example.com/client"
	_, err = ti.service.CreateUserSessionIssuerCimdClient(ctx, &cimdgen.CreateUserSessionIssuerCimdClientPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: clientURI,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	created, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		SessionToken:        nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: clientURI,
	})
	require.NoError(t, err)
	require.Empty(t, created.Client.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, created.Client.OrganizationID)
	require.Equal(t, clientURI, created.Client.ClientIDMetadataURI)
	addAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCimdClientAdd)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, addAudit.OrganizationID)
	require.False(t, addAudit.ProjectID.Valid)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	loaded, err := ti.service.GetCimdClient(readCtx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created.Client, loaded)
	listed, err := ti.service.ListCimdClients(readCtx, &orggen.ListCimdClientsPayload{
		UserSessionIssuerID: issuer.ID,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, []*types.UserSessionIssuerCimdClient{created.Client}, listed.Items)

	_, err = ti.service.CreateCimdClient(readCtx, &orggen.CreateCimdClientPayload{
		SessionToken:        nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: "https://forbidden.example.com/client",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.DeleteCimdClient(readCtx, &orggen.DeleteCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteUserSessionIssuerCimdClient(ctx, &cimdgen.DeleteUserSessionIssuerCimdClientPayload{
		ID:               created.Client.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetCimdClient(ctx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteCimdClient(ctx, &orggen.DeleteCimdClientPayload{ID: created.Client.ID, SessionToken: nil}))
	removeAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCimdClientRemove)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, removeAudit.OrganizationID)
	require.False(t, removeAudit.ProjectID.Valid)
	_, err = ti.service.GetCimdClient(ctx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuersRejectAPIKeyContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	apiKeyAuthCtx := *authCtx
	apiKeyAuthCtx.APIKeyID = uuid.NewString()
	apiKeyCtx := contextvalues.SetAuthContext(ctx, &apiKeyAuthCtx)

	_, err := ti.service.ListIssuers(apiKeyCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUserSessionIssuersRejectOverflowingDuration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "overflow-org",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 2562048,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	orgIssuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "overflow-org-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	overflowHours := 2562048
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            orgIssuer.ID,
		Slug:                          nil,
		AuthnChallengeMode:            nil,
		SessionDurationHours:          &overflowHours,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "overflow-project",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 2562048,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	projectIssuer, err := ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "overflow-project-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &projectgen.UpdateUserSessionIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ProjectSlugInput:              nil,
		ID:                            projectIssuer.ID,
		Slug:                          nil,
		AuthnChallengeMode:            nil,
		SessionDurationHours:          &overflowHours,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationUserSessionIssuerDeletePreflight(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-preflight",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(created.ID)
	siblingID := createSiblingProject(t, ctx, ti.conn, "org-preflight-sibling")

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              siblingID,
		Name:                   "Preflight toolset",
		Slug:                   "preflight-toolset",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           siblingID,
	})
	require.NoError(t, err)

	serverID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             siblingID,
		Name:                  pgtype.Text{String: "Preflight server", Valid: true},
		Slug:                  pgtype.Text{String: "preflight-server", Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuerID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "org-preflight-client")
	require.NoError(t, err)
	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, urn.NewUserSubject("org-preflight-user"))
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, 1, preflight.ClientCount)
	require.Equal(t, 1, preflight.LiveSessionCount)
	require.False(t, preflight.CanDelete)
	require.Equal(t, serverID.String(), preflight.McpServers[0].ID)
	require.Equal(t, "Preflight server", preflight.McpServers[0].Name)
	require.Equal(t, siblingID.String(), preflight.McpServers[0].ProjectID)
	require.Equal(t, toolset.ID.String(), preflight.Toolsets[0].ID)
	require.Equal(t, "Preflight toolset", preflight.Toolsets[0].Name)

	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}
