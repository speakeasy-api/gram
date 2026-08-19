package platformmcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

func docsCorpus(observedAt time.Time) []SetupResource {
	guide := func(provider, title, body string, aliases []string) SetupResource {
		return SetupResource{
			URI:          SetupResourceURI(provider, "provider_setup"),
			Name:         provider + "-provider-setup",
			Title:        title,
			Description:  "Reviewed setup guide for " + title + ".",
			Text:         body,
			Provider:     provider,
			Intent:       "provider_setup",
			Owner:        "fixture reviewers",
			Source:       "fixture@v1",
			ObservedAt:   observedAt,
			RevalidateBy: observedAt.AddDate(0, 0, 90),
			Aliases:      aliases,
			Links:        []string{"https://example.test/" + provider},
		}
	}
	return []SetupResource{
		guide("acme", "Acme", "# Acme\n\n## Generate OAuth credentials {#generate-oauth-credentials}\n\nRegister an OAuth app and copy the client id.\n\n## Find your tenant URL\n\nAcme tenants each have their own endpoint.\n", []string{"io.example/acme-mcp"}),
		guide("beta", "Beta", "# Beta\n\n## Create an API key\n\nBeta uses a static API key rather than OAuth.\n", nil),
	}
}

func TestDocsIndexCitesTheMatchedGuide(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	index := NewMemoryDocsIndex(docsCorpus(now.AddDate(0, 0, -10)), func() time.Time { return now })

	excerpts, err := index.Search(t.Context(), "acme oauth client id", maxDocsExcerpts)
	require.NoError(t, err)
	require.NotEmpty(t, excerpts)

	top := excerpts[0]
	require.Equal(t, SetupResourceURI("acme", "provider_setup"), top.URI)
	require.Equal(t, "Generate OAuth credentials", top.Heading)
	require.Equal(t, "fixture@v1", top.Source)
	require.Equal(t, "2026-07-22", top.ObservedAt)
	require.Equal(t, []string{"https://example.test/acme"}, top.Links)
	require.False(t, top.Stale)

	// One excerpt per guide: a caller asking a broad question should see the
	// range of guides that answer it, not one guide's table of contents.
	seen := map[string]bool{}
	for _, excerpt := range excerpts {
		require.False(t, seen[excerpt.URI], "duplicate guide %s in one result", excerpt.URI)
		seen[excerpt.URI] = true
		require.LessOrEqual(t, len(excerpt.Excerpt), maxDocsExcerptBytes)
	}
	require.LessOrEqual(t, len(excerpts), maxDocsExcerpts)
}

// An alias is how a caller names a provider when they are reading a registry
// entry rather than a guide, so it has to match the guide it belongs to.
func TestDocsIndexMatchesAliases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	index := NewMemoryDocsIndex(docsCorpus(now), func() time.Time { return now })

	excerpts, err := index.Search(t.Context(), "io.example/acme-mcp", maxDocsExcerpts)
	require.NoError(t, err)
	require.NotEmpty(t, excerpts)
	require.Equal(t, SetupResourceURI("acme", "provider_setup"), excerpts[0].URI)
}

// The limit is a bound the caller sets, not a hint. Returning the cap for a
// non-positive limit would make a caller asking for nothing receive five.
func TestDocsIndexHonoursTheRequestedLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	index := NewMemoryDocsIndex(docsCorpus(now), func() time.Time { return now })

	for _, limit := range []int{0, -1} {
		excerpts, err := index.Search(t.Context(), "acme beta credentials", limit)
		require.NoError(t, err)
		require.Emptyf(t, excerpts, "limit %d", limit)
	}

	excerpts, err := index.Search(t.Context(), "acme beta credentials", 1)
	require.NoError(t, err)
	require.Len(t, excerpts, 1)
}

// Every guide talks about OAuth clients and credentials, so a provider-named
// question scores against all of them. A weak match must not ride along as a
// citation — five sources for a one-provider question read as five answers.
func TestDocsIndexDropsWeakMatchesAgainstTheBestOne(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	index := NewMemoryDocsIndex(docsCorpus(now), func() time.Time { return now })

	// Both guides mention API keys and credentials; only one is Acme's.
	excerpts, err := index.Search(t.Context(), "acme oauth credentials", maxDocsExcerpts)
	require.NoError(t, err)
	require.Len(t, excerpts, 1, "only the named provider is cited")
	require.Equal(t, SetupResourceURI("acme", "provider_setup"), excerpts[0].URI)

	// The floor is relative, not a provider filter: a question that genuinely
	// spans providers still cites each one.
	excerpts, err = index.Search(t.Context(), "acme beta credentials", maxDocsExcerpts)
	require.NoError(t, err)
	require.Len(t, excerpts, 2)
}

// Content past its grace window is withheld rather than ranked low: an
// unreviewed step that loses on score today wins on score tomorrow.
func TestDocsIndexWithholdsContentPastItsGraceWindow(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	corpus := docsCorpus(observed)
	stale := observed.AddDate(0, 0, 90+setupResourceGraceDays-1)
	withheld := observed.AddDate(0, 0, 90+setupResourceGraceDays+1)

	now := stale
	index := NewMemoryDocsIndex(corpus, func() time.Time { return now })
	excerpts, err := index.Search(t.Context(), "acme oauth", maxDocsExcerpts)
	require.NoError(t, err)
	require.NotEmpty(t, excerpts)
	require.True(t, excerpts[0].Stale, "content inside the grace window is served, flagged")

	now = withheld
	excerpts, err = index.Search(t.Context(), "acme oauth", maxDocsExcerpts)
	require.NoError(t, err)
	require.Empty(t, excerpts, "content past the grace window is not served at all")
}

func allowingDocsBudget() OperationBudget {
	return OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
		Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
	}
}

func searchDocs(t *testing.T, reg *Registrar, query string) SearchGramDocsToolOutput {
	t.Helper()

	var descriptor Descriptor
	for _, candidate := range reg.Descriptors() {
		if candidate.Name == "search_gram_docs" {
			descriptor = candidate
		}
	}
	require.NotEmpty(t, descriptor.Name, "search_gram_docs is registered")

	ctx := ContextWithPrincipal(t.Context(), Principal{OrganizationID: "organization", ConnectionID: "connection"})
	arguments, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	result, err := descriptor.Invoke(ctx, arguments)
	require.NoError(t, err)

	output, ok := result.(SearchGramDocsToolOutput)
	require.True(t, ok, "search_gram_docs returns its structured output")
	return output
}

// Every citation must resolve to a resource this deployment actually serves.
// A search result whose links dangle is worse than no result: it reads as a
// reviewed source the reader can open, and it cannot be opened.
func TestSearchDocsCitationsResolveToRegisteredResources(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	corpus := docsCorpus(now)
	reg := newRegistrar(newTestMCPServer())
	registerSetupResources(reg, corpus, func() time.Time { return now })
	registerSearchDocsTool(reg, NewMemoryDocsIndex(corpus, func() time.Time { return now }), allowingDocsBudget())

	output := searchDocs(t, reg, "acme oauth client id")
	require.NotEmpty(t, output.Excerpts)
	require.Empty(t, output.Code)

	for _, excerpt := range output.Excerpts {
		for _, audience := range []Audience{AudienceExternal, AudienceAssistant} {
			_, ok := reg.ResourceFor(audience, excerpt.URI)
			require.Truef(t, ok, "cited resource %s is not readable by the %s audience", excerpt.URI, audience)
		}
	}

	// The same excerpts are offered as resource links, so a client that renders
	// them can open the full guide rather than re-deriving the URI.
	content := docsExcerptContent(output.Excerpts)
	links := 0
	for _, item := range content {
		if link, ok := item.(*mcp.ResourceLink); ok {
			links++
			_, resolved := reg.ResourceFor(AudienceExternal, link.URI)
			require.True(t, resolved, "resource link %s does not resolve", link.URI)
		}
	}
	require.Equal(t, len(output.Excerpts), links, "every excerpt carries a resource link")
}

func TestSearchDocsReturnsGuideUnavailableRatherThanGuessing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	corpus := docsCorpus(now)
	reg := newRegistrar(newTestMCPServer())
	registerSearchDocsTool(reg, NewMemoryDocsIndex(corpus, func() time.Time { return now }), allowingDocsBudget())

	output := searchDocs(t, reg, "gamma quantum teleportation setup")
	require.Empty(t, output.Excerpts)
	require.Equal(t, setupGuideUnavailableCode, output.Code)
	require.Contains(t, output.Message, "Do not invent setup steps")
}

func readDoc(t *testing.T, reg *Registrar, uri string) ReadGramDocToolOutput {
	t.Helper()

	var descriptor Descriptor
	for _, candidate := range reg.Descriptors() {
		if candidate.Name == "read_gram_doc" {
			descriptor = candidate
		}
	}
	require.NotEmpty(t, descriptor.Name, "read_gram_doc is registered")

	ctx := ContextWithPrincipal(t.Context(), Principal{OrganizationID: "organization"})
	arguments, err := json.Marshal(map[string]string{"uri": uri})
	require.NoError(t, err)
	result, err := descriptor.Invoke(ctx, arguments)
	require.NoError(t, err)

	output, ok := result.(ReadGramDocToolOutput)
	require.True(t, ok, "read_gram_doc returns its structured output")
	return output
}

// The assistant follows a citation with a tool because its channel has no
// resources/read. What it gets back must be the same content, under the same
// freshness rules, that an external client would read at that URI.
func TestReadDocAnswersTheSameURIsSearchCites(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	corpus := docsCorpus(observed)
	now := observed.AddDate(0, 0, 1)
	reg := newRegistrar(newTestMCPServer())
	registerSetupResources(reg, corpus, func() time.Time { return now })
	registerSearchDocsTool(reg, NewMemoryDocsIndex(corpus, func() time.Time { return now }), allowingDocsBudget())
	registerReadDocTool(reg)

	cited := searchDocs(t, reg, "acme oauth client id").Excerpts[0].URI
	output := readDoc(t, reg, cited)
	require.Empty(t, output.Code)
	require.Equal(t, cited, output.URI)
	require.Contains(t, output.Text, "Generate OAuth credentials")

	// Withheld content and an unpublished URI answer the same way: no guide,
	// no reconstruction, an explicit code the model is told how to handle.
	now = observed.AddDate(0, 0, 90+setupResourceGraceDays+1)
	withheld := readDoc(t, reg, cited)
	require.Equal(t, setupGuideUnavailableCode, withheld.Code)
	require.Empty(t, withheld.Text)

	unknown := readDoc(t, reg, SetupResourceURI("gamma", "provider_setup"))
	require.Equal(t, setupGuideUnavailableCode, unknown.Code)
	require.Empty(t, unknown.Text)
}

func TestSearchDocsRefusesWhenRateLimited(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	corpus := docsCorpus(now)
	reg := newRegistrar(newTestMCPServer())
	registerSearchDocsTool(reg, NewMemoryDocsIndex(corpus, func() time.Time { return now }), OperationBudget{
		Connection:   &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}},
		Organization: &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}},
	})

	var descriptor Descriptor
	for _, candidate := range reg.Descriptors() {
		if candidate.Name == "search_gram_docs" {
			descriptor = candidate
		}
	}
	ctx := ContextWithPrincipal(t.Context(), Principal{OrganizationID: "organization", ConnectionID: "connection"})
	arguments, err := json.Marshal(map[string]string{"query": "acme"})
	require.NoError(t, err)

	_, err = descriptor.Invoke(ctx, arguments)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "rate_limited")
}
