package chrepo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCopyProjection_LockstepWithInsertColumns pins the INSERT ... SELECT
// copy projection to the shared riskFindingColumns list: every column is
// passed through verbatim except inserted_at and the two exclusion flags,
// which are replaced. A new risk_findings column added to InsertRiskFindings
// automatically joins the projection — this test fails only if the
// projection helper's replacement set drifts.
func TestCopyProjection_LockstepWithInsertColumns(t *testing.T) {
	t.Parallel()

	projected := strings.Split(copyProjection("?", "NULL"), ", ")
	require.Len(t, projected, len(riskFindingColumns))

	for i, col := range riskFindingColumns {
		switch col {
		case "inserted_at":
			require.Equal(t, "?", projected[i])
		case "excluded_at":
			require.Equal(t, "?", projected[i])
		case "exclusion_id":
			require.Equal(t, "NULL", projected[i])
		default:
			require.Equal(t, col, projected[i], "column %d must pass through verbatim", i)
		}
	}
}

func TestRetroExclusionPredicate_ApplyConditions(t *testing.T) {
	t.Parallel()

	// Exactly one matcher is required.
	_, _, err := RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.applyConditions()
	require.Error(t, err)

	_, _, err = RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "secret.github_pat",
		Source:             "gitleaks",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.applyConditions()
	require.Error(t, err)

	// Full predicate: policy scope, fingerprint matcher, both filters — the
	// clause order matches the argument order.
	conds, args, err := RetroExclusionPredicate{
		PolicyID:           "policy-1",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: []string{"fp1", "fp2"},
		RuleIDFilter:       "secret.github_pat",
		SourceFilter:       "gitleaks",
	}.applyConditions()
	require.NoError(t, err)
	require.Equal(t,
		"dead_letter_reason = '' AND excluded_at IS NULL AND risk_policy_id = ? AND fingerprint_tenant_hs256 IN (?,?) AND rule_id = ? AND source = ?",
		conds,
	)
	require.Equal(t, []any{"policy-1", "fp1", "fp2", "secret.github_pat", "gitleaks"}, args)
}

// TestRetroReversalKeep pins the keep guards for an active exclusion's
// reversal: KeepMatching mirrors the apply's match conditions except that the
// exact matcher also retains rows with no stored fingerprint (unprovable,
// un-restorable); KeepScope keeps only the SQL-provable scope for regex.
func TestRetroReversalKeep(t *testing.T) {
	t.Parallel()

	exact := RetroExclusionPredicate{
		PolicyID:           "policy-1",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: []string{"fp1", "fp2"},
		RuleIDFilter:       "secret.github_pat",
		SourceFilter:       "",
	}
	keep, err := exact.KeepMatching()
	require.NoError(t, err)
	require.Equal(t,
		"rn = 1 AND exclusion_id = ? AND NOT (risk_policy_id = ? AND (fingerprint_tenant_hs256 = '' OR fingerprint_tenant_hs256 IN (?,?)) AND rule_id = ?)",
		reversalWhere(keep),
	)
	require.Equal(t, []any{"policy-1", "fp1", "fp2", "secret.github_pat"}, keep.args)

	// KeepMatching demands exactly one matcher, like the apply.
	_, err = RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.KeepMatching()
	require.Error(t, err)

	// KeepScope: policy + filters only; empty scope means a blanket reversal
	// shape (callers skip the statement instead).
	regex := RetroExclusionPredicate{
		PolicyID:           "policy-1",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "gitleaks",
	}
	scope := regex.KeepScope()
	require.False(t, scope.Empty())
	require.Equal(t, "rn = 1 AND exclusion_id = ? AND NOT (risk_policy_id = ? AND source = ?)", reversalWhere(scope))

	unscoped := RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.KeepScope()
	require.True(t, unscoped.Empty())
	require.Equal(t, "rn = 1 AND exclusion_id = ?", reversalWhere(unscoped))
	require.Equal(t, "rn = 1 AND exclusion_id = ?", reversalWhere(BlanketReversal()))
}
