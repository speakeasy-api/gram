package worktype

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)?$`)

func TestAllReturnsCopy(t *testing.T) {
	t.Parallel()

	first := All()
	require.NotEmpty(t, first)

	first[0].DisplayName = "mutated"
	require.NotEqual(t, "mutated", All()[0].DisplayName, "All must return a copy of the registry")
}

func TestKeysAreWellFormed(t *testing.T) {
	t.Parallel()

	for _, c := range All() {
		require.Regexp(t, keyPattern, string(c.Key), "key %q must be snake_case with at most one dot", c.Key)
	}
}

func TestChildKeysAreNamespacedUnderParent(t *testing.T) {
	t.Parallel()

	for _, c := range All() {
		if c.Parent == "" {
			require.NotContains(t, string(c.Key), ".", "top-level key %q must not be dotted", c.Key)
			continue
		}
		require.True(t, strings.HasPrefix(string(c.Key), string(c.Parent)+"."),
			"child key %q must be prefixed with its parent key %q", c.Key, c.Parent)
	}
}

func TestParentsExistAndAreTopLevel(t *testing.T) {
	t.Parallel()

	for _, c := range All() {
		if c.Parent == "" {
			continue
		}
		parent, ok := Get(c.Parent)
		require.True(t, ok, "parent %q of %q must exist", c.Parent, c.Key)
		require.Empty(t, parent.Parent, "hierarchy is two levels: parent %q of %q must be top-level", c.Parent, c.Key)
	}
}

func TestDisplayNamesNonEmpty(t *testing.T) {
	t.Parallel()

	for _, c := range All() {
		require.NotEmpty(t, c.DisplayName, "category %q must have a display name", c.Key)
	}
}

func TestJudgeGuidanceOnClassifiableOnly(t *testing.T) {
	t.Parallel()

	classifiable := make(map[Key]bool)
	for _, c := range Classifiable() {
		classifiable[c.Key] = true
	}

	for _, c := range All() {
		if classifiable[c.Key] {
			require.NotEmpty(t, c.JudgeGuidance, "classifiable category %q must have judge guidance", c.Key)
		} else {
			require.Empty(t, c.JudgeGuidance, "parent category %q must not have judge guidance", c.Key)
		}
	}
}

func TestClassifiableExcludesParentsAndKeepsLeaves(t *testing.T) {
	t.Parallel()

	classifiable := make(map[Key]bool)
	for _, c := range Classifiable() {
		classifiable[c.Key] = true
	}

	parents := make(map[Key]bool)
	for _, c := range All() {
		if c.Parent != "" {
			parents[c.Parent] = true
		}
	}

	for _, c := range All() {
		if parents[c.Key] {
			require.False(t, classifiable[c.Key], "parent %q must not be classifiable", c.Key)
		} else {
			require.True(t, classifiable[c.Key], "leaf %q must be classifiable", c.Key)
		}
	}

	require.True(t, classifiable[KeyPersonal], "personal must be directly classifiable")
	require.True(t, classifiable[KeyOther], "other must be directly classifiable")
}

func TestGet(t *testing.T) {
	t.Parallel()

	c, ok := Get(KeyEngineeringCodeReview)
	require.True(t, ok)
	require.Equal(t, KeyEngineeringCodeReview, c.Key)
	require.Equal(t, KeyEngineering, c.Parent)
	require.Equal(t, "Code Review", c.DisplayName)

	_, ok = Get("nonsense")
	require.False(t, ok)
}

func TestTopLevel(t *testing.T) {
	t.Parallel()

	top, ok := TopLevel(KeyEngineeringBugFixing)
	require.True(t, ok)
	require.Equal(t, KeyEngineering, top)

	top, ok = TopLevel(KeyPersonal)
	require.True(t, ok)
	require.Equal(t, KeyPersonal, top)

	_, ok = TopLevel("nonsense")
	require.False(t, ok)
}

func TestVersionIsPositive(t *testing.T) {
	t.Parallel()

	require.Positive(t, Version)
}
