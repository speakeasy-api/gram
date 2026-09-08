package risk_analysis

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// permuteDeletedRows returns every ordering of rows, for exhausting the
// unspecified order a DELETE ... RETURNING can produce.
func permuteDeletedRows(rows []repo.DeleteRiskResultsForUnitsRow) [][]repo.DeleteRiskResultsForUnitsRow {
	if len(rows) <= 1 {
		return [][]repo.DeleteRiskResultsForUnitsRow{append([]repo.DeleteRiskResultsForUnitsRow(nil), rows...)}
	}
	var out [][]repo.DeleteRiskResultsForUnitsRow
	for i := range rows {
		rest := make([]repo.DeleteRiskResultsForUnitsRow, 0, len(rows)-1)
		rest = append(rest, rows[:i]...)
		rest = append(rest, rows[i+1:]...)
		for _, tail := range permuteDeletedRows(rest) {
			out = append(out, append([]repo.DeleteRiskResultsForUnitsRow{rows[i]}, tail...))
		}
	}
	return out
}

// TestPriorRowIndex_DismissalsDeterministicAcrossPermutations pins the
// invariant behind the dismissed-first sort in priorRowIndex: a group of k
// byte-identical findings with d dismissals must map those d dismissals onto
// the d LOWEST recompute ordinals — the ids buildRows reproduces first —
// identically for every order the DELETE happens to return the rows in.
// Otherwise a redrive could scatter a dismissal onto an ordinal the scanner
// did not reproduce and silently lose it.
func TestPriorRowIndex_DismissalsDeterministicAcrossPermutations(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	policyID := uuid.New()
	msgID := uuid.NullUUID{UUID: uuid.New(), Valid: true}

	base := repo.DeleteRiskResultsForUnitsRow{
		ID:                  uuid.Nil,
		RiskPolicyVersion:   3,
		ChatMessageID:       msgID,
		ChatContentPartID:   uuid.NullUUID{},
		Found:               true,
		Source:              "gitleaks",
		RuleID:              pgtype.Text{String: "generic-api-key", Valid: true},
		Description:         pgtype.Text{String: "Generic API key", Valid: true},
		Match:               pgtype.Text{String: "IDENTICAL_MATCH_TOKEN", Valid: true},
		StartPos:            pgtype.Int4{Int32: 10, Valid: true},
		EndPos:              pgtype.Int4{Int32: 31, Valid: true},
		DeadLetterReason:    pgtype.Text{String: "", Valid: false},
		FalsePositiveAt:     pgtype.Timestamptz{},
		FalsePositiveReason: pgtype.Text{String: "", Valid: false},
	}

	// k=4 byte-identical rows under distinct legacy stored ids, d=2 dismissed
	// with distinct reasons so the assignment (not just the count) is pinned.
	deleted := make([]repo.DeleteRiskResultsForUnitsRow, 4)
	for i := range deleted {
		deleted[i] = base
		deleted[i].ID = uuid.UUID{byte(i + 1)}
	}
	deleted[1].FalsePositiveAt = pgtype.Timestamptz{Time: base.FalsePositiveAt.Time.AddDate(2026, 0, 0), Valid: true}
	deleted[1].FalsePositiveReason = pgtype.Text{String: "reason-b", Valid: true}
	deleted[3].FalsePositiveAt = deleted[1].FalsePositiveAt
	deleted[3].FalsePositiveReason = pgtype.Text{String: "reason-d", Valid: true}

	// The recompute ordinals in mint order: ordinal 0 and 1 are the ids
	// buildRows assigns to the first two reproduced siblings.
	key := deletedRowIdentity(projectID, policyID, base).key()
	mint := newResultRowIDs()
	ordinalIDs := []uuid.UUID{mint.mint(key), mint.mint(key), mint.mint(key), mint.mint(key)}

	for _, perm := range permuteDeletedRows(deleted) {
		index := priorRowIndex(projectID, policyID, perm)
		require.Len(t, index, 4)

		var dismissedIDs []uuid.UUID
		for id, row := range index {
			if row.FalsePositiveAt.Valid {
				dismissedIDs = append(dismissedIDs, id)
			}
		}
		require.ElementsMatch(t, ordinalIDs[:2], dismissedIDs,
			"dismissals must land on the lowest ordinals for every RETURNING order")

		// The stored-id tiebreak fixes which dismissal takes which ordinal:
		// deleted[1] (smaller stored id) always precedes deleted[3].
		require.Equal(t, "reason-b", index[ordinalIDs[0]].FalsePositiveReason.String)
		require.Equal(t, "reason-d", index[ordinalIDs[1]].FalsePositiveReason.String)
	}
}
