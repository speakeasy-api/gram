package riskfindings

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/internal/conv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestSourceRejectsUnrepresentableBatchSize(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("oversized batch requires a 64-bit int")
	}
	oversized := int64(math.MaxInt32) + 1
	// Invalid input must be rejected before opening a database query.
	err := NewSource(nil).Read(t.Context(), pipeline.Criteria{CriteriaBatchSize: int(oversized)}, nil)
	require.ErrorContains(t, err, "source batch size exceeds SQLc limit")
}

func TestSourceContinuesBeyondCappedPage(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	chatID := tn.newChat(t, "", "")
	messageID := tn.newMessage(t, chatID, time.Now())
	const count = DefaultBatchSize + 1
	fixtures := make([]riskrepo.InsertRiskResultsParams, count)
	want := make([]uuid.UUID, count)
	for i := range fixtures {
		want[i] = uuid.Must(uuid.NewV7())
		fixtures[i] = riskrepo.InsertRiskResultsParams{
			ID: want[i], ProjectID: tn.projectID, OrganizationID: tn.orgID,
			RiskPolicyID: tn.policyID, RiskPolicyVersion: 1,
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "presidio", Found: true, RuleID: conv.ToPGText("pii.email_address"),
		}
	}
	n, err := riskrepo.New(tn.pool).InsertRiskResults(t.Context(), fixtures)
	require.NoError(t, err)
	require.EqualValues(t, count, n)

	source := NewSource(tn.pool)
	out := make(chan SourceRow, count)
	require.NoError(t, source.Read(t.Context(), pipeline.Criteria{
		CriteriaOrgID: tn.orgID, CriteriaBatchSize: math.MaxInt32,
	}, out))
	close(out)
	var got []uuid.UUID
	for row := range out {
		got = append(got, row.ID)
	}
	require.Equal(t, want, got)
	require.EqualValues(t, count, source.Scanned())
}
