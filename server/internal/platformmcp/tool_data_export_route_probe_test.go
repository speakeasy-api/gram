package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
)

func TestDataExportTestSignalNamesCoverEverySignal(t *testing.T) {
	t.Parallel()

	for _, source := range dataexports.Sources() {
		for _, signal := range source.Signals {
			require.NotEmpty(t, dataExportTestSignalNames[signal], "signal %q of %q has no name", signal, source.Name)
		}
	}
	require.Equal(t, "one test event on unknown", dataExportTestSignalName("unknown"))
}

func TestListClauses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		clauses []string
		want    string
	}{
		{clauses: []string{"A, which sends x"}, want: "A, which sends x"},
		{clauses: []string{"A, which sends x", "B, which sends y"}, want: "A, which sends x and B, which sends y"},
		{clauses: []string{"A, which sends x", "B, which sends y", "C, which sends z"}, want: "A, which sends x; B, which sends y; and C, which sends z"},
	} {
		require.Equal(t, tc.want, listClauses(tc.clauses))
	}
}
