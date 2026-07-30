package riskfindingscols

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
)

func TestSourcePaginatesInIdOrderAndResumes(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	chatID := tn.newChat(t)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	ids := make([]uuid.UUID, 0, 5)
	for i := range 5 {
		msgID := tn.newMessage(t, chatID, base.Add(time.Duration(i)*time.Minute))
		ids = append(ids, tn.newFinding(t, msgID, base.Add(time.Duration(i)*time.Minute+30*time.Second)))
	}
	// UUIDv7 ids minted in the same millisecond are not guaranteed monotonic;
	// the source contract is id order, so compare against the sorted set.
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

	// A page size smaller than the row count forces multiple keyset pages.
	source := NewSource(tn.pool)
	rows := readAll(t, source, pipeline.Criteria{
		CriteriaOrgID:     tn.orgID,
		CriteriaBatchSize: 2,
	})

	require.Len(t, rows, 5)
	require.EqualValues(t, 5, source.Scanned())
	for i, row := range rows {
		require.Equal(t, ids[i], row.ID, "rows must stream in id order")
	}

	// Resuming after the third id re-reads exactly the tail, honoring the
	// exclusive cursor.
	resumed := readAll(t, NewSource(tn.pool), pipeline.Criteria{
		CriteriaOrgID:     tn.orgID,
		CriteriaBatchSize: 2,
		CriteriaCursor:    ids[2],
	})
	require.Len(t, resumed, 2)
	require.Equal(t, ids[3], resumed[0].ID)
	require.Equal(t, ids[4], resumed[1].ID)
}

func TestSourceHonorsTenantAndTimeBounds(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	chatID := tn.newChat(t)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	early := tn.newFinding(t, tn.newMessage(t, chatID, base), base)
	middle := tn.newFinding(t, tn.newMessage(t, chatID, base.Add(time.Hour)), base.Add(time.Hour))
	late := tn.newFinding(t, tn.newMessage(t, chatID, base.Add(2*time.Hour)), base.Add(2*time.Hour))

	source := NewSource(tn.pool)

	// A different org matches nothing.
	require.Empty(t, readAll(t, source, pipeline.Criteria{CriteriaOrgID: "org_other"}))

	// A different project matches nothing.
	require.Empty(t, readAll(t, source, pipeline.Criteria{
		CriteriaOrgID:     tn.orgID,
		CriteriaProjectID: uuid.New(),
	}))

	// The matching project returns everything.
	require.Len(t, readAll(t, source, pipeline.Criteria{
		CriteriaOrgID:     tn.orgID,
		CriteriaProjectID: tn.projectID,
	}), 3)

	// [from, to) brackets exactly the middle finding: from is inclusive, to is
	// exclusive.
	windowed := readAll(t, source, pipeline.Criteria{
		CriteriaOrgID: tn.orgID,
		CriteriaFrom:  base.Add(time.Hour),
		CriteriaTo:    base.Add(2 * time.Hour),
	})
	require.Len(t, windowed, 1)
	require.Equal(t, middle, windowed[0].ID)

	// The bounds by themselves keep the edges out.
	fromOnly := readAll(t, source, pipeline.Criteria{
		CriteriaOrgID: tn.orgID,
		CriteriaFrom:  base.Add(2 * time.Hour),
	})
	require.Len(t, fromOnly, 1)
	require.Equal(t, late, fromOnly[0].ID)

	toOnly := readAll(t, source, pipeline.Criteria{
		CriteriaOrgID: tn.orgID,
		CriteriaTo:    base.Add(time.Hour),
	})
	require.Len(t, toOnly, 1)
	require.Equal(t, early, toOnly[0].ID)
}

func TestSourceResolvesAssistantAndMessageTime(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)

	// Chat A backs a live assistant thread.
	chatA := tn.newChat(t)
	assistantID, _ := tn.linkAssistant(t, chatA)
	msgTimeA := base
	findingA := tn.newFinding(t, tn.newMessage(t, chatA, msgTimeA), base.Add(time.Hour))

	// Chat B's thread is soft-deleted: no assistant attribution.
	chatB := tn.newChat(t)
	_, threadB := tn.linkAssistant(t, chatB)
	tn.softDeleteThread(t, threadB)
	msgTimeB := base.Add(time.Minute)
	findingB := tn.newFinding(t, tn.newMessage(t, chatB, msgTimeB), base.Add(time.Hour))

	// Chat C never had a thread.
	chatC := tn.newChat(t)
	msgTimeC := base.Add(2 * time.Minute)
	findingC := tn.newFinding(t, tn.newMessage(t, chatC, msgTimeC), base.Add(time.Hour))

	rows := readAll(t, NewSource(tn.pool), pipeline.Criteria{CriteriaOrgID: tn.orgID})
	require.Len(t, rows, 3)

	byID := make(map[uuid.UUID]SourceRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	a := byID[findingA]
	require.Equal(t, assistantID.String(), a.AssistantID)
	require.True(t, msgTimeA.Equal(a.MessageCreatedAt), "message_created_at must be the chat message event time")
	require.True(t, base.Add(time.Hour).Equal(a.CreatedAt))

	b := byID[findingB]
	require.Empty(t, b.AssistantID, "a soft-deleted thread must not attribute an assistant")
	require.True(t, msgTimeB.Equal(b.MessageCreatedAt))

	c := byID[findingC]
	require.Empty(t, c.AssistantID)
	require.True(t, msgTimeC.Equal(c.MessageCreatedAt))
}

func TestSourceFallsBackForContentPartFinding(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	chatID := tn.newChat(t)
	// Even with a live thread on the chat, a content-part-anchored finding has
	// no chat message to join through, so both enrichments fall back: the
	// assistant lookup keys off the missing message's chat and yields '', and
	// message_created_at collapses to the finding's own created_at (the same
	// value the ClickHouse column DEFAULT computes).
	tn.linkAssistant(t, chatID)

	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	findingID := tn.newContentPartFinding(t, chatID, createdAt)

	rows := readAll(t, NewSource(tn.pool), pipeline.Criteria{CriteriaOrgID: tn.orgID})
	require.Len(t, rows, 1)
	require.Equal(t, findingID, rows[0].ID)
	require.True(t, createdAt.Equal(rows[0].MessageCreatedAt), "content-part findings fall back to the finding created_at")
	require.Empty(t, rows[0].AssistantID)
}

func TestSourceSkipsNonFindings(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	chatID := tn.newChat(t)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-24 * time.Hour)
	msgID := tn.newMessage(t, chatID, base)
	finding := tn.newFinding(t, msgID, base.Add(time.Minute))
	tn.newNonFinding(t, msgID, false) // found=false sentinel
	tn.newNonFinding(t, msgID, true)  // found=true but rule_id IS NULL

	rows := readAll(t, NewSource(tn.pool), pipeline.Criteria{CriteriaOrgID: tn.orgID})
	require.Len(t, rows, 1, "only clean true positives exist in ClickHouse, so only they are worth mutating")
	require.Equal(t, finding, rows[0].ID)
}
