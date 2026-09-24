package activities_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSessionQuarantineReassertRepopulatesCircuit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "session_quarantine_reassert_"+uuid.NewString()[:8])
	require.NoError(t, err)

	organizationID := "org-session-quarantine-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Session Quarantine Test",
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Session Quarantine Test",
		Slug:           "session-quarantine-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	sessionID := "reassert-session-" + uuid.NewString()
	_, err = riskrepo.New(db).CreateSessionQuarantine(ctx, riskrepo.CreateSessionQuarantineParams{
		OrganizationID: organizationID,
		ProjectID:      project.ID,
		SessionID:      sessionID,
		RiskPolicyID:   uuid.NullUUID{},
		RiskPolicyName: "Reassert test policy",
		UserID:         "user-session-quarantine-test",
		Reason:         "reassert test",
	})
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	projectID := project.ID.String()
	t.Cleanup(func() {
		_ = sessionquarantine.Delete(context.Background(), cacheAdapter, organizationID, projectID, sessionID)
	})
	require.NoError(t, sessionquarantine.Delete(ctx, cacheAdapter, organizationID, projectID, sessionID))

	before, err := sessionquarantine.Read(ctx, cacheAdapter, organizationID, projectID, sessionID)
	require.NoError(t, err)
	require.Nil(t, before)

	activity := activities.NewSessionQuarantineReassert(testenv.NewLogger(t), db, cacheAdapter)
	require.NoError(t, activity.Do(ctx))

	after, err := sessionquarantine.Read(ctx, cacheAdapter, organizationID, projectID, sessionID)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.Equal(t, "Reassert test policy", after.RiskPolicyName)
}
