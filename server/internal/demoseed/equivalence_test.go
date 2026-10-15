//go:build demoseed_safety && demoseed_equiv

package demoseed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestGoSeedMatchesLegacy is a temporary oracle for the SQL → Go port: it
// runs the legacy SQL seed twice (to learn which columns differ run to run —
// random ids, now()-based times) and the Go seed once, then compares every
// Postgres row with the volatile columns masked, and the ClickHouse tables by
// count and by breakdown.
//
//	mise run test:server -tags=demoseed_safety,demoseed_equiv -run TestGoSeedMatchesLegacy ./internal/demoseed/
func TestGoSeedMatchesLegacy(t *testing.T) {
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)
	spec := DefaultSpec()

	type result struct {
		pg        demoseedtest.PostgresSnapshot
		chCounts  map[string]uint64
		breakdown map[string]uint64
	}
	run := func(legacy bool) result {
		db, err := infra.CloneTestDatabase(t, "testdb")
		require.NoError(t, err)
		if legacy {
			require.NoError(t, runLegacy(ctx, logger, db, ch, blob, spec))
		} else {
			require.NoError(t, Run(ctx, logger, db, ch, blob, spec))
		}
		pg, err := demoseedtest.SnapshotPostgres(ctx, db)
		require.NoError(t, err)
		chState, err := demoseedtest.SnapshotClickHouse(ctx, ch, spec.OrgID, []string{spec.ProjectID()})
		require.NoError(t, err)
		counts := map[string]uint64{}
		for table, st := range chState {
			counts[table] = st.DemoCount
		}
		bd, err := demoseedtest.ClickHouseDemoBreakdown(ctx, ch, spec.OrgID, spec.ProjectID())
		require.NoError(t, err)
		return result{pg: pg, chCounts: counts, breakdown: bd}
	}

	old1 := run(true)
	old2 := run(true)
	got := run(false)

	var failures []string
	for table, oldRows := range old1.pg {
		volatile := volatileColumns(oldRows, old2.pg[table])
		want := maskRows(oldRows, volatile)
		have := maskRows(got.pg[table], volatile)
		if missing, extra := multisetDiff(want, have); len(missing)+len(extra) > 0 {
			failures = append(failures, fmt.Sprintf("postgres %s (volatile %v): %d rows only in legacy, %d only in Go\n  legacy-only: %s\n  go-only:     %s",
				table, volatile, len(missing), len(extra), sample(missing), sample(extra)))
		}
	}
	for table, n := range old1.chCounts {
		if got.chCounts[table] != n {
			failures = append(failures, fmt.Sprintf("clickhouse %s: legacy %d rows, Go %d", table, n, got.chCounts[table]))
		}
	}
	for key, n := range old1.breakdown {
		if got.breakdown[key] != n {
			failures = append(failures, fmt.Sprintf("clickhouse breakdown %s: legacy %d, Go %d", key, n, got.breakdown[key]))
		}
	}
	for key, n := range got.breakdown {
		if _, ok := old1.breakdown[key]; !ok {
			failures = append(failures, fmt.Sprintf("clickhouse breakdown %s: only in Go (%d)", key, n))
		}
	}
	sort.Strings(failures)
	require.Empty(t, failures, strings.Join(failures, "\n"))
}

// volatileColumns names the columns whose value multiset differs between two
// runs of the same seed.
func volatileColumns(a, b map[string]int) []string {
	values := func(rows map[string]int) map[string][]string {
		out := map[string][]string{}
		for raw, n := range rows {
			var row map[string]any
			if err := json.Unmarshal([]byte(raw), &row); err != nil {
				panic(err)
			}
			for col, v := range row {
				enc, _ := json.Marshal(v)
				for range n {
					out[col] = append(out[col], string(enc))
				}
			}
		}
		for col := range out {
			sort.Strings(out[col])
		}
		return out
	}
	va, vb := values(a), values(b)
	var volatile []string
	for col, xs := range va {
		if strings.Join(xs, "\x00") != strings.Join(vb[col], "\x00") {
			volatile = append(volatile, col)
		}
	}
	sort.Strings(volatile)
	return volatile
}

func maskRows(rows map[string]int, volatile []string) map[string]int {
	out := map[string]int{}
	for raw, n := range rows {
		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			panic(err)
		}
		for _, col := range volatile {
			delete(row, col)
		}
		enc, _ := json.Marshal(row)
		out[string(enc)] += n
	}
	return out
}

func multisetDiff(want, have map[string]int) (missing, extra []string) {
	for row, n := range want {
		for range n - have[row] {
			missing = append(missing, row)
		}
	}
	for row, n := range have {
		for range n - want[row] {
			extra = append(extra, row)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func sample(rows []string) string {
	if len(rows) > 3 {
		rows = rows[:3]
	}
	return strings.Join(rows, "\n               ")
}
