package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func TestSetUserSessionIssuer_AttachesOrganizationIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Organization authenticated toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "shared-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	issuerID := issuer.ID.String()

	updated, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		Slug:                toolset.Slug,
		UserSessionIssuerID: &issuerID,
	})
	require.NoError(t, err)
	require.Equal(t, issuerID, *updated.UserSessionIssuerID)
}

func TestSetUserSessionIssuer_RejectsSiblingProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Sibling issuer toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	projectSlug := "sibling-" + uuid.NewString()[:8]
	siblingProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Sibling project",
		Slug:           projectSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	issuerID := createProjectIssuer(t, ctx, ti, siblingProject.ID, "sibling-workforce").String()

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		Slug:                toolset.Slug,
		UserSessionIssuerID: &issuerID,
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// Only the hosted wrapper (id == toolset id) follows the toolset's issuer.
func TestSetUserSessionIssuer_LeavesIndependentWrappersAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	toolset := createMinimalPrivateToolset(t, ctx, ti, "Shared backing toolset")
	toolsetID := uuid.MustParse(toolset.ID)
	mode := types.NetworkAccessMode("public_only")
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)

	wrapperIssuer := createProjectIssuer(t, ctx, ti, projectID, "independent-wrapper-issuer")
	toolsetIssuer := createProjectIssuer(t, ctx, ti, projectID, "toolset-issuer")

	servers := mcpserversrepo.New(ti.conn)
	independent, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                pgtype.Text{String: "Independent wrapper", Valid: true},
		Slug:                pgtype.Text{String: "independent-" + uuid.NewString()[:8], Valid: true},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: wrapperIssuer, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	issuerOf := func(serverID uuid.UUID) uuid.NullUUID {
		server, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
		require.NoError(t, err)
		return server.UserSessionIssuerID
	}

	linked := toolsetIssuer.String()
	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &linked})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: toolsetIssuer, Valid: true}, issuerOf(toolsetID), "the hosted wrapper follows its toolset")
	require.Equal(t, uuid.NullUUID{UUID: wrapperIssuer, Valid: true}, issuerOf(independent.ID), "an independent wrapper keeps its own issuer")

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
	require.NoError(t, err)
	require.False(t, issuerOf(toolsetID).Valid, "unlinking the toolset unlinks its hosted wrapper")
	require.Equal(t, uuid.NullUUID{UUID: wrapperIssuer, Valid: true}, issuerOf(independent.ID), "unlinking the toolset leaves an independent wrapper's issuer alone")
}

func TestSetUserSessionIssuer_RejectsForeignOrganizationIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Foreign issuer toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	foreignOrganizationID := "org-" + uuid.NewString()
	require.NoError(t, organizationsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, organizationsrepo.CreateOrganizationMetadataParams{
		ID:   foreignOrganizationID,
		Name: "Foreign organization",
		Slug: "foreign-" + uuid.NewString(),
	}))
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: foreignOrganizationID, Valid: true},
		Slug:                         "foreign-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	issuerID := issuer.ID.String()

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		Slug:                toolset.Slug,
		UserSessionIssuerID: &issuerID,
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
