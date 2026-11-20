package aitargets

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestWireVersionStaysInDeviceRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prefix uint32
		want   int32
	}{
		{name: "zero", prefix: 0, want: 1},
		{name: "sign bit only", prefix: 1 << 31, want: 1},
		{name: "one", prefix: 1, want: 1},
		{name: "sign bit and one", prefix: 1<<31 | 1, want: 1},
		{name: "max int32", prefix: math.MaxInt32, want: math.MaxInt32},
		{name: "all ones", prefix: math.MaxUint32, want: math.MaxInt32},
		{name: "ordinary", prefix: 0x1234_5678, want: 0x1234_5678},
		{name: "ordinary with sign bit", prefix: 0x9234_5678, want: 0x1234_5678},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, wireVersion(tc.prefix))
		})
	}
}

func TestDefaultsAreSharedPerSnapshotButReturnedAsCopies(t *testing.T) {
	t.Parallel()
	catalog := aivendors.NewSnapshot(aivendors.Products())
	first := Defaults(catalog)
	require.NotEmpty(t, first)
	want := Defaults(catalog)
	first[0].DisplayName = "Mutated"
	first[0].Signatures.Binaries = append(first[0].Signatures.Binaries, "mutated")
	require.Equal(t, want, Defaults(catalog))
	require.Equal(t, ListVersion(want), NewOrganizationList(catalog, nil).Snapshot.ListVersion)
}
