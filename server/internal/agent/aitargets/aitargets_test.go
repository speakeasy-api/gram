package aitargets_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
)

func TestTargetsAreWellFormed(t *testing.T) {
	t.Parallel()

	targets := aitargets.Targets()
	require.NotEmpty(t, targets)

	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		_, duplicate := seen[target.ID]
		assert.False(t, duplicate, "duplicate target id %q", target.ID)
		seen[target.ID] = struct{}{}

		assert.NotEmpty(t, target.ID, "target id must be set")
		assert.Equal(t, strings.ToLower(target.ID), target.ID, "target id %q must be lowercase", target.ID)
		assert.NotContains(t, target.ID, " ", "target id %q must not contain spaces", target.ID)
		assert.NotEmpty(t, target.DisplayName, "target %q must have a display name", target.ID)
		assert.Contains(t,
			[]aitargets.Category{aitargets.CategoryHarness, aitargets.CategoryLocalModel},
			target.Category,
			"target %q has unknown category %q", target.ID, target.Category,
		)
	}
}

func TestByIDResolvesEveryTarget(t *testing.T) {
	t.Parallel()

	for _, target := range aitargets.Targets() {
		resolved, ok := aitargets.ByID(target.ID)
		require.True(t, ok, "target %q must resolve", target.ID)
		assert.Equal(t, target, resolved)
	}

	_, ok := aitargets.ByID("definitely-not-a-target")
	assert.False(t, ok)
}

// TestTargetsReturnsACopy guards the accessor against callers mutating the
// package-level catalog.
func TestTargetsReturnsACopy(t *testing.T) {
	t.Parallel()

	first := aitargets.Targets()
	first[0].DisplayName = "mutated"

	fresh := aitargets.Targets()
	assert.NotEqual(t, "mutated", fresh[0].DisplayName)
}
