package risk

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// The FP mirror republishes batch-scanned Postgres rows, so its surfaces must
// match the offline backfill's per-source mapping — NOT the live stream
// defaults (batch gitleaks offsets index the composed scan surface, batch
// presidio offsets a YAML transform). A drift here would let a dismiss/undo
// rewrite a backfilled row's reveal semantics.
func TestFPMirrorSurface(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"gitleaks":         "scan_surface",
		"presidio":         "legacy_presidio",
		"prompt_injection": "none",
		"llm_judge":        "none",
		"shadow_mcp":       "derived",
		"account_identity": "derived",
		"destructive_tool": "derived",
		"cli_destructive":  "derived",
		"custom":           "",
		"":                 "",
	}
	for source, want := range cases {
		require.Equal(t, want, fpMirrorSurface(source), "fpMirrorSurface(%q)", source)
	}
}

// TestFPMirrorMessage pins the converged suppression state on the mirror's
// republished message: a mark carries excluded_at (same timestamp as the
// legacy false_positive_at), excluded_reason=manual and the user reason as
// excluded_detail; an unmark carries none of it, so the CH writer's exclusion
// check decides the republished row's state.
func TestFPMirrorMessage(t *testing.T) {
	t.Parallel()

	fpAt := time.Date(2026, 8, 1, 12, 30, 0, 123456789, time.UTC)
	row := repo.RiskResult{
		ID:                  uuid.Must(uuid.NewV7()),
		ProjectID:           uuid.Must(uuid.NewV7()),
		OrganizationID:      "org-1",
		RiskPolicyID:        uuid.Must(uuid.NewV7()),
		RiskPolicyVersion:   3,
		Source:              "gitleaks",
		CreatedAt:           pgtype.Timestamptz{Time: fpAt.Add(-time.Hour), Valid: true, InfinityModifier: 0},
		FalsePositiveAt:     pgtype.Timestamptz{Time: fpAt, Valid: true, InfinityModifier: 0},
		FalsePositiveReason: pgtype.Text{String: "test data", Valid: true},
	}

	marked := fpMirrorMessage(row)
	require.Equal(t, "2026-08-01T12:30:00.123456789Z", marked.GetFalsePositiveAt(), "fractional seconds survive the mirror format")
	require.Equal(t, "2026-08-01T12:30:00.123456789Z", marked.GetExcludedAt(), "excluded_at mirrors the mark timestamp")
	require.Equal(t, chrepo.ExcludedReasonManual, marked.GetExcludedReason())
	require.Equal(t, "test data", marked.GetExcludedDetail())

	row.FalsePositiveAt = pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: 0}
	row.FalsePositiveReason = pgtype.Text{String: "", Valid: false}
	unmarked := fpMirrorMessage(row)
	require.Empty(t, unmarked.GetFalsePositiveAt())
	require.Empty(t, unmarked.GetExcludedAt(), "an unmark republish carries no excluded state")
	require.Empty(t, unmarked.GetExcludedReason())
	require.Empty(t, unmarked.GetExcludedDetail())
}
