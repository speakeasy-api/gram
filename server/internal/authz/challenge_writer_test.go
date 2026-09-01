package authz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
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

// newChallengeCHWriter builds a writer over a live ClickHouse connection and
// returns both so tests can assert on the rows the writer persisted.
func newChallengeCHWriter(t *testing.T) (*ChallengeCHWriter, clickhouse.Conn) {
	t.Helper()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)

	return NewChallengeCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn), conn
}

func TestChallengeCHWriterPersistsMessage(t *testing.T) {
	t.Parallel()

	writer, conn := newChallengeCHWriter(t)
	message := testChallengeMessage()

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{message})
	require.Equal(t, []error{nil}, failed)

	var count uint64
	err := conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
}

func TestChallengeCHWriterPersistsBatch(t *testing.T) {
	t.Parallel()

	writer, conn := newChallengeCHWriter(t)
	first := testChallengeMessage()
	second := testChallengeMessage()

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{first, second})
	require.Equal(t, []error{nil, nil}, failed)

	var count uint64
	err := conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id IN (?, ?)`, first.GetId(), second.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, uint64(2), count)
}

// A poison record must not cost the batch its valid rows: the writer drops the
// bad message and inserts the rest.
func TestChallengeCHWriterSkipsInvalidMessageInBatch(t *testing.T) {
	t.Parallel()

	writer, conn := newChallengeCHWriter(t)
	valid := testChallengeMessage()
	invalid := testChallengeMessage()
	invalid.SetTraceId(strings.Repeat("a", maxChallengeTraceIDBytes+1))

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{invalid, valid})
	require.Equal(t, []error{nil, nil}, failed)

	var validCount uint64
	err := conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, valid.GetId()).Scan(&validCount)
	require.NoError(t, err)
	require.Equal(t, uint64(1), validCount)

	var invalidCount uint64
	err = conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, invalid.GetId()).Scan(&invalidCount)
	require.NoError(t, err)
	require.Zero(t, invalidCount)
}

// A failing insert says nothing about a poison record, so the poison keeps its
// nil entry and is acknowledged while only the rows the insert covered redeliver.
func TestChallengeCHWriterDropsPoisonWhenInsertFails(t *testing.T) {
	t.Parallel()

	writer, _ := newChallengeCHWriter(t)
	invalid := testChallengeMessage()
	invalid.SetTraceId(strings.Repeat("a", maxChallengeTraceIDBytes+1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	failed := writer.processBatch(ctx, []*authzv1.Challenge{invalid, testChallengeMessage()})
	require.Len(t, failed, 2)
	require.NoError(t, failed[0])
	require.ErrorContains(t, failed[1], "insert authz challenges")
}

func TestChallengeCHWriterAcknowledgesInvalidMessage(t *testing.T) {
	t.Parallel()

	writer, _ := newChallengeCHWriter(t)
	id := "not-a-uuid"
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{authzv1.Challenge_builder{
		Id:        &id,
		Timestamp: &timestamp,
	}.Build()})
	require.Equal(t, []error{nil}, failed)
}

func TestChallengeCHWriterAcknowledgesOverlongTraceID(t *testing.T) {
	t.Parallel()

	writer, conn := newChallengeCHWriter(t)
	message := testChallengeMessage()
	message.SetTraceId(strings.Repeat("a", maxChallengeTraceIDBytes+1))

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{message})
	require.Equal(t, []error{nil}, failed)

	var count uint64
	err := conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChallengeCHWriterAcknowledgesOverlongSpanID(t *testing.T) {
	t.Parallel()

	writer, conn := newChallengeCHWriter(t)
	message := testChallengeMessage()
	message.SetSpanId(strings.Repeat("a", maxChallengeSpanIDBytes+1))

	failed := writer.processBatch(t.Context(), []*authzv1.Challenge{message})
	require.Equal(t, []error{nil}, failed)

	var count uint64
	err := conn.QueryRow(t.Context(), `SELECT count() FROM authz_challenges WHERE id = ?`, message.GetId()).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChallengeCHWriterRetriesClickHouseFailure(t *testing.T) {
	t.Parallel()

	writer, _ := newChallengeCHWriter(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	failed := writer.processBatch(ctx, []*authzv1.Challenge{testChallengeMessage()})
	require.Len(t, failed, 1)
	require.ErrorContains(t, failed[0], "insert authz challenges")
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
