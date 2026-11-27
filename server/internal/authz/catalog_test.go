package authz

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every scope the engine knows must be described, and nothing may be
// described that the engine does not know. ScopeRoot is the one exception:
// it is the engine's bypass, not a grant a person chooses.
func TestScopeCatalog_CoversEveryRegisteredScope(t *testing.T) {
	t.Parallel()

	catalogued := make(map[Scope]bool, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		require.False(t, catalogued[entry.Scope], "scope %q is catalogued twice", entry.Scope)
		catalogued[entry.Scope] = true
	}

	for _, scope := range RegisteredScopes() {
		if scope == ScopeRoot {
			require.False(t, catalogued[scope], "root must not be catalogued")
			continue
		}
		require.True(t, catalogued[scope], "registered scope %q has no catalogue entry", scope)
	}
	require.Len(t, scopeCatalog, len(RegisteredScopes())-1)
}

func TestScopeCatalog_EntriesAreComplete(t *testing.T) {
	t.Parallel()

	for _, entry := range scopeCatalog {
		require.NotEmpty(t, entry.Label, "%s: label", entry.Scope)
		require.NotEmpty(t, entry.Description, "%s: description", entry.Scope)
		require.True(t, strings.HasSuffix(entry.Description, "."), "%s: description must be a sentence", entry.Scope)
		require.Contains(t, ScopeCategories, entry.Category, "%s: category", entry.Scope)
		require.NotEmpty(t, entry.ResourceType, "%s: resource type", entry.Scope)

		owner, ok := ScopeGrantSource(entry.Scope)
		require.True(t, ok, "%s: grant source", entry.Scope)
		require.NotEmpty(t, owner)

		visibility, ok := ScopeVisibilityFor(entry.Scope)
		require.True(t, ok)
		if visibility == ScopeVisibilityInternal {
			require.True(t, strings.HasPrefix(entry.Label, "Exception: "), "%s: internal scopes are labelled as exceptions", entry.Scope)
			require.False(t, entry.Sensitive(), "%s: exception scopes withdraw access and are never sensitive", entry.Scope)
		} else {
			require.False(t, strings.HasPrefix(entry.Label, "Exception: "), "%s: visible scopes are not labelled as exceptions", entry.Scope)
		}
	}
}

// The sensitive set is a product decision (AIS-825): other members' private
// activity, and administration that can remove people or rewrite every role.
// Secrets and credentials are deliberately not in it.
func TestScopeCatalog_SensitiveSet(t *testing.T) {
	t.Parallel()

	require.Equal(t, []Scope{ScopeOrgAdmin, ScopeChannelsReadAll, ScopeChatRead, ScopeChatWrite, ScopeLogRead}, SensitiveScopes())

	reasons := []struct {
		scope Scope
		want  Sensitivity
	}{
		{scope: ScopeOrgAdmin, want: SensitivityDestructiveAdmin},
		{scope: ScopeChannelsReadAll, want: SensitivityConversations},
		{scope: ScopeChatRead, want: SensitivityConversations},
		{scope: ScopeChatWrite, want: SensitivityConversations},
		{scope: ScopeLogRead, want: SensitivityToolLogs},
	}
	for _, tc := range reasons {
		entry, ok := ScopeEntryFor(tc.scope)
		require.True(t, ok)
		require.Equal(t, tc.want, entry.Sensitivity, tc.scope)
	}
}

// A scope that includes a sensitive scope is itself sensitive, or the warning
// is lost when the broader scope is granted instead.
func TestScopeCatalog_SensitivityFollowsImplication(t *testing.T) {
	t.Parallel()

	for _, entry := range scopeCatalog {
		for _, sub := range entry.SubScopes() {
			subEntry, ok := ScopeEntryFor(Scope(sub))
			require.True(t, ok, "%s includes unknown %s", entry.Scope, sub)
			if subEntry.Sensitive() {
				require.True(t, entry.Sensitive(), "%s includes sensitive %s but is not sensitive itself", entry.Scope, sub)
			}
		}
	}
}

func TestScopeCatalog_SubScopesMatchExpansions(t *testing.T) {
	t.Parallel()

	admin, ok := ScopeEntryFor(ScopeOrgAdmin)
	require.True(t, ok)
	require.Equal(t, []string{"org:device_agent_sync", "org:hooks_ingest", "org:read"}, admin.SubScopes())

	write, ok := ScopeEntryFor(ScopeMCPWrite)
	require.True(t, ok)
	require.Equal(t, []string{"mcp:read"}, write.SubScopes(), "mcp:write does not imply mcp:connect")

	chatWrite, ok := ScopeEntryFor(ScopeChatWrite)
	require.True(t, ok)
	require.Equal(t, []string{"chat:read"}, chatWrite.SubScopes())

	logRead, ok := ScopeEntryFor(ScopeLogRead)
	require.True(t, ok)
	require.Empty(t, logRead.SubScopes())
}

// The API design builds its scope enums from these, so they must cover the
// whole catalogue and list each exclusion scope once.
func TestScopeNames_FollowTheCatalogue(t *testing.T) {
	t.Parallel()

	names := ScopeNames()
	require.Len(t, names, len(scopeCatalog))
	for i, entry := range scopeCatalog {
		require.Equal(t, string(entry.Scope), names[i])
	}

	exclusions := ExclusionScopeNames()
	sorted := slices.Clone(exclusions)
	slices.Sort(sorted)
	require.Len(t, slices.Compact(sorted), len(exclusions), "each exclusion scope is listed once")
	for _, entry := range scopeCatalog {
		exclusion, ok := ExclusionScopeFor(entry.Scope)
		if ok {
			require.Contains(t, exclusions, string(exclusion), "%s: exclusion scope missing", entry.Scope)
		}
	}
	require.Equal(t, exclusions[0], string(ScopeOrgBlockedRead), "exclusions follow the catalogue order")
	require.Contains(t, exclusions, string(ScopeRiskPolicyBypass))
}

func TestScopeCatalog_IsACopy(t *testing.T) {
	t.Parallel()

	first := ScopeCatalog()
	first[0].Label = "changed"
	second := ScopeCatalog()
	require.NotEqual(t, "changed", second[0].Label)
}

// The catalogue is grouped by category in ScopeCategories order, so a
// consumer that groups it gets one contiguous section per category.
func TestScopeCatalog_IsGroupedByCategory(t *testing.T) {
	t.Parallel()

	var seen []ScopeCategory
	for _, entry := range scopeCatalog {
		if len(seen) == 0 || seen[len(seen)-1] != entry.Category {
			require.NotContains(t, seen, entry.Category, "%s: category %q is split across the catalogue", entry.Scope, entry.Category)
			seen = append(seen, entry.Category)
		}
	}
	require.Equal(t, ScopeCategories, seen)
}

func TestScopeCategories_AreDistinct(t *testing.T) {
	t.Parallel()

	sorted := slices.Clone(ScopeCategories)
	slices.Sort(sorted)
	require.Len(t, slices.Compact(sorted), len(ScopeCategories))
}

// The scope table in docs/rbac.md is a rendering of the catalogue. This test
// fails when the two drift; `go test ./internal/authz -run DocTable -update-docs`
// rewrites the table from the catalogue.
func TestScopeCatalog_DocTableIsCurrent(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "docs", "rbac.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := string(content)

	begin := strings.Index(doc, scopeTableBegin)
	end := strings.Index(doc, scopeTableEnd)
	require.True(t, begin >= 0 && end > begin, "docs/rbac.md lacks the %s / %s markers", scopeTableBegin, scopeTableEnd)
	begin += len(scopeTableBegin)

	// Blank lines on both sides are what the Markdown formatter keeps.
	want := "\n\n" + renderScopeTable(t, ScopeCatalog()) + "\n\n"
	if *updateDocs {
		require.NoError(t, os.WriteFile(path, []byte(doc[:begin]+want+doc[end:]), 0o600))
		return
	}
	require.Equal(t, want, doc[begin:end], "the scope table in docs/rbac.md is stale; run `go test ./internal/authz -run DocTable -update-docs` from server/")
}

// Every task scope is one a person can be granted: catalogued, visible
// and not implied by another scope in the same task.
func TestTasks_StayInsideTheCatalogue(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, task := range Tasks() {
		require.NotEmpty(t, task.Slug)
		require.False(t, seen[task.Slug], "task %q listed twice", task.Slug)
		seen[task.Slug] = true
		require.NotEmpty(t, task.Label, task.Slug)
		require.False(t, strings.HasSuffix(task.Label, "."), "%s: a label is not a sentence", task.Slug)
		require.GreaterOrEqual(t, len(task.Scopes), 2, "%s: a task with one scope is just that scope", task.Slug)

		for _, scope := range task.Scopes {
			entry, ok := ScopeEntryFor(scope)
			require.True(t, ok, "%s: %s is not catalogued", task.Slug, scope)
			visibility, ok := ScopeVisibilityFor(scope)
			require.True(t, ok)
			require.Equal(t, ScopeVisibilityUserVisible, visibility, "%s: %s is not grantable by a person", task.Slug, entry.Scope)

			for _, other := range task.Scopes {
				if other == scope {
					continue
				}
				require.NotContains(t, ScopeImplicationClosure(other), scope,
					"%s: %s already includes %s", task.Slug, other, scope)
			}
		}
	}
}

func TestTasks_IsACopy(t *testing.T) {
	t.Parallel()

	first := Tasks()
	first[0].Scopes[0] = ScopeRoot
	first[0].Label = "changed"
	second := Tasks()
	require.NotEqual(t, ScopeRoot, second[0].Scopes[0])
	require.NotEqual(t, "changed", second[0].Label)
}

func TestTasksNeeding(t *testing.T) {
	t.Parallel()

	slugs := func(tasks []ScopeTask) []string {
		out := make([]string, 0, len(tasks))
		for _, task := range tasks {
			out = append(out, task.Slug)
		}
		slices.Sort(out)
		return out
	}

	require.Equal(t, []string{"agent_setup", "mcp_maintenance"}, slugs(TasksNeeding(ScopeEnvironmentRead)))
	require.Equal(t, []string{"session_review"}, slugs(TasksNeeding(ScopeLogRead)))
	require.Empty(t, TasksNeeding(ScopeOrgAdmin))
}

var updateDocs = flag.Bool("update-docs", false, "rewrite the generated scope table in docs/rbac.md")

const (
	scopeTableBegin = "<!-- scope-table:begin -->"
	scopeTableEnd   = "<!-- scope-table:end -->"
)

// renderScopeTable writes the catalogue as a Markdown table in the layout the
// repository's formatter produces, so the committed docs pass its check. A
// catalogue entry without a visibility fails the test rather than being
// documented under a default.
func renderScopeTable(t *testing.T, entries []ScopeEntry) string {
	t.Helper()

	header := []string{"Scope", "Label", "Category", "Visibility", "Sensitive", "Includes", "Description"}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		visibility, ok := ScopeVisibilityFor(entry.Scope)
		require.True(t, ok, "%s: no visibility in the catalogue", entry.Scope)
		sensitive := "no"
		if entry.Sensitive() {
			sensitive = string(entry.Sensitivity)
		}
		includes := "—"
		if subs := entry.SubScopes(); len(subs) > 0 {
			quoted := make([]string, 0, len(subs))
			for _, sub := range subs {
				quoted = append(quoted, "`"+sub+"`")
			}
			includes = strings.Join(quoted, ", ")
		}
		rows = append(rows, []string{
			"`" + string(entry.Scope) + "`",
			entry.Label,
			string(entry.Category),
			visibility,
			sensitive,
			includes,
			strings.ReplaceAll(entry.Description, "|", "\\|"),
		})
	}

	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = max(widths[i], displayWidth(cell))
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], displayWidth(cell))
		}
	}

	var out strings.Builder
	writeRow := func(cells []string) {
		out.WriteString("|")
		for i, cell := range cells {
			fmt.Fprintf(&out, " %s%s |", cell, strings.Repeat(" ", widths[i]-displayWidth(cell)))
		}
		out.WriteString("\n")
	}
	writeRow(header)
	out.WriteString("|")
	for _, width := range widths {
		fmt.Fprintf(&out, " %s |", strings.Repeat("-", width))
	}
	out.WriteString("\n")
	for _, row := range rows {
		writeRow(row)
	}
	return strings.TrimRight(out.String(), "\n")
}

// displayWidth counts characters, not bytes, so an em dash pads like a letter.
func displayWidth(s string) int {
	return len([]rune(s))
}
