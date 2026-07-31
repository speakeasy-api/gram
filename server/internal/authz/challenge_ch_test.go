package authz

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestChallengeCHWriter_HandleBatch_InsertsIntoClickHouse(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	w := NewChallengeCHWriter(testenv.NewLogger(t), conn)

	reqID := "req_" + uuid.NewString()
	id := uuid.NewString()
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	orgID := "org_" + uuid.NewString()
	msg := authzv1.ChallengeRow_builder{
		Id:             &id,
		Timestamp:      &ts,
		OrganizationId: &orgID,
		ProjectId:      new("proj_1"),
		TraceId:        new("trace"),
		SpanId:         new("span"),
		RequestId:      &reqID,
		PrincipalUrn:   new("user:u1"),
		PrincipalType:  new(string(authzrepo.PrincipalTypeUser)),
		UserId:         new("u1"),
		Operation:      new(string(authzrepo.OperationRequire)),
		Outcome:        new(string(authzrepo.OutcomeAllow)),
		Reason:         new(string(authzrepo.ReasonGrantMatched)),
		Scope:          new(string(ScopeProjectRead)),
		ResourceKind:   new("project"),
		ResourceId:     new("proj_1"),
		RequestedChecks: []*authzv1.ChallengeRow_RequestedCheck{
			authzv1.ChallengeRow_RequestedCheck_builder{
				Scope:        new(string(ScopeProjectRead)),
				ResourceKind: new("project"),
				ResourceId:   new("proj_1"),
				Selector:     new(`{"resource_kind":"project"}`),
			}.Build(),
		},
		EvaluatedGrantCount: new(uint32(2)),
	}.Build()

	require.NoError(t, w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, []gcp.MessageMetadata{{}}))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT id, request_id, operation, evaluated_grant_count, requested_checks.scope
			FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var gotID, op string
		var gotReqID *string
		var evalCount uint32
		var reqScopes []string
		if err := rows.Scan(&gotID, &gotReqID, &op, &evalCount, &reqScopes); err != nil {
			return false
		}
		return gotID == id &&
			gotReqID != nil && *gotReqID == reqID &&
			op == string(authzrepo.OperationRequire) &&
			evalCount == 2 &&
			len(reqScopes) == 1 && reqScopes[0] == string(ScopeProjectRead)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeCHWriter_HandleBatch_SkipsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	w := NewChallengeCHWriter(testenv.NewLogger(t), conn)

	id := uuid.NewString()
	orgID := "org_" + uuid.NewString()
	badTS := "not-a-timestamp"
	msg := authzv1.ChallengeRow_builder{
		Id:             &id,
		Timestamp:      &badTS,
		OrganizationId: &orgID,
	}.Build()

	require.NoError(t, w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, nil))

	rows, err := conn.Query(t.Context(), `
		SELECT count() FROM authz_challenges WHERE organization_id = ?
	`, orgID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var n uint64
	require.NoError(t, rows.Scan(&n))
	require.Equal(t, uint64(0), n)
}

func TestChallengeCHWriter_HandleBatch_InsertErrorNacks(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	w := NewChallengeCHWriter(testenv.NewLogger(t), conn)

	id := uuid.NewString()
	ts := time.Now().UTC().Format(time.RFC3339)
	msg := authzv1.ChallengeRow_builder{
		Id:        &id,
		Timestamp: &ts,
	}.Build()

	err = w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, nil)
	require.Error(t, err)
}

func TestChallengeRowProtoRoundTrip(t *testing.T) {
	t.Parallel()

	email := "a@example.com"
	reqID := "req_1"
	row := authzrepo.ChallengeRow{
		ID:             uuid.NewString(),
		Timestamp:      time.Now().UTC().Truncate(time.Millisecond),
		OrganizationID: "org_1",
		ProjectID:      "proj_1",
		TraceID:        "t",
		SpanID:         "s",
		RequestID:      &reqID,
		PrincipalURN:   "user:u",
		PrincipalType:  authzrepo.PrincipalTypeUser,
		UserEmail:      &email,
		RoleSlugs:      []string{"admin"},
		Operation:      authzrepo.OperationFilter,
		Outcome:        authzrepo.OutcomeAllow,
		Reason:         authzrepo.ReasonGrantMatched,
		Scope:          string(ScopeProjectRead),
		ResourceKind:   "project",
		ResourceID:     "proj_1",
		ExpandedScopes: []string{string(ScopeRoot), string(ScopeProjectRead)},
		RequestedChecks: []authzrepo.RequestedCheck{
			{Scope: string(ScopeProjectRead), ResourceKind: "project", ResourceID: "proj_1", Selector: "{}"},
		},
		MatchedGrants: []authzrepo.MatchedGrant{
			{PrincipalURN: "role:admin", Scope: string(ScopeProjectRead), Selector: "{}", MatchedViaCheckScope: string(ScopeProjectRead)},
		},
		EvaluatedGrantCount:  3,
		FilterCandidateCount: 4,
		FilterAllowedCount:   1,
	}

	got, err := challengeRowFromProto(challengeRowToProto(row))
	require.NoError(t, err)
	require.Equal(t, row.ID, got.ID)
	require.Equal(t, row.OrganizationID, got.OrganizationID)
	require.Equal(t, row.RequestID, got.RequestID)
	require.Equal(t, row.UserEmail, got.UserEmail)
	require.Equal(t, row.RoleSlugs, got.RoleSlugs)
	require.Equal(t, row.Operation, got.Operation)
	require.Equal(t, row.ExpandedScopes, got.ExpandedScopes)
	require.Equal(t, row.RequestedChecks, got.RequestedChecks)
	require.Equal(t, row.MatchedGrants, got.MatchedGrants)
	require.Equal(t, row.FilterCandidateCount, got.FilterCandidateCount)
	require.Equal(t, row.FilterAllowedCount, got.FilterAllowedCount)
	require.True(t, row.Timestamp.Equal(got.Timestamp))
}
