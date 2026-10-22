package workloadpolicy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func listTokenEndpointsPayload() *gen.ListTokenEndpointsPayload {
	return &gen.ListTokenEndpointsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

var oneDay = pgtype.Interval{Microseconds: int64(24 * time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true}

func newOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) usersessions_repo.UserSessionIssuer {
	t.Helper()

	issuer, err := usersessions_repo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessions_repo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(ti.orgID),
		Slug:                         slug,
		AuthnChallengeMode:           "interactive",
		SessionDuration:              oneDay,
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	return issuer
}

func newProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) usersessions_repo.UserSessionIssuer {
	t.Helper()

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     conv.ToPGText(ti.orgID),
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    oneDay,
	})
	require.NoError(t, err)

	return issuer
}

func setShared(t *testing.T, ctx context.Context, ti *testInstance, issuer usersessions_repo.UserSessionIssuer, pinnedIssuerURL string) {
	t.Helper()

	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: string(authserver.ModeShared),
		PinnedIssuerUrl:         pgtype.Text{String: pinnedIssuerURL, Valid: pinnedIssuerURL != ""},
		IssuerID:                issuer.ID,
		OrganizationID:          ti.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
}

func TestListTokenEndpoints_ListsSharedIssuersAtBothLevels(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	projectIssuer := newProjectIssuer(t, ctx, ti, ti.projectID, "project-issuer")
	setShared(t, ctx, ti, projectIssuer, "")
	orgIssuer := newOrganizationIssuer(t, ctx, ti, "org-issuer")
	setShared(t, ctx, ti, orgIssuer, "")

	result, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	org := result.Items[0]
	require.Equal(t, orgIssuer.ID.String(), org.UserSessionIssuerID)
	require.Equal(t, "org-issuer", org.UserSessionIssuerSlug)
	require.Empty(t, org.ProjectID)
	require.Empty(t, org.ProjectName)
	require.Equal(t, testServerURL+"/oauth/usi/"+orgIssuer.ID.String(), org.Issuer)
	require.Equal(t, testServerURL+"/oauth/usi/"+orgIssuer.ID.String()+"/token", org.TokenEndpoint)
	require.Equal(t, "gram.example.com", org.McpHost)

	project := result.Items[1]
	require.Equal(t, projectIssuer.ID.String(), project.UserSessionIssuerID)
	require.Equal(t, ti.projectID.String(), project.ProjectID)
	require.NotEmpty(t, project.ProjectName)
	require.Equal(t, testServerURL+"/oauth/usi/"+projectIssuer.ID.String()+"/token", project.TokenEndpoint)
}

func TestListTokenEndpoints_LeavesOutEndpointModeIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	newOrganizationIssuer(t, ctx, ti, "endpoint-issuer")
	newProjectIssuer(t, ctx, ti, ti.projectID, "endpoint-project-issuer")

	result, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

// A shared authorization server on the authentication host, pinned there or
// derived for an issuer that opts in, lists its token endpoint on that host,
// while its MCP servers stay on the server URL's host.
func TestListTokenEndpoints_ListsAuthenticationHostTokenEndpoints(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	pinned := newOrganizationIssuer(t, ctx, ti, "pinned-issuer")
	setShared(t, ctx, ti, pinned, testAuthenticationHostURL+authserver.SharedPath(pinned.ID))
	optedIn := newProjectIssuer(t, ctx, ti, ti.projectID, "opted-in-issuer")
	setShared(t, ctx, ti, optedIn, "")
	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerUseAuthenticationHostFixture(ctx, testrepo.SetUserSessionIssuerUseAuthenticationHostFixtureParams{
		UseAuthenticationHost: true,
		IssuerID:              optedIn.ID,
		OrganizationID:        ti.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)

	result, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	for _, item := range result.Items {
		issuerURL := testAuthenticationHostURL + "/oauth/usi/" + item.UserSessionIssuerID
		require.Equal(t, issuerURL, item.Issuer)
		require.Equal(t, issuerURL+"/token", item.TokenEndpoint)
		require.Equal(t, "gram.example.com", item.McpHost)
	}
}

// An issuer pinned to a host this deployment does not serve falls back to
// per-endpoint authorization servers, so it has no shared token endpoint.
func TestListTokenEndpoints_LeavesOutIssuersPinnedToAnUnservedHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := newOrganizationIssuer(t, ctx, ti, "elsewhere")
	setShared(t, ctx, ti, issuer, "https://elsewhere.example.com"+authserver.SharedPath(issuer.ID))

	result, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestListTokenEndpoints_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListTokenEndpoints(withScopes(t, ctx, ti), listTokenEndpointsPayload())
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.ListTokenEndpoints(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), listTokenEndpointsPayload())
	require.NoError(t, err)
}

// A project's API key sees the organization's issuers and its own project's,
// never a sibling project's; a dashboard session sees them all.
func TestListTokenEndpoints_ScopesAnAPIKeyToItsProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	sibling, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           "sibling",
		Slug:           "sibling-" + uuid.NewString()[:8],
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	orgIssuer := newOrganizationIssuer(t, ctx, ti, "org-issuer")
	setShared(t, ctx, ti, orgIssuer, "")
	ownIssuer := newProjectIssuer(t, ctx, ti, ti.projectID, "own-issuer")
	setShared(t, ctx, ti, ownIssuer, "")
	siblingIssuer := newProjectIssuer(t, ctx, ti, sibling.ID, "sibling-issuer")
	setShared(t, ctx, ti, siblingIssuer, "")

	keyed, err := ti.service.ListTokenEndpoints(asAPIKey(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{orgIssuer.ID.String(), ownIssuer.ID.String()}, issuerIDs(keyed.Items))

	session, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{orgIssuer.ID.String(), ownIssuer.ID.String(), siblingIssuer.ID.String()}, issuerIDs(session.Items))
}

func issuerIDs(items []*types.WorkloadTokenEndpoint) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserSessionIssuerID)
	}
	return ids
}
