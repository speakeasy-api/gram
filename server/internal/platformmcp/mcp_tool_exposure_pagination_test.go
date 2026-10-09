package platformmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedExposedTools commits a toolset version that exposes every fixture tool,
// so a page size below three forces the read to span pages.
func seedExposedTools(t *testing.T, ctx context.Context, fixture toolExposureFixture) {
	t.Helper()
	require.NoError(t, testrepo.New(fixture.conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{
		ToolsetID: fixture.toolsetID, ProjectID: fixture.project.ID, OrganizationID: fixture.principal.OrganizationID,
		Version: 1, ToolUrns: fixture.tools,
	}))
}

func TestToolExposurePagesALargeListToCompletion(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_pages")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Len(t, first.ToolURNs, 2)
	require.Equal(t, 3, first.ToolCount, "tool_count is the whole list, not the page")
	require.True(t, first.Truncated)
	require.NotEmpty(t, first.NextToolCursor, "a partial page hands back a way to read the rest")

	second, err := fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	require.NoError(t, err)
	require.False(t, second.Truncated)
	require.Empty(t, second.NextToolCursor)
	require.Equal(t, 3, second.ToolCount)

	read := slices.Concat(first.ToolURNs, second.ToolURNs)
	require.Equal(t, slices.Sorted(slices.Values(fixture.tools)), read, "the pages together are the whole list, each tool once")
}

func TestToolExposureVersionCoversTheWholeListAndOnlyTheLastPageCarriesIt(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_page_version")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Empty(t, first.ExposureVersion, "a partial page carries no version to confirm a change against")

	last, err := fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	require.NoError(t, err)
	require.Equal(t,
		toolExposureVersion(fixture.project.ID, fixture.toolsetID, fixture.toolsetID, 1, fixture.tools),
		last.ExposureVersion, "the version names the complete committed list")
	require.NotEqual(t,
		toolExposureVersion(fixture.project.ID, fixture.toolsetID, fixture.toolsetID, 1, last.ToolURNs),
		last.ExposureVersion, "the version is not derived from the page it arrived on")

	// The version from a completed read is the one a change is accepted on.
	removed, err := fixture.service.RemoveTools(ctx, fixture.principal, ChangeMCPToolsInput{
		ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(),
		ToolURNs: []string{fixture.tools[0]}, ExpectedVersion: last.ExposureVersion,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", removed.Outcome)
}

// The hazard GRW-233 closes: a caller that has read only the first page of a
// long list must not be able to confirm a change against it.
func TestToolExposureChangeConfirmedAgainstAPartialReadCannotSucceed(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_partial_change")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	partial, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.True(t, partial.Truncated)

	_, err = fixture.service.RemoveTools(ctx, fixture.principal, ChangeMCPToolsInput{
		ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(),
		ToolURNs: []string{partial.ToolURNs[0]}, ExpectedVersion: partial.ExposureVersion,
		IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)

	fixture.service.exposurePageSize = maxExposedToolURNs
	after, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Equal(t, 3, after.ToolCount, "nothing was removed on the strength of a partial read")
}

// A cursor is signed, not encrypted, so anything it carries is readable by the
// caller. It must not carry the version a change is confirmed against, or a
// caller could decode the first page's cursor and skip the rest of the read.
func TestToolExposureCursorDoesNotRevealTheExposureVersion(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_cursor_opaque")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	last, err := fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	require.NoError(t, err)
	require.NotEmpty(t, last.ExposureVersion)

	token, err := base64.RawURLEncoding.DecodeString(first.NextToolCursor)
	require.NoError(t, err)
	require.NotContains(t, string(token), last.ExposureVersion, "the first page's cursor must not hand out the version")
}

// A caller with no cursor binding (for example a connection-less assistant
// with no user) still gets the first page, but nothing it could confirm a
// change against and no cursor it could never present back.
func TestToolExposureUnboundCallerGetsAPartialPageWithoutVersionOrCursor(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_unbound")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	unbound := Principal{OrganizationID: fixture.principal.OrganizationID}
	require.Empty(t, principalCursorBinding(unbound))
	page, err := fixture.service.Exposure(ctx, unbound, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.True(t, page.Truncated)
	require.Len(t, page.ToolURNs, 2)
	require.Empty(t, page.NextToolCursor)
	require.Empty(t, page.ExposureVersion)
}

func TestToolExposurePageAfterTheListChangedIsRefused(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_page_moved")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	require.NoError(t, testrepo.New(fixture.conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{
		ToolsetID: fixture.toolsetID, ProjectID: fixture.project.ID, OrganizationID: fixture.principal.OrganizationID,
		Version: 2, ToolUrns: fixture.tools[:1],
	}))

	_, err = fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code, "pages of two different lists are never stitched together")
}

// A server that stops being toolset-backed mid-read must not look like one
// whose tools are its upstream's: get_mcp would drop tool_exposure and the
// pages already served would pass for the whole list.
func TestToolExposurePageAfterTheServerLostItsToolsetIsRefused(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_page_unbound")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	_, err = toolsetsrepo.New(fixture.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: first.ToolsetSlug, ProjectID: fixture.project.ID})
	require.NoError(t, err)

	_, err = fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code)

	_, err = fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.ErrorIs(t, err, ErrMCPToolExposureMissing, "a fresh read reports the server as not toolset-backed")
}

func TestToolExposureCursorIsBoundToItsServerAndCaller(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_cursor_binding")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)

	other := fixture.principal
	other.Generation = "another-session"
	for name, call := range map[string]func() error{
		"another server": func() error {
			_, err := fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, uuid.New(), first.NextToolCursor)
			return err
		},
		"another session": func() error {
			_, err := fixture.service.ExposurePage(ctx, other, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
			return err
		},
		"a project tool-list cursor": func() error {
			page, err := fixture.service.ListProjectTools(ctx, fixture.principal, fixture.project, ListProjectToolsInput{ProjectID: fixture.project.ID.String(), Limit: 1})
			require.NoError(t, err)
			_, err = fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, page.NextCursor)
			return err
		},
	} {
		var refusal *MCPToolExposureError
		require.ErrorAsf(t, call(), &refusal, "%s", name)
		require.Equalf(t, "invalid_request", refusal.Code, "%s", name)
	}
}

// get_mcp advertises tool_cursor, accepts a partial page without a version,
// and returns a stale cursor as a readable refusal rather than a tool failure.
func TestGetMCPAdvertisesToolPagingAndRefusesAStaleToolCursor(t *testing.T) {
	t.Parallel()

	reader := refusingProjectReader{mcpErr: fmt.Errorf("read platform MCP tool exposure: %w", toolExposureCursorInvalid())}
	server, registrar := newServer(reader, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	bindExternalTestPrincipal(server)
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})

	var getMCP *mcp.Tool
	for _, tool := range listAdvertisedTools(t, server) {
		if tool.Name == "get_mcp" {
			getMCP = tool
		}
	}
	require.NotNil(t, getMCP)
	input, err := json.Marshal(getMCP.InputSchema)
	require.NoError(t, err)
	require.Contains(t, string(input), `"tool_cursor"`)

	encoded, err := json.Marshal(getMCP.OutputSchema)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(encoded, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	page, err := json.Marshal(MCP{ID: "mcp", ProjectID: "project", BackendKind: MCPBackendHosted, Distributions: []MCPDistribution{}, Operations: []string{}, ToolExposure: &MCPToolExposure{
		ToolsetID: "toolset", ToolCount: 3, ToolURNs: []string{"a", "b"}, Truncated: true, NextToolCursor: "next",
	}})
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(page, &decoded))
	require.NoError(t, resolved.Validate(decoded), "a partial page without exposure_version satisfies the advertised schema")

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "tool-exposure-page-test", Version: "0.0.1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_mcp", Arguments: map[string]any{
		"project_id": uuid.NewString(), "mcp_id": uuid.NewString(), "tool_cursor": "stale",
	}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var refusal toolExposureRefusal
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "tool_cursor")
}

// Pages stitched into one read must agree on how far a change would reach, so
// a server that gains a sharing server mid-read refuses the next page.
func TestToolExposurePageAfterTheListGainedASharingServerIsRefused(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_page_shared")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2

	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.Zero(t, first.SharedWithOther)

	_, err = mcpserversrepo.New(fixture.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: fixture.project.ID, Name: conv.ToPGText("Second front"), Slug: conv.ToPGText("second-front"),
		ToolsetID: uuid.NullUUID{UUID: fixture.toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)

	_, err = fixture.service.ExposurePage(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID, first.NextToolCursor)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "conflict", refusal.Code, "the first page's shared_with_other_servers no longer describes the list")
}

// A service missing its page cursor key or page size cannot serve a read it
// could page, so it reports itself unavailable instead of serving one.
func TestToolExposureServiceWithoutPagingIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_tool_exposure_no_paging")
	seedExposedTools(t, ctx, fixture)

	fixture.service.exposurePageSize = 0
	_, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.ErrorIs(t, err, ErrUnavailable)

	fixture.service.exposurePageSize = maxExposedToolURNs
	fixture.service.exposureCursors = nil
	_, err = fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.ErrorIs(t, err, ErrUnavailable)
}

// get_mcp must never answer a tool_cursor with a first-page projection as if
// it continued the read: a cursor nothing can continue is refused, and a
// foreign one is refused before the inventory is read.
func TestGetMCPRefusesAToolCursorItCannotContinue(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedToolExposureFixture(t, t.Context(), "platform_mcp_get_mcp_tool_cursor")
	seedExposedTools(t, ctx, fixture)
	fixture.service.exposurePageSize = 2
	first, err := fixture.service.Exposure(ctx, fixture.principal, fixture.project.ID, fixture.toolsetID)
	require.NoError(t, err)
	require.NotEmpty(t, first.NextToolCursor)

	engine := authz.NewEngine(testenv.NewLogger(t), fixture.conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	input := GetMCPInput{ProjectID: fixture.project.ID.String(), MCPID: fixture.toolsetID.String(), ToolCursor: first.NextToolCursor}
	var refusal *MCPToolExposureError

	withoutExposure := NewPostgresReader(testenv.NewLogger(t), fixture.conn).WithAuthorization(engine)
	_, err = withoutExposure.GetMCP(ctx, fixture.principal, input)
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code, "a reader with no exposure read cannot continue any cursor")

	withExposure := NewPostgresReader(testenv.NewLogger(t), fixture.conn).WithAuthorization(engine).WithToolExposure(fixture.service)
	input.ToolCursor = "not-a-cursor"
	_, err = withExposure.GetMCP(ctx, fixture.principal, input)
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
}
