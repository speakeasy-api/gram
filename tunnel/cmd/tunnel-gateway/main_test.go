package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMaxSessionsDefaultsOnEmptyOrZero(t *testing.T) {
	empty, err := parseMaxSessions("")
	require.NoError(t, err)
	require.Zero(t, empty)

	zero, err := parseMaxSessions("0")
	require.NoError(t, err)
	require.Zero(t, zero)
}

func TestParseMaxSessionsAcceptsPositiveValue(t *testing.T) {
	maxSessions, err := parseMaxSessions("42")
	require.NoError(t, err)
	require.Equal(t, 42, maxSessions)
}

func TestParseMaxSessionsRejectsInvalidValue(t *testing.T) {
	_, err := parseMaxSessions("not-a-number")
	require.Error(t, err)

	_, err = parseMaxSessions("-1")
	require.Error(t, err)
}

func TestParsePartitionDefaultsToUSWhenUnset(t *testing.T) {
	got, err := parsePartition("", false)
	require.NoError(t, err)
	require.Equal(t, "us", got)
}

func TestParsePartitionAcceptsKnownPartitions(t *testing.T) {
	for _, want := range []string{"us", "eu"} {
		got, err := parsePartition(want, true)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestParsePartitionRejectsEmptyOrUnknown(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "empty", raw: "", wantErr: `unknown partition ""`},
		{name: "unknown", raw: "apac", wantErr: `unknown partition "apac"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePartition(tc.raw, true)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
