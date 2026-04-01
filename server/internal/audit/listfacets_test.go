package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
)

func TestAuditService_ListFacets_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	otherProject := createProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:alice",
		actorType:        "user",
		actorDisplayName: new("Alice 1"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:alice",
		actorType:        "user",
		actorDisplayName: new("Alice Latest"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "user:alice",
		actorType:      "user",
		action:         "api_key:create",
		subjectID:      uuid.NewString(),
		subjectType:    "api_key",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:bob",
		actorType:        "user",
		actorDisplayName: new("Bob"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:bob",
		actorType:        "user",
		actorDisplayName: new("Bob"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:        "service:no-name",
		actorType:      "service_account",
		action:         "deployment:tag",
		subjectID:      uuid.NewString(),
		subjectType:    "deployment",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		actorID:          "user:alice",
		actorType:        "user",
		actorDisplayName: new("Alice Other Project"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		actorID:        "user:other-project-only",
		actorType:      "user",
		action:         "project:delete",
		subjectID:      uuid.NewString(),
		subjectType:    "project",
	})

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		ProjectSlug:  authCtx.ProjectSlug,
	})
	require.NoError(t, err)
	require.Len(t, result.Actors, 3)
	require.Len(t, result.Actions, 3)

	require.Equal(t, "user:alice", result.Actors[0].Value)
	require.Equal(t, "Alice Latest", result.Actors[0].DisplayName)
	require.EqualValues(t, 3, result.Actors[0].Count)
	require.Equal(t, "user:bob", result.Actors[1].Value)
	require.Equal(t, "Bob", result.Actors[1].DisplayName)
	require.EqualValues(t, 2, result.Actors[1].Count)
	require.Equal(t, "service:no-name", result.Actors[2].Value)
	require.Equal(t, "service:no-name", result.Actors[2].DisplayName)
	require.EqualValues(t, 1, result.Actors[2].Count)

	require.Equal(t, "project:update", result.Actions[0].Value)
	require.Equal(t, "project:update", result.Actions[0].DisplayName)
	require.EqualValues(t, 4, result.Actions[0].Count)
	require.Equal(t, "api_key:create", result.Actions[1].Value)
	require.EqualValues(t, 1, result.Actions[1].Count)
	require.Equal(t, "deployment:tag", result.Actions[2].Value)
	require.EqualValues(t, 1, result.Actions[2].Count)
}

func TestAuditService_ListFacets_OrganizationScopeIncludesAllProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	otherProject := createProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "user:shared",
		actorType:        "user",
		actorDisplayName: new("Shared User"),
		action:           "project:update",
		subjectID:        uuid.NewString(),
		subjectType:      "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		actorID:        "user:shared",
		actorType:      "user",
		action:         "project:update",
		subjectID:      uuid.NewString(),
		subjectType:    "project",
	})
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NullUUID{UUID: otherProject.ID, Valid: true},
		actorID:        "user:other-project",
		actorType:      "user",
		action:         "project:delete",
		subjectID:      uuid.NewString(),
		subjectType:    "project",
	})

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		ProjectSlug:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Actors, 2)
	require.Equal(t, "user:shared", result.Actors[0].Value)
	require.EqualValues(t, 2, result.Actors[0].Count)
	require.Equal(t, "user:other-project", result.Actors[1].Value)
	require.EqualValues(t, 1, result.Actors[1].Count)
	require.Len(t, result.Actions, 2)
	require.Equal(t, "project:update", result.Actions[0].Value)
	require.EqualValues(t, 2, result.Actions[0].Count)
	require.Equal(t, "project:delete", result.Actions[1].Value)
	require.EqualValues(t, 1, result.Actions[1].Count)
}
