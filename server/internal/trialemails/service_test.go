package trialemails

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Printf("clean up test infrastructure: %v", err)
	}

	os.Exit(code)
}

func TestTrialStartedFansOutOnlyToActiveAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	firstAdmin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Admin One")
	secondAdmin := ti.seedAdmin(t, ctx, "<SECOND_ADMIN_USER_ID>", "<SECOND_ADMIN_EMAIL>@example.test", "Admin Two")
	ti.seedMember(t, ctx, "<MEMBER_USER_ID>", "<MEMBER_EMAIL>@example.test", "Member")

	err := ti.service.TrialStarted(ctx, ti.organizationID)
	require.NoError(t, err)

	require.Equal(t, []loops.FindContactInput{
		{UserID: firstAdmin.id},
		{Email: firstAdmin.email},
		{UserID: secondAdmin.id},
		{Email: secondAdmin.email},
	}, ti.client.finds())
	require.Equal(t, []loops.UpdateContactInput{
		{Email: firstAdmin.email, FirstName: new("Admin"), UserID: firstAdmin.id, CustomProperties: activeProperties(ti)},
		{Email: secondAdmin.email, FirstName: new("Admin"), UserID: secondAdmin.id, CustomProperties: activeProperties(ti)},
	}, ti.client.updates())
	require.Equal(t, []loops.SendEventInput{
		trialStartedEvent(ti, firstAdmin),
		trialStartedEvent(ti, secondAdmin),
	}, ti.client.events())
}

func TestTrialStartedUsesLifecyclePropertiesAndStableIdempotency(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	admin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Taylor Admin")

	require.NoError(t, ti.service.TrialStarted(ctx, ti.organizationID))
	require.NoError(t, ti.service.TrialStarted(ctx, ti.organizationID))

	updates := ti.client.updates()
	require.Len(t, updates, 2)
	require.Equal(t, activeProperties(ti), updates[0].CustomProperties)
	require.Equal(t, new("Taylor"), updates[0].FirstName)

	events := ti.client.events()
	require.Len(t, events, 2)
	require.Equal(t, activeProperties(ti), events[0].EventProperties)
	require.Equal(t, events[0].IdempotencyKey, events[1].IdempotencyKey)
	require.Equal(t, trialStartedEvent(ti, admin).IdempotencyKey, events[0].IdempotencyKey)
}

func TestTrialStartedSkipsInactiveTrial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithTrial(t, false)
	ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Admin")

	require.NoError(t, ti.service.TrialStarted(ctx, ti.organizationID))
	require.Empty(t, ti.client.finds())
	require.Empty(t, ti.client.updates())
	require.Empty(t, ti.client.events())
}

func TestAdminAddedStartsSequenceForNewActiveAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	admin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "New Admin")
	ti.seedMember(t, ctx, "<MEMBER_USER_ID>", "<MEMBER_EMAIL>@example.test", "Member")

	require.NoError(t, ti.service.AdminAdded(ctx, ti.organizationID, admin.id))
	require.Equal(t, []loops.SendEventInput{trialStartedEvent(ti, admin)}, ti.client.events())

	err := ti.service.AdminAdded(ctx, ti.organizationID, "<MEMBER_USER_ID>")
	require.ErrorContains(t, err, "not found in active organization administrators")
	require.Len(t, ti.client.updates(), 1)
	require.Len(t, ti.client.events(), 1)
}

func TestTrialStartedDoesNotSendToUnsubscribedContact(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	admin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Admin")
	ti.client.contactsByEmail[admin.email] = &loops.Contact{Email: admin.email, Subscribed: false}

	require.NoError(t, ti.service.TrialStarted(ctx, ti.organizationID))
	require.Equal(t, []loops.FindContactInput{
		{UserID: admin.id},
		{Email: admin.email},
	}, ti.client.finds())
	require.Equal(t, []loops.UpdateContactInput{{
		Email:            admin.email,
		FirstName:        new("Admin"),
		UserID:           admin.id,
		CustomProperties: activeProperties(ti),
	}}, ti.client.updates())
	require.Empty(t, ti.client.events())
}

func TestTrialStartedDoesNotSendToUnsubscribedContactFoundByUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	admin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Admin")
	ti.client.contactsByUserID[admin.id] = &loops.Contact{Email: "<OLD_ADMIN_EMAIL>@example.test", UserID: &admin.id, Subscribed: false}

	require.NoError(t, ti.service.TrialStarted(ctx, ti.organizationID))
	require.Equal(t, []loops.FindContactInput{{UserID: admin.id}}, ti.client.finds())
	require.Equal(t, []loops.UpdateContactInput{{
		Email:            admin.email,
		FirstName:        new("Admin"),
		UserID:           admin.id,
		CustomProperties: activeProperties(ti),
	}}, ti.client.updates())
	require.Empty(t, ti.client.events())
}

func TestTrialStartedContinuesAfterContactFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	failedAdmin := ti.seedAdmin(t, ctx, "<FAILED_ADMIN_USER_ID>", "<FAILED_ADMIN_EMAIL>@example.test", "Failed Admin")
	remainingAdmin := ti.seedAdmin(t, ctx, "<REMAINING_ADMIN_USER_ID>", "<REMAINING_ADMIN_EMAIL>@example.test", "Remaining Admin")
	ti.client.updateErrors[failedAdmin.id] = errors.New("update failed")

	err := ti.service.TrialStarted(ctx, ti.organizationID)
	require.Error(t, err)
	require.Equal(t, []loops.SendEventInput{trialStartedEvent(ti, remainingAdmin)}, ti.client.events())
}

func TestTrialInactiveUpdatesOnlyActiveAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	firstAdmin := ti.seedAdmin(t, ctx, "<ADMIN_USER_ID>", "<ADMIN_EMAIL>@example.test", "Admin One")
	secondAdmin := ti.seedAdmin(t, ctx, "<SECOND_ADMIN_USER_ID>", "<SECOND_ADMIN_EMAIL>@example.test", "Admin Two")
	ti.seedMember(t, ctx, "<MEMBER_USER_ID>", "<MEMBER_EMAIL>@example.test", "Member")

	require.NoError(t, ti.service.TrialInactive(ctx, ti.organizationID))
	require.Equal(t, []loops.FindContactInput{
		{UserID: firstAdmin.id},
		{Email: firstAdmin.email},
		{UserID: secondAdmin.id},
		{Email: secondAdmin.email},
	}, ti.client.finds())
	require.Equal(t, []loops.UpdateContactInput{
		{Email: firstAdmin.email, FirstName: new("Admin"), UserID: firstAdmin.id, CustomProperties: map[string]any{"trialActive": false}},
		{Email: secondAdmin.email, FirstName: new("Admin"), UserID: secondAdmin.id, CustomProperties: map[string]any{"trialActive": false}},
	}, ti.client.updates())
	require.Empty(t, ti.client.events())
}

type testInstance struct {
	service        *Service
	client         *fakeWorkflowClient
	conn           *pgxpool.Pool
	organizationID string
	trialCreatedAt time.Time
	trialEndsAt    time.Time
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithTrial(t, true)
}

func newTestServiceWithTrial(t *testing.T, active bool) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "trialemailstest")
	require.NoError(t, err)

	createdAt := time.Date(2099, time.August, 1, 12, 0, 0, 0, time.UTC)
	endsAt := time.Date(2099, time.August, 15, 12, 0, 0, 0, time.UTC)
	if !active {
		endsAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
		createdAt = endsAt.Add(-14 * 24 * time.Hour)
	}
	const organizationID = "<ORGANIZATION_ID>"
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "<ORGANIZATION_NAME>",
		Slug:               "<ORGANIZATION_SLUG>",
		GramAccountType:    "enterprise",
		WorkosID:           conv.PtrToPGText(nil),
		Whitelisted:        true,
		FreeTrialStartedAt: conv.ToPGTimestamptz(createdAt),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(endsAt),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	}))
	require.NoError(t, trialsrepo.New(conn).InsertTrialFixture(ctx, trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID,
		Tier:           "enterprise",
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		EndsAt:         conv.ToPGTimestamptz(endsAt),
		ConvertedAt:    conv.PtrToPGTimestamptz(nil),
		DemotedAt:      conv.PtrToPGTimestamptz(nil),
	}))

	client := newFakeWorkflowClient()
	return ctx, &testInstance{
		service:        NewService(conn, client, testenv.NewLogger(t), "https://app.example.test/"),
		client:         client,
		conn:           conn,
		organizationID: organizationID,
		trialCreatedAt: createdAt,
		trialEndsAt:    endsAt,
	}
}

type testAdmin struct {
	id    string
	email string
}

func (ti *testInstance) seedAdmin(t *testing.T, ctx context.Context, userID, email, displayName string) testAdmin {
	t.Helper()
	ti.seedUser(t, ctx, userID, email, displayName, "admin")
	return testAdmin{id: userID, email: email}
}

func (ti *testInstance) seedMember(t *testing.T, ctx context.Context, userID, email, displayName string) {
	t.Helper()
	ti.seedUser(t, ctx, userID, email, displayName, "member")
}

func (ti *testInstance) seedUser(t *testing.T, ctx context.Context, userID, email, displayName, roleSlug string) {
	t.Helper()

	queries := repo.New(ti.conn)
	now := time.Date(2099, time.July, 31, 12, 0, 0, 0, time.UTC)
	_, err := queries.UpsertOrganizationRole(ctx, repo.UpsertOrganizationRoleParams{
		OrganizationID:    ti.organizationID,
		WorkosSlug:        roleSlug,
		WorkosName:        roleSlug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
	err = userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       userID,
		WorkosID: conv.ToPGText("workos-" + userID),
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: ti.organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	rows, err := queries.UpsertOrganizationRoleAssignment(ctx, repo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     ti.organizationID,
		WorkosUserID:       "workos-" + userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGTextEmpty(""),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func activeProperties(ti *testInstance) map[string]any {
	return map[string]any{
		"organizationName": "<ORGANIZATION_NAME>",
		"dashboardUrl":     "https://app.example.test/<ORGANIZATION_SLUG>",
		"trialEndsAt":      ti.trialEndsAt.Format(time.RFC3339),
		"trialActive":      true,
	}
}

func trialStartedEvent(ti *testInstance, admin testAdmin) loops.SendEventInput {
	return loops.SendEventInput{
		Email:           admin.email,
		UserID:          admin.id,
		EventName:       "trial_started",
		EventProperties: activeProperties(ti),
		IdempotencyKey:  trialStartedIdempotencyKey(ti.organizationID, admin.id, ti.trialCreatedAt),
	}
}

type fakeWorkflowClient struct {
	mu               sync.Mutex
	contactsByEmail  map[string]*loops.Contact
	contactsByUserID map[string]*loops.Contact
	findErrors       map[string]error
	updateErrors     map[string]error
	eventErrors      map[string]error
	findInputs       []loops.FindContactInput
	updateInputs     []loops.UpdateContactInput
	eventInputs      []loops.SendEventInput
}

func newFakeWorkflowClient() *fakeWorkflowClient {
	return &fakeWorkflowClient{
		contactsByEmail:  map[string]*loops.Contact{},
		contactsByUserID: map[string]*loops.Contact{},
		findErrors:       map[string]error{},
		updateErrors:     map[string]error{},
		eventErrors:      map[string]error{},
	}
}

func (c *fakeWorkflowClient) FindContact(_ context.Context, input loops.FindContactInput) (*loops.Contact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.findInputs = append(c.findInputs, input)
	if err := c.findErrors[input.Email]; err != nil {
		return nil, err
	}
	if input.UserID != "" {
		return c.contactsByUserID[input.UserID], nil
	}
	return c.contactsByEmail[input.Email], nil
}

func (c *fakeWorkflowClient) UpdateContact(_ context.Context, input loops.UpdateContactInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateInputs = append(c.updateInputs, input)
	if err := c.updateErrors[input.UserID]; err != nil {
		return err
	}
	contact := &loops.Contact{Email: input.Email, Subscribed: true}
	c.contactsByEmail[input.Email] = contact
	if input.UserID != "" {
		contact.UserID = &input.UserID
		c.contactsByUserID[input.UserID] = contact
	}
	return nil
}

func (c *fakeWorkflowClient) SendEvent(_ context.Context, input loops.SendEventInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventInputs = append(c.eventInputs, input)
	return c.eventErrors[input.UserID]
}

func (c *fakeWorkflowClient) finds() []loops.FindContactInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]loops.FindContactInput(nil), c.findInputs...)
}

func (c *fakeWorkflowClient) updates() []loops.UpdateContactInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]loops.UpdateContactInput(nil), c.updateInputs...)
}

func (c *fakeWorkflowClient) events() []loops.SendEventInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]loops.SendEventInput(nil), c.eventInputs...)
}
