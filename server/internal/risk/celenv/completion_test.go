package celenv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func labels(items []CompletionItem) map[string]string {
	m := make(map[string]string, len(items))
	for _, it := range items {
		m[it.Label] = it.Category
	}
	return m
}

func TestComplete_TopLevelNames(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	c := eng.Complete("")
	require.Equal(t, "name", c.Context)
	l := labels(c.Items)
	require.Equal(t, "variable", l["content"])
	require.Equal(t, "variable", l["tool_calls"])
	require.Equal(t, "globalMacro", l["has"]) // global macro offered at name position
	require.NotContains(t, l, "exists")       // list macros are member-only
}

func TestComplete_FieldOffersMatchers(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	c := eng.Complete("content.")
	require.Equal(t, "member", c.Context)
	l := labels(c.Items)
	require.Equal(t, "matcher", l["matchRegex"])
	require.Equal(t, "matcher", l["get"])
	require.NotContains(t, l, "exists")
}

func TestComplete_ListOffersMacros(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	c := eng.Complete("tool_calls.")
	l := labels(c.Items)
	require.Equal(t, "macro", l["exists"])
	require.Equal(t, "macro", l["all"])
	require.NotContains(t, l, "matchRegex") // a list is not a field
}

func TestComplete_BoundToolOffersFields(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	// `t` is bound to a tool element; completing `t.` must offer the tool members,
	// resolved by type-checking `t` with the binding declared — not by a guess.
	c := eng.Complete("tool_calls.exists(t, t.")
	l := labels(c.Items)
	require.Equal(t, "field", l["name"])
	require.Equal(t, "field", l["args"])
	require.NotContains(t, l, "matchRegex")
}

func TestComplete_BoundToolFieldOffersMatchers(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	c := eng.Complete("tool_calls.exists(t, t.name.")
	l := labels(c.Items)
	require.Equal(t, "matcher", l["matchExact"])
	require.NotContains(t, l, "name") // a field, not the tool object
}

func TestComplete_GetChainStaysField(t *testing.T) {
	t.Parallel()
	eng, err := New()
	require.NoError(t, err)

	// get() returns a field, so the chain keeps offering matchers.
	c := eng.Complete(`content.get("a").`)
	l := labels(c.Items)
	require.Equal(t, "matcher", l["matchText"])
}
