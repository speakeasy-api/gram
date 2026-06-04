package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Effective-tag derivation and ?tags= filtering are tested in the shared
// toolfilter package. These cover only the runtime's request-level tag parsing.

func TestParseTagsFilter_AbsentReturnsNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, parseTagsFilter(""))
}

func TestParseTagsFilter_OnlyDelimitersReturnsNil(t *testing.T) {
	t.Parallel()
	// A value made up entirely of empty segments must not become a one-element
	// [""] slice, which would otherwise filter every tool out.
	require.Nil(t, parseTagsFilter(" , , "))
}

func TestParseTagsFilter_TrimsAndDropsEmptySegments(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"a", "b"}, parseTagsFilter("a,,  b "))
}

func TestParseTagsFilter_Deduplicates(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"a", "b"}, parseTagsFilter("a,b,a,b"))
}

func TestParseTagsFilter_Single(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"billing"}, parseTagsFilter("billing"))
}
