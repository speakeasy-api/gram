package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// setIssuerShared switches an issuer to shared mode, pinned to
// pinnedIssuerURL unless it is empty.
func setIssuerShared(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, pinnedIssuerURL string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(ctx, testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: string(authserver.ModeShared),
		PinnedIssuerUrl:         pgtype.Text{String: pinnedIssuerURL, Valid: pinnedIssuerURL != ""},
		IssuerID:                issuerID,
		OrganizationID:          authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
}

func createOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) uuid.UUID {
	t.Helper()

	issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{Slug: slug, AuthnChallengeMode: "interactive", SessionDurationHours: 24})
	require.NoError(t, err)
	return uuid.MustParse(issuer.ID)
}

func TestUserSessionIssuerReportsEndpointAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "endpoint-issuer")

	issuer, err := ti.service.GetUserSessionIssuer(ctx, &projectgen.GetUserSessionIssuerPayload{ID: new(issuerID.String())})
	require.NoError(t, err)
	require.Equal(t, string(authserver.ModeEndpoint), issuer.AuthorizationServerMode)
	require.False(t, issuer.UseAuthenticationHost)
	require.False(t, issuer.IssuerURLPinned)
	require.Nil(t, issuer.IssuerURL)
	require.Nil(t, issuer.TokenEndpoint)
	require.Nil(t, issuer.AuthorizationServerUnavailableReason)
}

func TestUserSessionIssuerReportsSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "shared-issuer")
	setIssuerShared(t, ctx, ti, issuerID, "")
	issuerURL := testServerURL + authserver.SharedPath(issuerID)

	issuer, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: issuerID.String()})
	require.NoError(t, err)
	require.Equal(t, string(authserver.ModeShared), issuer.AuthorizationServerMode)
	require.False(t, issuer.IssuerURLPinned)
	require.Equal(t, issuerURL, *issuer.IssuerURL)
	require.Equal(t, issuerURL+"/token", *issuer.TokenEndpoint)
	require.Nil(t, issuer.AuthorizationServerUnavailableReason)

	// Projects inherit the organization's issuer, and see the same values.
	listed, err := ti.service.ListUserSessionIssuers(ctx, &projectgen.ListUserSessionIssuersPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, issuer, listed.Items[0])
}

func TestUserSessionIssuerReportsPinnedSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "pinned-issuer")
	issuerURL := testServerURL + authserver.SharedPath(issuerID)
	setIssuerShared(t, ctx, ti, issuerID, issuerURL)

	issuer, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: issuerID.String()})
	require.NoError(t, err)
	require.True(t, issuer.IssuerURLPinned)
	require.Equal(t, issuerURL, *issuer.IssuerURL)
	require.Equal(t, issuerURL+"/token", *issuer.TokenEndpoint)
}

func TestUserSessionIssuerReportsUnavailableSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		pin    func(issuerID uuid.UUID) string
		reason string
	}{
		{name: "unserved host", reason: "host_not_served", pin: func(issuerID uuid.UUID) string {
			return "https://other.example.com" + authserver.SharedPath(issuerID)
		}},
		{name: "another issuer's path", reason: "pinned_issuer_url_invalid", pin: func(uuid.UUID) string {
			return testServerURL + authserver.SharedPath(uuid.New())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			issuerID := createOrganizationIssuer(t, ctx, ti, "unavailable-issuer")
			setIssuerShared(t, ctx, ti, issuerID, tc.pin(issuerID))

			issuer, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: issuerID.String()})
			require.NoError(t, err)
			require.Equal(t, string(authserver.ModeShared), issuer.AuthorizationServerMode)
			require.Nil(t, issuer.IssuerURL)
			require.Nil(t, issuer.TokenEndpoint)
			require.Equal(t, tc.reason, *issuer.AuthorizationServerUnavailableReason)
		})
	}
}

// Audit snapshots record what the issuer stores, not values derived from the
// deployment's hosts, so a host change never reads as an issuer change.
func TestUserSessionIssuerAuditSnapshotLeavesOutSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "audited-issuer")
	setIssuerShared(t, ctx, ti, issuerID, testServerURL+authserver.SharedPath(issuerID))

	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{ID: issuerID.String(), Slug: new("audited-issuer-renamed")})
	require.NoError(t, err)
	require.NotNil(t, updated.IssuerURL)

	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(updateAudit.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, string(authserver.ModeShared), snapshot["AuthorizationServerMode"])
	require.Equal(t, true, snapshot["IssuerURLPinned"])
	require.Nil(t, snapshot["IssuerURL"])
	require.Nil(t, snapshot["TokenEndpoint"])
}

func TestOrganizationUserSessionIssuersListByTier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationIssuerID := createOrganizationIssuer(t, ctx, ti, "tier-organization")
	projectIssuerID := seedIssuer(t, ctx, ti, "tier-project")
	sibling := seedSiblingProject(t, ctx, ti, "tier-sibling")

	ids := func(tier *string) []string {
		listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Tier: tier})
		require.NoError(t, err)
		ids := make([]string, 0, len(listed.Items))
		for _, item := range listed.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}

	require.ElementsMatch(t, []string{organizationIssuerID.String()}, ids(new("organization")))
	require.ElementsMatch(t, []string{projectIssuerID.String(), sibling.issuerID.String()}, ids(new("project")))
	require.ElementsMatch(t, []string{organizationIssuerID.String(), projectIssuerID.String(), sibling.issuerID.String()}, ids(nil))

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{})
	require.NoError(t, err)
	for _, item := range listed.Items {
		switch item.ID {
		case organizationIssuerID.String():
			require.Nil(t, item.ProjectName)
		case sibling.issuerID.String():
			require.Equal(t, "tier-sibling", *item.ProjectName)
		default:
			require.NotEmpty(t, *item.ProjectName)
		}
	}
}

func TestOrganizationUserSessionIssuerGetsProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sibling := seedSiblingProject(t, ctx, ti, "get-sibling")

	issuer, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: sibling.issuerID.String()})
	require.NoError(t, err)
	require.Equal(t, sibling.projectID.String(), issuer.ProjectID)
	require.Equal(t, "get-sibling", *issuer.ProjectName)
}

func TestOrganizationUserSessionIssuerMigratePreflightWarnsOfAuthorizationServerChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sourceID := createOrganizationIssuer(t, ctx, ti, "shared-source")
	targetID := createOrganizationIssuer(t, ctx, ti, "endpoint-target")
	sourceIssuerURL := testServerURL + authserver.SharedPath(sourceID)
	setIssuerShared(t, ctx, ti, sourceID, sourceIssuerURL)

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.ElementsMatch(t, []*orggen.UserSessionIssuerFieldMismatch{
		{Field: "authorization_server_mode", SourceValue: string(authserver.ModeShared), TargetValue: string(authserver.ModeEndpoint)},
		{Field: "issuer_url", SourceValue: sourceIssuerURL, TargetValue: ""},
	}, preflight.Warnings)
	require.NotEmpty(t, preflight.WarningsFingerprint)
}

// Two shared issuers never share an issuer URL, so consolidating one onto
// another always changes what clients are pointed at.
func TestOrganizationUserSessionIssuerMigratePreflightWarnsOfSharedIssuerURLChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sourceID := createOrganizationIssuer(t, ctx, ti, "shared-source")
	targetID := createOrganizationIssuer(t, ctx, ti, "shared-target")
	setIssuerShared(t, ctx, ti, sourceID, "")
	setIssuerShared(t, ctx, ti, targetID, "")

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
	require.NoError(t, err)
	require.Equal(t, []*orggen.UserSessionIssuerFieldMismatch{
		{Field: "issuer_url", SourceValue: testServerURL + authserver.SharedPath(sourceID), TargetValue: testServerURL + authserver.SharedPath(targetID)},
	}, preflight.Warnings)
}

// Promoting an issuer keeps its row, so its shared authorization server keeps
// its issuer URL.
func TestOrganizationUserSessionIssuerMoveKeepsSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "promoted-issuer")
	issuerURL := testServerURL + authserver.SharedPath(issuerID)
	setIssuerShared(t, ctx, ti, issuerID, issuerURL)

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: issuerID.String()})
	require.NoError(t, err)
	require.Empty(t, moved.ProjectID)
	require.Equal(t, string(authserver.ModeShared), moved.AuthorizationServerMode)
	require.True(t, moved.IssuerURLPinned)
	require.Equal(t, issuerURL, *moved.IssuerURL)
}

// seedOtherOrganizationIssuer creates a project-specific issuer in another
// organization.
func seedOtherOrganizationIssuer(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	organizationID := "org-" + uuid.NewString()
	_, err := organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Other Organization",
		Slug:        "other-organization-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{String: organizationID, Valid: true},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "other-project",
		Slug:           "other-project",
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	issuer, err := repo.New(ti.conn).CreateUserSessionIssuer(ctx, repo.CreateUserSessionIssuerParams{
		ProjectID:          project.ID,
		OrganizationID:     pgtype.Text{String: organizationID, Valid: true},
		Slug:               "other-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func TestOrganizationUserSessionIssuersHideOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	foreignID := seedOtherOrganizationIssuer(t, ctx, ti)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Tier: new("project")})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, foreignID.String(), item.ID)
	}

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: foreignID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ListMcpServers(ctx, &orggen.ListMcpServersPayload{UserSessionIssuerID: foreignID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuersHideDeletedProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sibling := seedSiblingProject(t, ctx, ti, "deleted-sibling")
	_, err := projectsrepo.New(ti.conn).DeleteProject(ctx, sibling.projectID)
	require.NoError(t, err)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Tier: new("project")})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, sibling.issuerID.String(), item.ID)
	}

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: sibling.issuerID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// An organization-level response names the project a moved issuer now
// belongs to, as get and list do.
func TestOrganizationUserSessionIssuerMoveNamesTargetProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "narrowed-issuer")
	projectID := createSiblingProject(t, ctx, ti.conn, "narrowed-project")

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: issuerID.String(), ProjectID: new(projectID.String())})
	require.NoError(t, err)
	require.Equal(t, projectID.String(), moved.ProjectID)
	require.Equal(t, "narrowed-project", *moved.ProjectName)
}
