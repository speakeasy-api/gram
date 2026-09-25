package agentmanagement

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func activitySessionFixture(t *testing.T, conn *pgxpool.Pool, org string, agentID uuid.UUID, used time.Time) repo.SeedAgentCredentialSessionActivityFixtureParams {
	t.Helper()
	issuer, err := testrepo.New(conn).InsertOrganizationTierUserSessionIssuerFixture(t.Context(), testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID: conv.ToPGText(org), Slug: "activity-" + uuid.NewString(), AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return repo.SeedAgentCredentialSessionActivityFixtureParams{
		ID: uuid.New(), OrganizationID: conv.ToPGText(org), ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IssuerID: issuer, Subject: urn.NewAgentSubject(agentID), Jti: uuid.NewString(),
		ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), LastUsedAt: conv.ToPGTimestamptz(used), DeletedAt: conv.PtrToPGTimestamptz(nil),
	}
}

func activityKeyFixture(org, owner string, agentID uuid.UUID, used time.Time) repo.SeedAgentCredentialKeyActivityFixtureParams {
	return repo.SeedAgentCredentialKeyActivityFixtureParams{
		ID: uuid.New(), OrganizationID: org, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, OwnerUserID: owner,
		Name: "activity-" + uuid.NewString(), KeyHash: "fixture-" + uuid.NewString(),
		Subject:    conv.ToPGText(urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String()),
		LastUsedAt: conv.ToPGTimestamptz(used), DeletedAt: conv.PtrToPGTimestamptz(nil), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
	}
}

func grantActivityScope(t *testing.T, conn *pgxpool.Pool, org, user string, agentID uuid.UUID, scope authz.Scope) {
	t.Helper()
	selectors, err := authz.NewSelector(scope, agentID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: org, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user), Scope: string(scope), Selectors: selectors,
	})
	require.NoError(t, err)
}

func TestListAgentCredentialActivityCombinesSourcesAndRetainsRevokedHistory(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	agent := createAgent(t, conn, "org-a", "owner", "Observed")
	keyOnly := createAgent(t, conn, "org-a", "owner", "Key only")
	unknown := createAgent(t, conn, "org-a", "owner", "Unknown")
	queries := repo.New(conn)
	used := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	session := activitySessionFixture(t, conn, "org-a", agent.ID, used)
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), session))
	key := activityKeyFixture("org-a", "owner", agent.ID, used.Add(time.Minute))
	key.DeletedAt = conv.ToPGTimestamptz(used.Add(2 * time.Minute))
	key.ExpiresAt = key.DeletedAt
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), key))
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture("org-a", "owner", keyOnly.ID, used)))
	nullSession := activitySessionFixture(t, conn, "org-a", unknown.ID, used)
	nullSession.LastUsedAt = conv.PtrToPGTimestamptz(nil)
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), nullSession))
	nullKey := activityKeyFixture("org-a", "owner", unknown.ID, used)
	nullKey.LastUsedAt = conv.PtrToPGTimestamptz(nil)
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), nullKey))
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 3)
	byID := make(map[string]*gen.ManagedAgent, len(listed))
	for _, view := range listed {
		byID[view.ID] = view
	}
	require.Equal(t, new(used.Add(time.Minute).Format(time.RFC3339Nano)), byID[agent.ID.String()].LastCredentialUsedAt)
	require.Equal(t, new(used.Format(time.RFC3339Nano)), byID[keyOnly.ID.String()].LastCredentialUsedAt)
	require.Nil(t, byID[unknown.ID.String()].LastCredentialUsedAt)
	// A later revoked, expired session wins over the key without hiding its history.
	session.ID, session.Jti = uuid.New(), uuid.NewString()
	session.LastUsedAt = conv.ToPGTimestamptz(used.Add(3 * time.Minute))
	session.DeletedAt = conv.ToPGTimestamptz(used.Add(4 * time.Minute))
	session.ExpiresAt = session.DeletedAt
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), session))
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	for _, view := range listed {
		if view.ID == agent.ID.String() {
			require.Equal(t, new(used.Add(3*time.Minute).Format(time.RFC3339Nano)), view.LastCredentialUsedAt)
		}
	}
}

func TestListAgentCredentialActivityRequiresCredentialPermission(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "reader")
	agent := createAgent(t, conn, "org-a", "owner", "Observed")
	used := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.New(conn).SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture("org-a", "owner", agent.ID, used)))
	grantActivityScope(t, conn, "org-a", "reader", agent.ID, authz.ScopeAgentRead)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "reader")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Permissions.Read)
	require.False(t, listed[0].Permissions.Authorize)
	require.Nil(t, listed[0].LastCredentialUsedAt)
	grantActivityScope(t, conn, "org-a", "reader", agent.ID, authz.ScopeAgentAuthorize)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Permissions.Authorize)
	require.Equal(t, new(used.Format(time.RFC3339Nano)), listed[0].LastCredentialUsedAt)
}

func TestListAgentCredentialActivityRejectsDisagreeingTenancy(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	for _, org := range []string{"org-a", "org-b"} {
		seedOrganization(t, conn, org)
		seedOrganizationUser(t, conn, org, "owner")
	}
	agent := createAgent(t, conn, "org-a", "owner", "Observed")
	foreignProject, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Foreign", Slug: "foreign", OrganizationID: "org-b",
	})
	require.NoError(t, err)
	queries := repo.New(conn)
	used := time.Now().UTC().Truncate(time.Second)
	// Each invalid row carries the exact agent subject but disagrees on tenancy.
	foreignIssuer := activitySessionFixture(t, conn, "org-b", agent.ID, used)
	foreignIssuer.OrganizationID = conv.ToPGText("org-a")
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), foreignIssuer))
	foreignSession := activitySessionFixture(t, conn, "org-a", agent.ID, used)
	foreignSession.OrganizationID = conv.ToPGText("org-b")
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), foreignSession))
	foreignSessionProject := activitySessionFixture(t, conn, "org-a", agent.ID, used)
	foreignSessionProject.ProjectID = uuid.NullUUID{UUID: foreignProject.ID, Valid: true}
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), foreignSessionProject))
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture("org-b", "owner", agent.ID, used)))
	foreignKeyProject := activityKeyFixture("org-a", "owner", agent.ID, used)
	foreignKeyProject.ProjectID = uuid.NullUUID{UUID: foreignProject.ID, Valid: true}
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), foreignKeyProject))
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Nil(t, listed[0].LastCredentialUsedAt)
	// Legacy org-null sessions still count when every known tenant agrees.
	legacy := activitySessionFixture(t, conn, "org-a", agent.ID, used.Add(-time.Hour))
	legacy.OrganizationID = conv.PtrToPGText(nil)
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), legacy))
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Equal(t, new(used.Add(-time.Hour).Format(time.RFC3339Nano)), listed[0].LastCredentialUsedAt)
}

func TestListAgentCredentialActivityIsolatesSiblingAndOwnerSubjects(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	agent := createAgent(t, conn, "org-a", "owner", "Observed")
	sibling := createAgent(t, conn, "org-a", "owner", "Sibling")
	used := time.Now().UTC().Truncate(time.Second)
	queries := repo.New(conn)
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture("org-a", "owner", agent.ID, used)))
	ownerSession := activitySessionFixture(t, conn, "org-a", sibling.ID, used.Add(time.Minute))
	ownerSession.Subject = urn.NewUserSubject("owner")
	require.NoError(t, queries.SeedAgentCredentialSessionActivityFixture(t.Context(), ownerSession))
	ownerKey := activityKeyFixture("org-a", "owner", sibling.ID, used.Add(time.Minute))
	ownerKey.Subject = conv.ToPGText(urn.NewPrincipal(urn.PrincipalTypeUser, "owner").String())
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), ownerKey))
	ownerKey.ID, ownerKey.Name, ownerKey.KeyHash = uuid.New(), "legacy", uuid.NewString()
	ownerKey.Subject = conv.PtrToPGText(nil)
	require.NoError(t, queries.SeedAgentCredentialKeyActivityFixture(t.Context(), ownerKey))
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	listed, err := service.List(validatedHumanContext(t, "org-a", "owner"), &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	for _, view := range listed {
		if view.ID == agent.ID.String() {
			require.Equal(t, new(used.Format(time.RFC3339Nano)), view.LastCredentialUsedAt)
		} else {
			require.Nil(t, view.LastCredentialUsedAt)
		}
	}
}

func TestListAgentCredentialActivityRespectsOwnerReassignmentLatch(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	agent := createAgent(t, conn, "org-a", "owner", "Observed")
	used := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.New(conn).SeedAgentCredentialKeyActivityFixture(t.Context(), activityKeyFixture("org-a", "owner", agent.ID, used)))
	require.NoError(t, testrepo.New(conn).SetAgentOwnerLatchFixture(t.Context(), agent.ID))
	grantActivityScope(t, conn, "org-a", "owner", agent.ID, authz.ScopeAgentRead)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.False(t, listed[0].Permissions.Authorize)
	require.Nil(t, listed[0].LastCredentialUsedAt)
	grantActivityScope(t, conn, "org-a", "owner", agent.ID, authz.ScopeAgentAuthorize)
	listed, err = service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.True(t, listed[0].Permissions.Authorize)
	require.Equal(t, new(used.Format(time.RFC3339Nano)), listed[0].LastCredentialUsedAt)
}

func TestListAgentCredentialActivityUsesOneBatchQuery(t *testing.T) {
	t.Parallel()
	conn, counter := newActivityTracedTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	seedOrganizationUser(t, conn, "org-a", "reader")
	for _, name := range []string{"First", "Second", "Third"} {
		agent := createAgent(t, conn, "org-a", "owner", name)
		grantActivityScope(t, conn, "org-a", "reader", agent.ID, authz.ScopeAgentRead)
	}
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	listed, err := service.List(validatedHumanContext(t, "org-a", "owner"), &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 3)
	require.Equal(t, int64(1), counter.Load())
	listed, err = service.List(validatedHumanContext(t, "org-a", "reader"), &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 3)
	require.Equal(t, int64(1), counter.Load(), "read-only inventory must skip the activity query")
}
