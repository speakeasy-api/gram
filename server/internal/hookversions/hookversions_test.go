package hookversions

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
)

func TestPolicyClassify(t *testing.T) {
	t.Parallel()

	policy := Policy{Latest: semver.MustParse("0.3.46"), Minimum: semver.MustParse("0.3.0")}
	tests := []struct {
		name       string
		version    string
		generation string
		want       Status
	}{
		{name: "latest", version: "0.3.46", generation: "ingest_binary", want: StatusCurrent},
		{name: "newer than latest", version: "0.4.0", generation: "ingest_binary", want: StatusCurrent},
		{name: "v prefix", version: "v0.3.46", generation: "ingest_binary", want: StatusCurrent},
		{name: "behind", version: "0.3.20", generation: "ingest_binary", want: StatusBehind},
		{name: "at minimum", version: "0.3.0", generation: "ingest_binary", want: StatusBehind},
		{name: "below minimum", version: "0.2.9", generation: "ingest_binary", want: StatusUnsupported},
		{name: "legacy keyed", version: "", generation: "legacy_keyed", want: StatusUnsupported},
		{name: "legacy unkeyed with version", version: "0.3.46", generation: "legacy_unkeyed", want: StatusUnsupported},
		{name: "script without version", version: "", generation: "ingest_script", want: StatusUnknown},
		{name: "unparseable", version: "dev", generation: "ingest_binary", want: StatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, policy.Classify(tt.version, tt.generation))
		})
	}
}

func TestPolicyClassifyWithoutMinimum(t *testing.T) {
	t.Parallel()

	policy := Policy{Latest: semver.MustParse("0.3.46"), Minimum: nil}
	require.Equal(t, StatusBehind, policy.Classify("0.1.1", "ingest_binary"))
	require.Equal(t, StatusUnknown, Policy{Latest: nil, Minimum: nil}.Classify("0.3.46", "ingest_binary"))
}
