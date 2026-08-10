package authz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestChallengeRowFromMessage(t *testing.T) {
	t.Parallel()

	message := testChallengeMessage()
	row, err := challengeRowFromMessage(message)
	require.NoError(t, err)

	require.Equal(t, message.GetId(), row.ID)
	require.Equal(t, message.GetTimestamp(), row.Timestamp.Format(time.RFC3339Nano))
	require.Equal(t, "org_test", row.OrganizationID)
	require.Equal(t, "project_test", row.ProjectID)
	require.Len(t, row.TraceID, maxChallengeTraceIDBytes)
	require.Len(t, row.SpanID, maxChallengeSpanIDBytes)
	require.Equal(t, "user:user_test", row.PrincipalURN)
	require.Equal(t, authzrepo.OutcomeAllow, row.Outcome)
	require.Equal(t, []string{"role:member"}, row.RoleSlugs)
	require.Equal(t, uint32(3), row.EvaluatedGrantCount)
	require.Equal(t, []authzrepo.RequestedCheck{{
		Scope:        "project:read",
		ResourceKind: "project",
		ResourceID:   "project_test",
		Selector:     `{"project_id":"project_test"}`,
	}}, row.RequestedChecks)
	require.Equal(t, []authzrepo.MatchedGrant{{
		PrincipalURN:         "role:member",
		Scope:                "project:read",
		Selector:             `{"project_id":"project_test"}`,
		MatchedViaCheckScope: "project:read",
	}}, row.MatchedGrants)
}

func TestChallengeCHWriterPersistsMessage(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	writer := NewChallengeCHWriter(testenv.NewLogger(t), conn)
	message := testChallengeMessage()

	err = writer.Handle(t.Context(), message, gcp.MessageMetadata{
		ID:              "message-id",
		Attributes:      nil,
		DeliveryAttempt: nil,
	})
	require.NoError(t, err)

	var count uint64
	err = conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
}

func TestChallengeCHWriterAcknowledgesInvalidMessage(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	writer := NewChallengeCHWriter(testenv.NewLogger(t), conn)
	id := "not-a-uuid"
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	err = writer.Handle(t.Context(), authzv1.Challenge_builder{
		Id:        &id,
		Timestamp: &timestamp,
	}.Build(), gcp.MessageMetadata{
		ID:              "message-id",
		Attributes:      nil,
		DeliveryAttempt: nil,
	})
	require.NoError(t, err)
}

func TestChallengeCHWriterAcknowledgesOverlongTraceID(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	writer := NewChallengeCHWriter(testenv.NewLogger(t), conn)
	message := testChallengeMessage()
	message.SetTraceId(strings.Repeat("a", maxChallengeTraceIDBytes+1))

	err = writer.Handle(t.Context(), message, gcp.MessageMetadata{
		ID:              "message-id",
		Attributes:      nil,
		DeliveryAttempt: nil,
	})
	require.NoError(t, err)

	var count uint64
	err = conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChallengeCHWriterAcknowledgesOverlongSpanID(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	writer := NewChallengeCHWriter(testenv.NewLogger(t), conn)
	message := testChallengeMessage()
	message.SetSpanId(strings.Repeat("a", maxChallengeSpanIDBytes+1))

	err = writer.Handle(t.Context(), message, gcp.MessageMetadata{
		ID:              "message-id",
		Attributes:      nil,
		DeliveryAttempt: nil,
	})
	require.NoError(t, err)

	var count uint64
	err = conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChallengeCHWriterRetriesClickHouseFailure(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	writer := NewChallengeCHWriter(testenv.NewLogger(t), conn)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = writer.Handle(ctx, testChallengeMessage(), gcp.MessageMetadata{
		ID:              "message-id",
		Attributes:      nil,
		DeliveryAttempt: nil,
	})
	require.ErrorContains(t, err, "insert authz challenge")
}

func testChallengeMessage() *authzv1.Challenge {
	id := uuid.NewString()
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	organizationID := "org_test"
	projectID := "project_test"
	traceID := strings.Repeat("a", maxChallengeTraceIDBytes)
	spanID := strings.Repeat("b", maxChallengeSpanIDBytes)
	principalURN := "user:user_test"
	principalType := "user"
	operation := "require"
	outcome := "allow"
	reason := "grant_matched"
	scope := "project:read"
	resourceKind := "project"
	resourceID := "project_test"
	selector := `{"project_id":"project_test"}`
	evaluatedGrantCount := uint32(3)

	matchedPrincipalURN := "role:member"
	return authzv1.Challenge_builder{
		Id:                  &id,
		Timestamp:           &timestamp,
		OrganizationId:      &organizationID,
		ProjectId:           &projectID,
		TraceId:             &traceID,
		SpanId:              &spanID,
		PrincipalUrn:        &principalURN,
		PrincipalType:       &principalType,
		RoleSlugs:           []string{"role:member"},
		Operation:           &operation,
		Outcome:             &outcome,
		Reason:              &reason,
		Scope:               &scope,
		ResourceKind:        &resourceKind,
		ResourceId:          &resourceID,
		Selector:            &selector,
		EvaluatedGrantCount: &evaluatedGrantCount,
		RequestedChecks: []*authzv1.Challenge_RequestedCheck{
			authzv1.Challenge_RequestedCheck_builder{
				Scope:        &scope,
				ResourceKind: &resourceKind,
				ResourceId:   &resourceID,
				Selector:     &selector,
			}.Build(),
		},
		MatchedGrants: []*authzv1.Challenge_MatchedGrant{
			authzv1.Challenge_MatchedGrant_builder{
				PrincipalUrn:         &matchedPrincipalURN,
				Scope:                &scope,
				Selector:             &selector,
				MatchedViaCheckScope: &scope,
			}.Build(),
		},
	}.Build()
}
