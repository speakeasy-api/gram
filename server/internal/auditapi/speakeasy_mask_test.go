package auditapi_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// Mirrors the unexported constant in the auditapi package so tests fail if the
// hardcoded Speakeasy org id ever drifts.
const speakeasyTeamOrganizationID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"

// seedSpeakeasyMember creates the Speakeasy org (if needed) and enrolls the
// given Gram user id as an active member.
func seedSpeakeasyMember(t *testing.T, ctx context.Context, ti *testInstance, userID string) {
	t.Helper()

	queries := orgrepo.New(ti.conn)
	_, err := queries.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          speakeasyTeamOrganizationID,
		Name:        "Speakeasy Team",
		Slug:        "speakeasy-team",
		WorkosID:    conv.ToPGTextEmpty(""),
		Whitelisted: conv.PtrToPGBool(nil),
	})
	require.NoError(t, err)

	_, err = queries.UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: speakeasyTeamOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

// Admin-surface actors are masked based on the stored surface even when the
// raw OIDC subject is not a Gram user or Speakeasy organization member.
func TestAuditService_List_MasksAdminSurfaceActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	adminSurface := "admin"

	adminLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "oidc|test-operator",
		actorType:        "user",
		actorDisplayName: new("Test Operator"),
		actorSlug:        new("test-operator"),
		actingSurface:    &adminSurface,
		action:           "organization:update",
		subjectID:        authCtx.ActiveOrganizationID,
		subjectType:      "organization",
	})

	controlLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "customer-user",
		actorType:        "user",
		actorDisplayName: new("Customer User"),
		actorSlug:        new("customer"),
		action:           "project:update",
		subjectID:        "project-1",
		subjectType:      "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, result.Logs, 2)

	byID := make(map[string]*gen.AuditLog, len(result.Logs))
	for _, log := range result.Logs {
		byID[log.ID] = log
	}

	adminLog := byID[adminLogID.String()]
	require.NotNil(t, adminLog)
	require.Equal(t, "Speakeasy Team", *adminLog.ActorDisplayName)
	require.Nil(t, adminLog.ActorSlug)

	controlLog := byID[controlLogID.String()]
	require.NotNil(t, controlLog)
	require.Equal(t, "Customer User", *controlLog.ActorDisplayName)
	require.Equal(t, "customer", *controlLog.ActorSlug)
}

// Admin actor facets use the same surface-based privacy rule and do not depend
// on a Speakeasy organization membership row for the raw OIDC subject.
func TestAuditService_ListFacets_MasksAdminSurfaceActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	adminSurface := "admin"

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "oidc|test-operator",
		actorType:        "user",
		actorDisplayName: new("Test Operator"),
		actorSlug:        new("test-operator"),
		actingSurface:    &adminSurface,
		action:           "organization:update",
		subjectID:        authCtx.ActiveOrganizationID,
		subjectType:      "organization",
	})

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Actors, 1)
	require.Equal(t, "oidc|test-operator", result.Actors[0].Value)
	require.Equal(t, "Speakeasy Team", result.Actors[0].DisplayName)
}

// Audit entries whose actor is a member of the Speakeasy org are surfaced to
// customer orgs as "Speakeasy Team" instead of the staff member's email.
func TestAuditService_List_MasksSpeakeasyOrgActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	staffUserID := uuid.NewString()
	seedSpeakeasyMember(t, ctx, ti, staffUserID)

	staffLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          staffUserID,
		actorType:        "user",
		actorDisplayName: new("david@speakeasy.com"),
		actorSlug:        new("david"),
		action:           "chat_session:access",
		subjectID:        uuid.NewString(),
		subjectType:      "chat_session",
	})

	customerLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "customer-user",
		actorType:        "user",
		actorDisplayName: new("customer@example.com"),
		actorSlug:        new("customer"),
		action:           "project:update",
		subjectID:        "project-1",
		subjectType:      "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
		SubjectType:  nil,
		SubjectID:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 2)

	byID := make(map[string]*gen.AuditLog, len(result.Logs))
	for _, log := range result.Logs {
		byID[log.ID] = log
	}

	staffLog := byID[staffLogID.String()]
	require.NotNil(t, staffLog)
	require.NotNil(t, staffLog.ActorDisplayName)
	require.Equal(t, "Speakeasy Team", *staffLog.ActorDisplayName, "speakeasy staff email must be masked")
	require.Nil(t, staffLog.ActorSlug, "speakeasy staff slug must be masked")

	customerLog := byID[customerLogID.String()]
	require.NotNil(t, customerLog)
	require.NotNil(t, customerLog.ActorDisplayName)
	require.Equal(t, "customer@example.com", *customerLog.ActorDisplayName, "non-speakeasy actors are untouched")
	require.NotNil(t, customerLog.ActorSlug)
	require.Equal(t, "customer", *customerLog.ActorSlug)
}

// Non-user actor types are never masked, even if their id happens to collide
// with a Speakeasy member's user id.
func TestAuditService_List_MaskSkipsNonUserActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	staffUserID := uuid.NewString()
	seedSpeakeasyMember(t, ctx, ti, staffUserID)

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          staffUserID,
		actorType:        "service_account",
		actorDisplayName: new("automation"),
		action:           "project:update",
		subjectID:        "project-1",
		subjectType:      "project",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		Cursor:       nil,
		ProjectSlug:  nil,
		ActorID:      nil,
		Action:       nil,
		SubjectType:  nil,
		SubjectID:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.NotNil(t, result.Logs[0].ActorDisplayName)
	require.Equal(t, "automation", *result.Logs[0].ActorDisplayName)
}

// Inside the Speakeasy org itself, ordinary staff actors keep their real
// identities, while admin-surface actors remain masked in the customer API.
func TestAuditService_List_DoesNotMaskSpeakeasyOrgViewingItself(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	staffUserID := uuid.NewString()
	seedSpeakeasyMember(t, ctx, ti, staffUserID)

	// Re-scope the session to the Speakeasy org as the active org.
	authCtx.ActiveOrganizationID = speakeasyTeamOrganizationID

	staffLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   speakeasyTeamOrganizationID,
		projectID:        uuid.NullUUID{},
		actorID:          staffUserID,
		actorType:        "user",
		actorDisplayName: new("david@speakeasy.com"),
		actorSlug:        new("david"),
		action:           "organization:update",
		subjectID:        speakeasyTeamOrganizationID,
		subjectType:      "organization",
	})

	adminSurface := "admin"
	adminLogID := insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   speakeasyTeamOrganizationID,
		projectID:        uuid.NullUUID{},
		actorID:          "oidc|test-operator",
		actorType:        "user",
		actorDisplayName: new("Test Operator"),
		actorSlug:        new("test-operator"),
		actingSurface:    &adminSurface,
		action:           "organization:update",
		subjectID:        speakeasyTeamOrganizationID,
		subjectType:      "organization",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, result.Logs, 2)

	byID := make(map[string]*gen.AuditLog, len(result.Logs))
	for _, log := range result.Logs {
		byID[log.ID] = log
	}

	staffLog := byID[staffLogID.String()]
	require.NotNil(t, staffLog)
	require.Equal(t, "david@speakeasy.com", *staffLog.ActorDisplayName)

	adminLog := byID[adminLogID.String()]
	require.NotNil(t, adminLog)
	require.Equal(t, "Speakeasy Team", *adminLog.ActorDisplayName)
	require.Nil(t, adminLog.ActorSlug)
}

// The actor facet list masks Speakeasy staff display names the same way the
// log feed does, so the audit page filter dropdown doesn't leak emails.
func TestAuditService_ListFacets_MasksSpeakeasyOrgActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	staffUserID := uuid.NewString()
	seedSpeakeasyMember(t, ctx, ti, staffUserID)

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          staffUserID,
		actorType:        "user",
		actorDisplayName: new("david@speakeasy.com"),
		action:           "chat_session:access",
		subjectID:        uuid.NewString(),
		subjectType:      "chat_session",
	})

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          "customer-user",
		actorType:        "user",
		actorDisplayName: new("customer@example.com"),
		action:           "project:update",
		subjectID:        "project-1",
		subjectType:      "project",
	})

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		ProjectSlug:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Actors, 2)

	byValue := make(map[string]string, len(result.Actors))
	for _, actor := range result.Actors {
		byValue[actor.Value] = actor.DisplayName
	}
	require.Equal(t, "Speakeasy Team", byValue[staffUserID])
	require.Equal(t, "customer@example.com", byValue["customer-user"])
}

// Facets for non-user actors are never masked, even if their id happens to
// collide with a Speakeasy member's user id.
func TestAuditService_ListFacets_MaskSkipsNonUserActors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)

	staffUserID := uuid.NewString()
	seedSpeakeasyMember(t, ctx, ti, staffUserID)

	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID:   authCtx.ActiveOrganizationID,
		projectID:        uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		actorID:          staffUserID,
		actorType:        "service_account",
		actorDisplayName: new("automation"),
		action:           "project:update",
		subjectID:        "project-1",
		subjectType:      "project",
	})

	result, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		ProjectSlug:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Actors, 1)
	require.Equal(t, staffUserID, result.Actors[0].Value)
	require.Equal(t, "automation", result.Actors[0].DisplayName)
}
