package toolsets

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTopLevelToolUrns_KeepsAllowedAndDedupes(t *testing.T) {
	t.Parallel()

	got, err := normalizeTopLevelToolUrns(
		[]string{" tools:http:a:one ", "tools:http:a:two", "tools:http:a:one"},
		[]string{"tools:http:a:one", "tools:http:a:two", "tools:http:a:three"},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"tools:http:a:one", "tools:http:a:two"}, got)
}

func TestNormalizeTopLevelToolUrns_RejectsUnknown(t *testing.T) {
	t.Parallel()

	_, err := normalizeTopLevelToolUrns(
		[]string{"tools:http:a:missing"},
		[]string{"tools:http:a:one"},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in this toolset")
}

func TestNormalizeTopLevelToolUrns_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := normalizeTopLevelToolUrns([]string{" "}, []string{"tools:http:a:one"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestIntersectToolUrns_DropsRemoved(t *testing.T) {
	t.Parallel()

	got := intersectToolUrns(
		[]string{"tools:http:a:one", "tools:http:a:gone", "tools:http:a:one"},
		[]string{"tools:http:a:one", "tools:http:a:two"},
	)
	require.Equal(t, []string{"tools:http:a:one"}, got)
}
