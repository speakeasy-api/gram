package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

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
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          siblingProject.ID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               "sibling-workforce",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
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
