//nolint:glint // This test exercises raw feedback retention state that has no dedicated fixture helper.
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

	var stored struct {
		OrganizationID       string
		ConnectionID         uuid.UUID
		ConnectionGeneration uuid.UUID
		Rating               int
		Note                 string
		DeliveryState        string
		ExpiresAt            time.Time
	}
	err = conn.QueryRow(ctx, `
SELECT organization_id, connection_id, connection_generation, rating, note, delivery_state, expires_at
FROM platform_mcp_feedback
WHERE id = $1`, created.TrackingID).Scan(
		&stored.OrganizationID,
		&stored.ConnectionID,
		&stored.ConnectionGeneration,
		&stored.Rating,
		&stored.Note,
		&stored.DeliveryState,
		&stored.ExpiresAt,
	)
	require.NoError(t, err)
	connectionID, generation, err := parseConnection(principal)
	require.NoError(t, err)
	require.Equal(t, principal.OrganizationID, stored.OrganizationID)
	require.Equal(t, connectionID, stored.ConnectionID)
	require.Equal(t, generation, stored.ConnectionGeneration)
	require.Equal(t, rating, stored.Rating)
	require.Equal(t, "Helpful", stored.Note)
	require.Equal(t, feedbackDeliveryQueued, stored.DeliveryState)
	require.WithinDuration(t, created.ExpiresAt, stored.ExpiresAt, time.Second)

	_, err = conn.Exec(ctx, `UPDATE platform_mcp_feedback SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, created.TrackingID)
	require.NoError(t, err)
	_, err = service.Submit(ctx, principal, FeedbackInput{Category: "other", Note: "Fresh", IdempotencyKey: "feedback-after-expiry"})
	require.NoError(t, err)
	var expiredCount int
	err = conn.QueryRow(ctx, `SELECT COUNT(*) FROM platform_mcp_feedback WHERE id = $1`, created.TrackingID).Scan(&expiredCount)
	require.NoError(t, err)
	require.Zero(t, expiredCount)
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
		ActiveGeneration: uuid.New(),
		ReauthorizedAt:   pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ConnectionID:     connectionID,
		OrganizationID:   freshPrincipal.OrganizationID,
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

	_, err = conn.Exec(ctx, `SELECT 1`)
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
