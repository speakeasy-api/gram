package partition_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

func TestParse_KnownPartitions(t *testing.T) {
	t.Parallel()

	for _, want := range partition.All() {
		t.Run(want.String(), func(t *testing.T) {
			t.Parallel()

			got, err := partition.Parse(want.String())
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestParse_RejectsUnknownNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "unknown", raw: "apac"},
		{name: "uppercase", raw: "US"},
		{name: "padded", raw: " us"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := partition.Parse(tc.raw)
			require.ErrorContains(t, err, "must be one of us, eu")
		})
	}
}

func TestDefault_IsUS(t *testing.T) {
	t.Parallel()

	require.Equal(t, partition.US, partition.Default)
}
