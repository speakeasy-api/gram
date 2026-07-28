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
