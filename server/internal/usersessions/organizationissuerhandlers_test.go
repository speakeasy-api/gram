package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestOrganizationUserSessionIssuersCRUDAndListIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		Slug:                 "shared-slug",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.Empty(t, first.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, first.OrganizationID)

	second, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
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

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	mode := "interactive"
	duration := 48
	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
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

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: first.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, updated, loaded)

	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: second.ID, SessionToken: nil, ApikeyToken: nil}))
	deleteAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, deleteAudit.OrganizationID)
	require.False(t, deleteAudit.ProjectID.Valid)
	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: second.ID, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestProjectIssuerMutationsRejectOrganizationOwnedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
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

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
}

func TestOrganizationUserSessionIssuerRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		Slug:                 "org-rbac",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.GetIssuer(readCtx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	_, err = ti.service.ListIssuers(readCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	_, err = ti.service.GetIssuerDeletePreflight(readCtx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)

	_, err = ti.service.CreateIssuer(readCtx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		Slug:                 "forbidden-create",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	mode := "interactive"
	_, err = ti.service.UpdateIssuer(readCtx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ID:                            created.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteIssuer(readCtx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestOrganizationUserSessionIssuerDeletePreflight(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
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

	preflight, err := ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, 1, preflight.ClientCount)
	require.Equal(t, 1, preflight.LiveSessionCount)
	require.False(t, preflight.CanDelete)
	require.Equal(t, serverID.String(), preflight.McpServers[0].ID)
	require.Equal(t, "Preflight server", preflight.McpServers[0].Name)
	require.Equal(t, siblingID.String(), preflight.McpServers[0].ProjectID)
	require.Equal(t, toolset.ID.String(), preflight.Toolsets[0].ID)
	require.Equal(t, "Preflight toolset", preflight.Toolsets[0].Name)

	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}
