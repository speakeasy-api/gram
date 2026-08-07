package mcp

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func inventoryFixture(names ...string) *consentToolInventory {
	tools := make([]consentInventoryTool, 0, len(names))
	for _, name := range names {
		tools = append(tools, consentInventoryTool{Name: name, Annotations: []string{}})
	}
	return inventoryFixtureWithTools(tools)
}

func inventoryFixtureWithTools(tools []consentInventoryTool) *consentToolInventory {
	return &consentToolInventory{
		StateID:        "state-1",
		Attempt:        "11111111-1111-4111-8111-111111111111",
		Resource:       "toolset:00000000-0000-0000-0000-000000000001",
		Tools:          tools,
		ExpectedCursor: "",
		Complete:       true,
		McpSessionID:   "",
	}
}

func inventoryServiceForTest(t *testing.T) *Service {
	t.Helper()
	return &Service{
		logger: testenv.NewLogger(t),
		consentToolInventoryCache: cache.NewTypedObjectCache[consentToolInventory](
			testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone,
		),
	}
}

func TestChosenToolSelection_MissingFilteringWithNoFieldsReadsOff(t *testing.T) {
	t.Parallel()

	sel, err := chosenToolSelection(url.Values{}, inventoryFixture("a"))
	require.NoError(t, err)
	require.Nil(t, sel)
}

func TestChosenToolSelection_MissingFilteringWithSelectionFieldsRejects(t *testing.T) {
	t.Parallel()

	form := url.Values{"tools": {"a"}}
	_, err := chosenToolSelection(form, inventoryFixture("a"))
	require.Error(t, err)
}

func TestChosenToolSelection_OffWithSelectionFieldsRejects(t *testing.T) {
	t.Parallel()

	form := url.Values{"tool_filtering": {"off"}, "tools": {"a"}}
	_, err := chosenToolSelection(form, inventoryFixture("a"))
	require.Error(t, err)
}

func TestChosenToolSelection_OnWithNoGrantsIsZeroTools(t *testing.T) {
	t.Parallel()

	sel, err := chosenToolSelection(url.Values{"tool_filtering": {"on"}}, inventoryFixture("a"))
	require.NoError(t, err)
	require.NotNil(t, sel)
	require.Empty(t, sel.Allow)
	require.False(t, sel.AllowsName("a"))
	require.Equal(t, "toolset:00000000-0000-0000-0000-000000000001", sel.Resource)
	require.NotEqual(t, uuid.Nil, sel.GrantID)
}

func TestChosenToolSelection_OnWithoutInventoryRejects(t *testing.T) {
	t.Parallel()

	_, err := chosenToolSelection(url.Values{"tool_filtering": {"on"}}, nil)
	require.Error(t, err)
}

func TestChosenToolSelection_ManualPicksIntersectAndCanonicalize(t *testing.T) {
	t.Parallel()

	form := url.Values{
		"tool_filtering": {"on"},
		"tools":          {"a", "crafted", "b"},
	}
	sel, err := chosenToolSelection(form, inventoryFixture("a", "b"))
	require.NoError(t, err)
	require.True(t, sel.AllowsName("a"))
	require.True(t, sel.AllowsName("b"))
	require.False(t, sel.AllowsName("crafted"))
	require.Len(t, sel.Allow, 2)
}

func TestChosenToolSelection_DuplicateManualPickRejects(t *testing.T) {
	t.Parallel()

	form := url.Values{
		"tool_filtering": {"on"},
		"tools":          {"a", "a"},
	}
	_, err := chosenToolSelection(form, inventoryFixture("a"))
	require.Error(t, err)
}

func TestChosenToolSelection_SnapshotAnnotationExpandsServerSide(t *testing.T) {
	t.Parallel()

	inventory := inventoryFixtureWithTools([]consentInventoryTool{
		{Name: "get_a", Annotations: []string{"read_only"}},
		{Name: "get_b", Annotations: []string{"read_only"}},
		{Name: "del_c", Annotations: []string{"destructive"}},
	})
	form := url.Values{
		"tool_filtering":   {"on"},
		"tool_annotations": {"read_only"},
	}
	sel, err := chosenToolSelection(form, inventory)
	require.NoError(t, err)
	require.Len(t, sel.Allow, 1)
	entry := sel.Allow[0]
	require.Equal(t, toolfilter.AllowTypeAnnotation, entry.Type)
	require.Equal(t, "read_only", entry.Name)
	require.Equal(t, toolfilter.AnnotationModeSnapshot, *entry.Mode)
	require.Equal(t, []string{"get_a", "get_b"}, *entry.Tools)
	require.True(t, sel.AllowsName("get_a"))
	require.False(t, sel.AllowsName("del_c"))
	require.Empty(t, sel.LiveAnnotations())
}

func TestChosenToolSelection_LiveAnnotationCarriesNoExpansion(t *testing.T) {
	t.Parallel()

	inventory := inventoryFixtureWithTools([]consentInventoryTool{
		{Name: "get_a", Annotations: []string{"read_only"}},
	})
	form := url.Values{
		"tool_filtering":        {"on"},
		"tool_annotations_live": {"read_only"},
	}
	sel, err := chosenToolSelection(form, inventory)
	require.NoError(t, err)
	require.Len(t, sel.Allow, 1)
	require.Equal(t, toolfilter.AnnotationModeLive, *sel.Allow[0].Mode)
	require.Nil(t, sel.Allow[0].Tools)
	require.False(t, sel.AllowsName("get_a"), "live grants contribute no frozen names")
	require.Equal(t, []string{"read_only"}, sel.LiveAnnotations())
}

func TestChosenToolSelection_GrantCoveredPickCanonicalizedAway(t *testing.T) {
	t.Parallel()

	inventory := inventoryFixtureWithTools([]consentInventoryTool{
		{Name: "get_a", Annotations: []string{"read_only"}},
		{Name: "plain", Annotations: []string{}},
	})
	form := url.Values{
		"tool_filtering":   {"on"},
		"tool_annotations": {"read_only"},
		"tools":            {"get_a", "plain"},
	}
	sel, err := chosenToolSelection(form, inventory)
	require.NoError(t, err)
	require.Len(t, sel.Allow, 2, "annotation entry plus the one uncovered pick")
	require.True(t, sel.AllowsName("plain"))
}

func TestChosenToolSelection_ZeroMatchAnnotationRejects(t *testing.T) {
	t.Parallel()

	form := url.Values{
		"tool_filtering":   {"on"},
		"tool_annotations": {"destructive"},
	}
	_, err := chosenToolSelection(form, inventoryFixture("plain"))
	require.Error(t, err)
}

func TestChosenToolSelection_UnknownAndDuplicateAnnotationsReject(t *testing.T) {
	t.Parallel()

	inventory := inventoryFixtureWithTools([]consentInventoryTool{
		{Name: "get_a", Annotations: []string{"read_only"}},
	})
	_, err := chosenToolSelection(url.Values{
		"tool_filtering":   {"on"},
		"tool_annotations": {"made_up"},
	}, inventory)
	require.Error(t, err)

	_, err = chosenToolSelection(url.Values{
		"tool_filtering":        {"on"},
		"tool_annotations":      {"read_only"},
		"tool_annotations_live": {"read_only"},
	}, inventory)
	require.Error(t, err, "one mode per annotation")
}

func TestChosenToolSelection_RejectsOverCountAndOverlongNames(t *testing.T) {
	t.Parallel()

	submitted := make([]string, consentToolNameLimit+1)
	for i := range submitted {
		submitted[i] = fmt.Sprintf("tool-%d", i)
	}
	_, err := chosenToolSelection(url.Values{
		"tool_filtering": {"on"},
		"tools":          submitted,
	}, inventoryFixture("tool-0"))
	require.Error(t, err)

	_, err = chosenToolSelection(url.Values{
		"tool_filtering": {"on"},
		"tools":          {strings.Repeat("x", consentInventoryMaxNameBytes+1)},
	}, inventoryFixture("a"))
	require.Error(t, err)
}

func TestConsentPrefillAttr_NilIsEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, consentPrefillAttr(nil))
}

func TestConsentPrefillAttr_SerializesGrantsAndPicks(t *testing.T) {
	t.Parallel()

	inventory := inventoryFixtureWithTools([]consentInventoryTool{
		{Name: "get_a", Annotations: []string{"read_only"}},
		{Name: "plain", Annotations: []string{}},
	})
	sel, err := chosenToolSelection(url.Values{
		"tool_filtering":        {"on"},
		"tool_annotations_live": {"read_only"},
		"tools":                 {"plain"},
	}, inventory)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"annotations":[{"name":"read_only","mode":"live"}],"tools":["plain"]}`,
		consentPrefillAttr(sel),
	)
}

func TestConsentHintsFromAnnotations_RoundTrip(t *testing.T) {
	t.Parallel()

	require.Nil(t, consentHintsFromAnnotations(nil))
	hints := consentHintsFromAnnotations([]string{"read_only", "open_world"})
	require.Equal(t, map[string]bool{"readOnlyHint": true, "openWorldHint": true}, hints)
}

func emptyDraft() consentToolInventory {
	draft := inventoryFixture()
	draft.Complete = false
	draft.Tools = []consentInventoryTool{}
	return *draft
}

func TestAppendConsentInventoryPage_SinglePageCompletesAndSorts(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	updated, err := s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", []consentInventoryTool{
		{Name: "zeta", Annotations: []string{"open_world", "read_only", "bogus"}},
		{Name: "alpha", Annotations: nil},
	}, "")
	require.NoError(t, err)
	require.True(t, updated.Complete)
	require.Equal(t, "alpha", updated.Tools[0].Name)
	require.Equal(t, []string{}, updated.Tools[0].Annotations)
	require.Equal(t, "zeta", updated.Tools[1].Name)
	require.Equal(t, []string{"read_only", "open_world"}, updated.Tools[1].Annotations)
}

func TestAppendConsentInventoryPage_MultiPageCursorChain(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	page1, err := s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", []consentInventoryTool{{Name: "a"}}, "next-1")
	require.NoError(t, err)
	require.False(t, page1.Complete)
	require.Equal(t, "next-1", page1.ExpectedCursor)

	page2, err := s.appendConsentInventoryPage(t.Context(), page1, "next-1", []consentInventoryTool{{Name: "b"}}, "")
	require.NoError(t, err)
	require.True(t, page2.Complete)
	require.Len(t, page2.Tools, 2)
}

func TestAppendConsentInventoryPage_OutOfOrderCursorRejected(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	_, err := s.appendConsentInventoryPage(t.Context(), emptyDraft(), "unexpected", nil, "")
	require.Error(t, err)
}

func TestAppendConsentInventoryPage_CompleteDraftRejectsMorePages(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	_, err := s.appendConsentInventoryPage(t.Context(), *inventoryFixture("a"), "", []consentInventoryTool{{Name: "b"}}, "")
	require.Error(t, err)
}

func TestAppendConsentInventoryPage_RejectsDuplicateAcrossPages(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	page1, err := s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", []consentInventoryTool{{Name: "a"}}, "next")
	require.NoError(t, err)
	_, err = s.appendConsentInventoryPage(t.Context(), page1, "next", []consentInventoryTool{{Name: "a"}}, "")
	require.Error(t, err)
}

func TestAppendConsentInventoryPage_RejectsBlankOverlongAndOverCap(t *testing.T) {
	t.Parallel()

	s := inventoryServiceForTest(t)
	_, err := s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", []consentInventoryTool{{Name: ""}}, "")
	require.Error(t, err)

	_, err = s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", []consentInventoryTool{
		{Name: strings.Repeat("x", consentInventoryMaxNameBytes+1)},
	}, "")
	require.Error(t, err)

	over := make([]consentInventoryTool, consentInventoryMaxTools+1)
	for i := range over {
		over[i] = consentInventoryTool{Name: strings.Repeat("t", 3) + string(rune('a'+i%26)) + strings.Repeat("x", i/26)}
	}
	_, err = s.appendConsentInventoryPage(t.Context(), emptyDraft(), "", over, "")
	require.Error(t, err)
}

func TestConsentRoleHiddenMeta_CapsNamesKeepsCount(t *testing.T) {
	t.Parallel()

	small := consentRoleHiddenMeta([]string{"b", "a"})
	require.Equal(t, map[string]any{"count": 2, "names": []string{"b", "a"}}, small)

	big := make([]string, consentRoleHiddenNamesCap+7)
	for i := range big {
		big[i] = fmt.Sprintf("tool_%03d", i)
	}
	capped := consentRoleHiddenMeta(big)
	require.Equal(t, consentRoleHiddenNamesCap+7, capped["count"])
	require.Len(t, capped["names"], consentRoleHiddenNamesCap)
}
