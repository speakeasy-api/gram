package chrepo

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRiskFindingStatesFoldsLikeEveryRiskRead: risk_finding_states_mv folds a
// finding's copies in ClickHouse, so it cannot share this package's SQL. It
// must still rank copies, set aside shadow rows and decide liveness the way
// every read here does, or analytics counts would drift from the Watchdog page.
func TestRiskFindingStatesFoldsLikeEveryRiskRead(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../clickhouse/schema.sql")
	require.NoError(t, err)
	match := regexp.MustCompile(`(?s)CREATE MATERIALIZED VIEW IF NOT EXISTS risk_finding_states_mv .*?;`).Find(schema)
	require.NotNil(t, match, "schema.sql defines risk_finding_states_mv")
	view := string(match)

	version := "(toUInt8" + stateRankSQL + ", inserted_at)"
	require.Contains(t, view, version, "copies rank as LatestCopyOrderSQL ranks them")
	require.Equal(t, strings.Count(view, "State("), strings.Count(view, version), "every state is taken by that rank")
	require.Contains(t, view, "toUInt8("+liveStateCond+" AND dead_letter_reason = '')", "liveness is as every read decides it")
	require.Contains(t, view, "WHERE "+notShadowCond, "shadow rows are never folded")
	require.Contains(t, view, "FROM risk_findings")
}
