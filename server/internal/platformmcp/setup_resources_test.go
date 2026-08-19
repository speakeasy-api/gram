package platformmcp

import (
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func newTestMCPServer() *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{
		Name:    "platform-mcp-test",
		Version: "0.0.1",
	}, nil)
}

func testSetupResource() SetupResource {
	observed := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return SetupResource{
		URI:          SetupResourceURI("fixture", "provider_setup"),
		Name:         "fixture-provider-setup",
		Title:        "Fixture provider setup",
		Description:  "Static reviewed fixture guide.",
		Text:         "# Setup\n",
		Provider:     "fixture",
		Intent:       "provider_setup",
		Owner:        "fixture reviewers",
		Source:       "fixture@v1",
		ObservedAt:   observed,
		RevalidateBy: observed.AddDate(0, 0, 90),
		Aliases:      nil,
		Links:        []string{"https://example.test/guide"},
		DocsURL:      "https://example.test/docs",
	}
}

func TestSetupResourceValidation(t *testing.T) {
	t.Parallel()

	valid := testSetupResource()
	require.NoError(t, ValidateSetupResource(valid))

	withURI := func(uri string) SetupResource {
		resource := testSetupResource()
		resource.URI = uri
		return resource
	}
	oversized := testSetupResource()
	oversized.Text = strings.Repeat("a", maxSetupResourceBytes+1)
	unattributed := testSetupResource()
	unattributed.Source = ""
	undated := testSetupResource()
	undated.ObservedAt = time.Time{}

	for name, resource := range map[string]SetupResource{
		"unparseable provider": withURI("gram://platform-mcp/setup/%zz/provider_setup"),
		"foreign scheme":       withURI("https://example.test/setup"),
		"extra path segment":   withURI(SetupResourceURI("fixture", "provider_setup/extra")),
		"oversized":            oversized,
		"unattributed":         unattributed,
		"undated":              undated,
	} {
		require.Error(t, ValidateSetupResource(resource), name)
	}
}

func TestSetupResourceStalenessWithholdsPastGraceWindow(t *testing.T) {
	t.Parallel()

	resource := testSetupResource()
	require.Equal(t, setupFresh, resource.staleness(resource.RevalidateBy.Add(-time.Hour)))
	require.Equal(t, setupStale, resource.staleness(resource.RevalidateBy.AddDate(0, 0, setupResourceGraceDays-1)))
	require.Equal(t, setupWithheld, resource.staleness(resource.RevalidateBy.AddDate(0, 0, setupResourceGraceDays+1)))
}

func TestSetupResourceReadWarnsThenWithholds(t *testing.T) {
	t.Parallel()

	resource := testSetupResource()
	now := resource.RevalidateBy.Add(-time.Hour)
	reg := newRegistrar(newTestMCPServer())
	registerSetupResources(reg, []SetupResource{resource}, func() time.Time { return now })

	descriptor, ok := reg.ResourceFor(AudienceExternal, resource.URI)
	require.True(t, ok, "resource is served to external clients")
	_, assistantOK := reg.ResourceFor(AudienceAssistant, resource.URI)
	require.True(t, assistantOK, "resource is served to the assistant so citation links resolve there too")

	text, err := descriptor.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, resource.Text, text)

	now = resource.RevalidateBy.AddDate(0, 0, 1)
	text, err = descriptor.Read(t.Context())
	require.NoError(t, err)
	require.Contains(t, text, "past its revalidation date")
	require.Contains(t, text, "https://example.test/guide")
	require.Contains(t, text, resource.Text)

	now = resource.RevalidateBy.AddDate(0, 0, setupResourceGraceDays+1)
	_, err = descriptor.Read(t.Context())
	require.ErrorIs(t, err, ErrSetupGuideUnavailable)

	// Withholding the guide must not withhold the sources behind it: the links
	// are what a caller is instructed to hand the reader instead.
	var withheld *SetupGuideUnavailableError
	require.ErrorAs(t, err, &withheld)
	require.Equal(t, resource.URI, withheld.URI)
	require.Equal(t, []string{"https://example.test/docs", "https://example.test/guide"}, withheld.TrustedLinks())
}
