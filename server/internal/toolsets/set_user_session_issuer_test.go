package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
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

func TestSetUserSessionIssuer_HonorsProjectScopedAuthorization(t *testing.T) {
	t.Parallel()

	projectGrant := func(projectID string, scope authz.Scope) authz.Grant {
		return authz.NewGrantWithSelector(scope, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		})
	}

	for _, tc := range []struct {
		name    string
		grants  func(string) []authz.Grant
		allowed bool
	}{
		{name: "matching project grant", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{projectGrant(projectID, authz.ScopeMCPWrite)}
		}, allowed: true},
		{name: "other project grant", grants: func(string) []authz.Grant {
			return []authz.Grant{projectGrant(uuid.NewString(), authz.ScopeMCPWrite)}
		}},
		{name: "matching project exclusion", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(projectID, authz.ScopeMCPBlockedWrite),
			}
		}},
		{name: "other project exclusion", grants: func(string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(uuid.NewString(), authz.ScopeMCPBlockedWrite),
			}
		}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestToolsetsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
				Name:         "Project authorization issuer toolset",
				ToolUrns:     []string{},
				ResourceUrns: []string{},
			})
			require.NoError(t, err)

			ctx = authztest.WithExactGrants(t, ctx, tc.grants(authCtx.ProjectID.String())...)
			_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeForbidden, oopsErr.Code)
			}
		})
	}
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
