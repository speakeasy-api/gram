package constants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsFunctionTimeoutSeconds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		seconds int
		want    bool
	}{
		{name: "below lower bound", seconds: 29, want: false},
		{name: "lower bound", seconds: 30, want: true},
		{name: "default", seconds: 60, want: true},
		{name: "upper bound", seconds: 600, want: true},
		{name: "above upper bound", seconds: 601, want: false},
		{name: "zero", seconds: 0, want: false},
		{name: "negative", seconds: -1, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsFunctionTimeoutSeconds(tc.seconds))
		})
	}
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
