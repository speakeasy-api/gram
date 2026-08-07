package catalog_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sqlclint/catalog"
)

// headings each Kind's body must contain, in this order. Keeping the sequence
// uniform is what lets the documents read as one reference rather than as
// eighteen independently authored pages.
var requiredHeadings = map[catalog.Kind][]string{
	catalog.KindDiagnostic: {
		"## What it checks",
		"## Why it matters",
		"## How to fix",
		"## Examples",
		"## Exemptions",
	},
	catalog.KindExemption: {
		"## When to use it",
		"## Why it is safe",
		"## Evidence required",
		"## Example",
		"## When not to use it",
	},
}

func TestLoadSucceeds(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)
	require.NotEmpty(t, c.All())
}

// Every diagnostic the engine can emit must be explainable, otherwise a
// diagnostic could cite a rule id that `sqlclint rule` cannot render.
func TestEveryDiagnosticIDHasADocument(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	for _, id := range catalog.DiagnosticIDs {
		rule, ok := c.Lookup(id)
		require.Truef(t, ok, "diagnostic %q has no document", id)
		require.Equalf(t, catalog.KindDiagnostic, rule.Kind, "%q is documented as a %s", id, rule.Kind)
	}
}

// The reverse direction: a diagnostic document that the engine never emits is
// dead documentation promising a check that does not run.
func TestEveryDiagnosticDocumentIsReferenced(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	referenced := make(map[string]bool, len(catalog.DiagnosticIDs))
	for _, id := range catalog.DiagnosticIDs {
		referenced[id] = true
	}

	for _, rule := range c.ByKind(catalog.KindDiagnostic) {
		require.Truef(t, referenced[rule.ID],
			"diagnostic document %q is not in catalog.DiagnosticIDs, so nothing emits it", rule.ID)
	}
}

func TestBodiesFollowTheHeadingSequenceForTheirKind(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	for _, rule := range c.All() {
		want := requiredHeadings[rule.Kind]
		require.NotEmptyf(t, want, "no heading sequence defined for kind %q", rule.Kind)

		at := 0
		for _, heading := range want {
			idx := strings.Index(rule.Body[at:], heading+"\n")
			require.GreaterOrEqualf(t, idx, 0,
				"%q is missing %q, or has it out of order", rule.ID, heading)
			at += idx + len(heading)
		}
	}
}

// Diagnostics must show both sides of the check, so a reader can see the shape
// being rejected next to the shape that fixes it.
func TestDiagnosticsShowAViolationAndACompliantExample(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	for _, rule := range c.ByKind(catalog.KindDiagnostic) {
		violation := strings.Index(rule.Body, "### Violation\n")
		compliant := strings.Index(rule.Body, "### Compliant\n")

		require.GreaterOrEqualf(t, violation, 0, "%q has no ### Violation example", rule.ID)
		require.GreaterOrEqualf(t, compliant, 0, "%q has no ### Compliant example", rule.ID)
		require.Lessf(t, violation, compliant, "%q shows the fix before the problem", rule.ID)
	}
}

func TestSummariesAreSingleLineSentences(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	for _, rule := range c.All() {
		require.NotContainsf(t, rule.Summary, "\n", "%q has a multi-line summary", rule.ID)
		require.LessOrEqualf(t, len(rule.Summary), 120, "%q has a summary too long for the rules table", rule.ID)
	}
}

// silenced_by is validated during Load; this pins the specific expectation that
// the scope diagnostics are silenceable and the meta ones are not.
func TestOnlyScopeDiagnosticsAreSilenceable(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	silenceable := map[string]bool{
		catalog.MissingTenantScope:  true,
		catalog.WrongTenantColumn:   true,
		catalog.NullableTenantParam: true,
	}

	for _, rule := range c.ByKind(catalog.KindDiagnostic) {
		if silenceable[rule.ID] {
			require.NotEmptyf(t, rule.SilencedBy, "%q should be silenceable by some exemption", rule.ID)
			continue
		}
		require.Emptyf(t, rule.SilencedBy,
			"%q is a structural diagnostic and must not be silenceable by an annotation", rule.ID)
	}
}

// missing-tenant-scope is the rule an annotation usually answers, so every
// category must be a valid response to it.
func TestEveryExemptionCanSilenceMissingTenantScope(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	rule, ok := c.Lookup(catalog.MissingTenantScope)
	require.True(t, ok)

	require.ElementsMatch(t, c.IDs(catalog.KindExemption), rule.SilencedBy)
}

func TestIsExemptionRejectsDiagnosticsAndUnknowns(t *testing.T) {
	t.Parallel()

	c, err := catalog.Load()
	require.NoError(t, err)

	require.True(t, c.IsExemption("global-table"))
	require.False(t, c.IsExemption(catalog.MissingTenantScope))
	require.False(t, c.IsExemption("not-a-category"))
}
