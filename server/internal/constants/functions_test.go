package constants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsFunctionTimeoutSeconds(t *testing.T) {
	t.Parallel()

	require.False(t, IsFunctionTimeoutSeconds(29))
	require.True(t, IsFunctionTimeoutSeconds(30))
	require.True(t, IsFunctionTimeoutSeconds(60))
	require.True(t, IsFunctionTimeoutSeconds(600))
	require.False(t, IsFunctionTimeoutSeconds(601))
	require.False(t, IsFunctionTimeoutSeconds(0))
	require.False(t, IsFunctionTimeoutSeconds(-1))
}

func TestEffectiveFunctionTimeoutSeconds(t *testing.T) {
	t.Parallel()

	stored := func(v int32) *int32 { return &v }

	cases := []struct {
		name        string
		stored      *int32
		wantSeconds int
		wantDefault bool
	}{
		{name: "unset uses default", stored: nil, wantSeconds: 60, wantDefault: true},
		{name: "staff-set value applies", stored: stored(300), wantSeconds: 300, wantDefault: false},
		{name: "lower bound applies", stored: stored(30), wantSeconds: 30, wantDefault: false},
		{name: "upper bound applies", stored: stored(600), wantSeconds: 600, wantDefault: false},
		{name: "below range is ignored", stored: stored(5), wantSeconds: 60, wantDefault: true},
		{name: "above range is ignored", stored: stored(3600), wantSeconds: 60, wantDefault: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			seconds, isDefault := EffectiveFunctionTimeoutSeconds(tc.stored)
			require.Equal(t, tc.wantSeconds, seconds)
			require.Equal(t, tc.wantDefault, isDefault)
		})
	}
}
