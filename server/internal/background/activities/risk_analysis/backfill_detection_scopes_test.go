package risk_analysis_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBackfillDetectionScopes_ComposesAndClearsLegacyFields(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	queries := riskrepo.New(conn)
	require.NoError(t, queries.SetRiskPolicyLegacyScopeFields(t.Context(), riskrepo.SetRiskPolicyLegacyScopeFieldsParams{
		MessageTypes: []string{message.ToolRequest},
		ScopeInclude: pgtype.Text{},
		ScopeExempt:  pgtype.Text{String: `content.matchText("harmless")`, Valid: true},
		ID:           td.policyID,
		ProjectID:    td.projectID,
	}))

	backfill := risk_analysis.NewBackfillDetectionScopes(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		conn,
		mustCELEngine(t),
	)
	result, err := backfill.Do(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Migrated)
	require.Equal(t, 0, result.Skipped)

	migrated, err := queries.GetRiskPolicy(t.Context(), riskrepo.GetRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	require.Empty(t, migrated.MessageTypes)
	require.False(t, migrated.ScopeInclude.Valid)
	require.False(t, migrated.ScopeExempt.Valid)
	require.Equal(t, td.policyVersion, migrated.Version, "behavior-preserving backfill must not bump the version")

	specs := risk_analysis.DetectionScopesFromConfig(migrated.AnalyzerConfig)
	require.Len(t, specs, 1, "gitleaks policy composes only the secrets category")
	require.Equal(t, "secrets", specs[0].Category)
	require.Equal(t, `kind == "tool_request"`, specs[0].ScopeInclude)
	require.Equal(t, `(kind == "assistant_message") || (content.matchText("harmless"))`, specs[0].ScopeExempt)

	// Idempotence: a second run finds nothing left to migrate.
	again, err := backfill.Do(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, again.Migrated)
}

func TestBackfillDetectionScopes_LeavesCleanPoliciesUntouched(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	backfill := risk_analysis.NewBackfillDetectionScopes(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		conn,
		mustCELEngine(t),
	)
	result, err := backfill.Do(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.Migrated)

	row, err := riskrepo.New(conn).GetRiskPolicy(t.Context(), riskrepo.GetRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	require.Empty(t, risk_analysis.DetectionScopesFromConfig(row.AnalyzerConfig))
}
