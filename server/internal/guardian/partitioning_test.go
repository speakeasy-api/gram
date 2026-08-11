package guardian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartitionString_Injective(t *testing.T) {
	t.Parallel()

	// Keys chosen to collide under naive ':'-joined encodings: segments
	// containing the separator, segment boundaries shifted between the
	// partition and subset sections, and length-prefix look-alikes.
	keys := []Partition{
		NewPartition("ns", "a:b"),
		NewPartition("ns", "a", "b"),
		NewPartition("ns", "a_b"),
		NewPartition("ns", "a").WithSubset("b"),
		NewPartition("ns").WithSubset("a", "b"),
		NewPartition("ns").WithSubset("a:b"),
		NewPartition("ns:1:a"),
		NewPartition("ns", "1:a"),
		NewPartition("ns", "::1", "8443"),
		NewPartition("ns", "::1:8443"),
		NewPartition("ns", "a|b"),
		NewPartition("ns", "a").WithSubset("b", "c"),
		NewPartition("ns", "a", "b").WithSubset("c"),
	}

	seen := make(map[string]Partition, len(keys))
	for _, key := range keys {
		id := key.String()
		prev, dup := seen[id]
		require.False(t, dup, "%#v and %#v collide on %q", key, prev, id)
		seen[id] = key
	}
}

func TestPartition_WithSubsetDoesNotAliasSiblings(t *testing.T) {
	t.Parallel()

	base := NewPartition("ns", "host").WithSubset("org-a")

	left := base.WithSubset("left")
	right := base.WithSubset("right")

	require.Equal(t, "org-a:left", left.Subset())
	require.Equal(t, "org-a:right", right.Subset())
	require.Equal(t, "org-a", base.Subset())
}
