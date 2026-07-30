package authz

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeChallengeInserter struct {
	rows []authzrepo.ChallengeRow
	err  error
}

func (f *fakeChallengeInserter) InsertChallenge(_ context.Context, row authzrepo.ChallengeRow) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, row)
	return nil
}

func TestChallengeCHWriter_HandleBatch_InsertsMappedRows(t *testing.T) {
	t.Parallel()

	ins := &fakeChallengeInserter{}
	w := NewChallengeCHWriter(testenv.NewLogger(t), ins)

	reqID := "req_" + uuid.NewString()
	id := uuid.NewString()
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	orgID := "org_" + uuid.NewString()
	msg := authzv1.ChallengeRow_builder{
		Id:             &id,
		Timestamp:      &ts,
		OrganizationId: &orgID,
		ProjectId:      ptr("proj_1"),
		TraceId:        ptr("trace"),
		SpanId:         ptr("span"),
		RequestId:      &reqID,
		PrincipalUrn:   ptr("user:u1"),
		PrincipalType:  ptr(string(authzrepo.PrincipalTypeUser)),
		UserId:         ptr("u1"),
		Operation:      ptr(string(authzrepo.OperationRequire)),
		Outcome:        ptr(string(authzrepo.OutcomeAllow)),
		Reason:         ptr(string(authzrepo.ReasonGrantMatched)),
		Scope:          ptr(string(ScopeProjectRead)),
		ResourceKind:   ptr("project"),
		ResourceId:     ptr("proj_1"),
		RequestedChecks: []*authzv1.ChallengeRow_RequestedCheck{
			authzv1.ChallengeRow_RequestedCheck_builder{
				Scope:        ptr(string(ScopeProjectRead)),
				ResourceKind: ptr("project"),
				ResourceId:   ptr("proj_1"),
				Selector:     ptr(`{"resource_kind":"project"}`),
			}.Build(),
		},
		EvaluatedGrantCount: ptrUint32(2),
	}.Build()

	err := w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, []gcp.MessageMetadata{{}})
	require.NoError(t, err)
	require.Len(t, ins.rows, 1)
	got := ins.rows[0]
	require.Equal(t, id, got.ID)
	require.Equal(t, orgID, got.OrganizationID)
	require.Equal(t, &reqID, got.RequestID)
	require.Equal(t, authzrepo.OperationRequire, got.Operation)
	require.Equal(t, uint32(2), got.EvaluatedGrantCount)
	require.Len(t, got.RequestedChecks, 1)
	require.Equal(t, string(ScopeProjectRead), got.RequestedChecks[0].Scope)
}

func TestChallengeCHWriter_HandleBatch_SkipsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	ins := &fakeChallengeInserter{}
	w := NewChallengeCHWriter(testenv.NewLogger(t), ins)

	id := uuid.NewString()
	badTS := "not-a-timestamp"
	msg := authzv1.ChallengeRow_builder{
		Id:        &id,
		Timestamp: &badTS,
	}.Build()

	err := w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, nil)
	require.NoError(t, err)
	require.Empty(t, ins.rows)
}

func TestChallengeCHWriter_HandleBatch_InsertErrorNacks(t *testing.T) {
	t.Parallel()

	ins := &fakeChallengeInserter{err: context.DeadlineExceeded}
	w := NewChallengeCHWriter(testenv.NewLogger(t), ins)

	id := uuid.NewString()
	ts := time.Now().UTC().Format(time.RFC3339)
	msg := authzv1.ChallengeRow_builder{
		Id:        &id,
		Timestamp: &ts,
	}.Build()

	err := w.HandleBatch(t.Context(), []*authzv1.ChallengeRow{msg}, nil)
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

func ptr(s string) *string { return &s }

func ptrUint32(v uint32) *uint32 { return &v }
