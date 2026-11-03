package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestFeedbackServicePersistsBoundedLocalFeedbackAndReplaysExactRetries(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_feedback")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	service := NewFeedbackService(conn)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	rating := 5
	input := FeedbackInput{Category: "success", Rating: &rating, ToolName: "get_platform_context", Note: "Helpful", IdempotencyKey: "feedback-replay"}

	created, err := service.Submit(ctx, principal, input)
	require.NoError(t, err)
	require.NotEmpty(t, created.TrackingID)
	require.Equal(t, feedbackDeliveryQueued, created.DeliveryState)
	require.WithinDuration(t, service.now().Add(feedbackRetention), created.ExpiresAt, time.Second)
	require.False(t, created.Replayed)

	replayed, err := service.Submit(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, created.TrackingID, replayed.TrackingID)

	input.Note = "Different"
	_, err = service.Submit(ctx, principal, input)
	require.ErrorIs(t, err, ErrFeedbackConflict)

	trackingID, err := uuid.Parse(created.TrackingID)
	require.NoError(t, err)
	stored, err := platformrepo.New(conn).GetPlatformMCPFeedbackFixture(ctx, trackingID)
	require.NoError(t, err)
	connectionID, generation, err := parseConnection(principal)
	require.NoError(t, err)
	require.Equal(t, principal.OrganizationID, stored.OrganizationID)
	require.Equal(t, uuid.NullUUID{UUID: connectionID, Valid: true}, stored.ConnectionID)
	require.Equal(t, uuid.NullUUID{UUID: generation, Valid: true}, stored.ConnectionGeneration)
	require.Equal(t, pgtype.Int4{Int32: int32(rating), Valid: true}, stored.Rating)
	require.Equal(t, pgtype.Text{String: "Helpful", Valid: true}, stored.Note)
	require.Equal(t, feedbackDeliveryQueued, stored.DeliveryState)
	require.True(t, stored.ExpiresAt.Valid)
	require.WithinDuration(t, created.ExpiresAt, stored.ExpiresAt.Time, time.Second)

	err = platformrepo.New(conn).ExpirePlatformMCPFeedbackFixture(ctx, trackingID)
	require.NoError(t, err)
	_, err = service.Submit(ctx, principal, FeedbackInput{Category: "other", Note: "Fresh", IdempotencyKey: "feedback-after-expiry"})
	require.NoError(t, err)
	// The idempotency key identifies the created row (the replay above returned
	// its tracking ID), so its absence proves the retention purge removed it.
	_, err = platformrepo.New(conn).GetPlatformMCPFeedbackByIdempotencyKey(ctx, platformrepo.GetPlatformMCPFeedbackByIdempotencyKeyParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		IdempotencyKey: "feedback-replay",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestFeedbackServiceEnforcesConnectionLimitAndRejectsReplacedGeneration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_feedback_limits")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	service := NewFeedbackService(conn)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }

	for i := range feedbackConnectionHourlyLimit {
		_, err := service.Submit(ctx, principal, FeedbackInput{Category: "other", Note: "Note", IdempotencyKey: "feedback-limit-" + string(rune('a'+i))})
		require.NoError(t, err)
	}
	_, err = service.Submit(ctx, principal, FeedbackInput{Category: "other", Note: "Note", IdempotencyKey: "feedback-limit-over"})
	require.ErrorIs(t, err, ErrFeedbackRateLimited)

	freshPrincipal, _ := seedRegistrationLifecycle(t, ctx, conn)
	connectionID, _, err := parseConnection(freshPrincipal)
	require.NoError(t, err)
	_, err = platformrepo.New(conn).RotatePlatformMCPConnectionGeneration(ctx, platformrepo.RotatePlatformMCPConnectionGenerationParams{
		ActiveGeneration:       uuid.New(),
		ReauthorizedAt:         pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		AuthorizationExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(90 * 24 * time.Hour), Valid: true},
		ConnectionID:           connectionID,
		OrganizationID:         freshPrincipal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = service.Submit(ctx, freshPrincipal, FeedbackInput{Category: "other", Note: "Note", IdempotencyKey: "feedback-revoked-generation"})
	require.ErrorIs(t, err, ErrFeedbackForbidden)
}

func TestFeedbackServiceRejectsCrossTenantConnectionBinding(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_feedback_tenant")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	foreignPrincipal := principal
	foreignPrincipal.OrganizationID = "org_" + uuid.NewString()

	_, err = NewFeedbackService(conn).Submit(ctx, foreignPrincipal, FeedbackInput{Category: "other", Note: "Note", IdempotencyKey: "feedback-cross-tenant"})
	require.ErrorIs(t, err, ErrFeedbackForbidden)

	err = platformrepo.New(conn).PingPlatformMCPDatabaseFixture(ctx)
	require.NoError(t, err)
	_, err = platformrepo.New(conn).GetPlatformMCPFeedbackByIdempotencyKey(ctx, platformrepo.GetPlatformMCPFeedbackByIdempotencyKeyParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		IdempotencyKey: "feedback-cross-tenant",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestFeedbackServiceReturnsUnavailableWithoutDatabase(t *testing.T) {
	t.Parallel()

	_, err := NewFeedbackService(nil).Submit(context.Background(), testPrincipal(), FeedbackInput{Category: "other", IdempotencyKey: "feedback-unavailable"})
	require.ErrorIs(t, err, ErrFeedbackUnavailable)
}
