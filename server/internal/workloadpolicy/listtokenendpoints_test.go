package workloadpolicy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
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

func newProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) usersessions_repo.UserSessionIssuer {
	t.Helper()

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          ti.projectID,
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
		AuthorizationServerMode: string(usersessions.AuthorizationServerModeShared),
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

	projectIssuer := newProjectIssuer(t, ctx, ti, "project-issuer")
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
	newProjectIssuer(t, ctx, ti, "endpoint-project-issuer")

	result, err := ti.service.ListTokenEndpoints(withoutProject(t, ctx), listTokenEndpointsPayload())
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

// An issuer pinned to a host this deployment does not serve falls back to
// per-endpoint authorization servers, so it has no shared token endpoint.
func TestListTokenEndpoints_LeavesOutIssuersPinnedToAnUnservedHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	issuer := newOrganizationIssuer(t, ctx, ti, "elsewhere")
	setShared(t, ctx, ti, issuer, "https://elsewhere.example.com"+usersessions.SharedAuthorizationServerPath(issuer.ID))

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
