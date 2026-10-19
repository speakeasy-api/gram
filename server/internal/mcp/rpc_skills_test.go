package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	mcpskillsrepo "github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// skillsFixture is one public hosted toolset in the test user's project with
// the Skills extension flag on for the owning organization, plus the ids a
// test needs to distribute skills to it.
type skillsFixture struct {
	authCtx        *contextvalues.AuthContext
	projectID      uuid.UUID
	organizationID string
	toolsetID      uuid.UUID
	mcpSlug        string
}

func newSkillsFixture(t *testing.T, ctx context.Context, ti *testInstance) *skillsFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "skills-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, authCtx.ActiveOrganizationID, true)

	return &skillsFixture{
		authCtx:        authCtx,
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		toolsetID:      toolset.ID,
		mcpSlug:        toolset.McpSlug.String,
	}
}

func skillContent(name, description string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name)
}

func rawSHA256(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

// servedSkill distributes one skill with one spec-valid version through a
// plugin that contains the fixture toolset and is assigned to principalURN.
// It returns the skill's id and the served content.
func servedSkill(t *testing.T, ctx context.Context, ti *testInstance, f *skillsFixture, name, principalURN string) (uuid.UUID, string) {
	t.Helper()

	return servedSkillVia(t, ctx, ti, f, name, principalURN, conv.ToNullUUID(f.toolsetID), uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

// servedSkillVia is servedSkill with the plugin containing the given toolset
// or MCP server instead of the fixture toolset.
func servedSkillVia(t *testing.T, ctx context.Context, ti *testInstance, f *skillsFixture, name, principalURN string, toolsetID, mcpServerID uuid.NullUUID) (uuid.UUID, string) {
	t.Helper()

	content := skillContent(name, "Serves "+name+".")
	skillsQueries := skillsrepo.New(ti.conn)
	skill, err := skillsQueries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: f.projectID, Name: name, DisplayName: name, Summary: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = skillsQueries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: content, CanonicalSha256: rawSHA256(content), RawSha256: rawSHA256(content), Description: pgtype.Text{},
		Metadata: []byte("{}"), SpecValid: true, ValidationErrors: []byte("[]"), CreatedByUserID: f.authCtx.UserID,
		ProjectID: f.projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)

	pluginsQueries := pluginsrepo.New(ti.conn)
	plugin, err := pluginsQueries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: f.organizationID, ProjectID: f.projectID, Name: name + "-plugin", Slug: name + "-plugin", Description: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = pluginsQueries.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: plugin.ID, ToolsetID: toolsetID, McpServerID: mcpServerID,
		DisplayName: "toolset", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	_, err = pluginsQueries.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: f.organizationID, PrincipalUrn: principalURN, InstallMode: "default", PluginID: plugin.ID,
	})
	require.NoError(t, err)

	_, err = skillsQueries.CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
		ProjectID: f.projectID, SkillID: skill.ID, PluginID: conv.ToNullUUID(plugin.ID),
		AssistantID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, PinnedVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Channel: "plugin", CreatedByUserID: f.authCtx.UserID,
	})
	require.NoError(t, err)

	return skill.ID, content
}

// makeDiscoverBody builds a 2026-07-28 server/discover request, which
// carries its declaration in `_meta` alongside the MCP-Protocol-Version header.
func makeDiscoverBody(t *testing.T) []byte {
	t.Helper()

	return makeMetaRPCBody(t, mcpversions.MethodServerDiscover, map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    mcpversions.Version20260728,
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "skills-client", "version": "1.0.0"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	})
}

// requireRPCResult decodes a successful response and returns its result.
func requireRPCResult(t *testing.T, w *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	rpc := decodeRPCResponse(t, w)
	require.NotContains(t, rpc, "error", "body=%s", w.Body.String())

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpc["result"], &result))
	return result
}

// requireRPCErrorCode asserts the response carries a JSON-RPC error with the
// given code.
func requireRPCErrorCode(t *testing.T, w *httptest.ResponseRecorder, code oops.MCPCode) {
	t.Helper()

	rpc := decodeRPCResponse(t, w)
	require.Contains(t, rpc, "error", "body=%s", w.Body.String())

	var rpcErr struct {
		Code    oops.MCPCode `json:"code"`
		Message string       `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rpc["error"], &rpcErr))
	require.Equal(t, code, rpcErr.Code, "body=%s", w.Body.String())
}

// requireSkillsCapability asserts the capabilities map declares the Skills
// extension with no optional features, and that the resources capability it
// depends on is declared too.
func requireSkillsCapability(t *testing.T, rawCapabilities json.RawMessage) {
	t.Helper()

	var capabilities map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawCapabilities, &capabilities))
	require.Contains(t, capabilities, "resources")
	require.Contains(t, capabilities, "extensions")

	var extensions map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(capabilities["extensions"], &extensions))
	require.JSONEq(t, `{}`, string(extensions[mcpversions.ExtensionSkills]))
}

// wireSkill is a SEP-2640 skill entry as a client decodes it.
type wireSkill struct {
	URI         string         `json:"uri"`
	Frontmatter map[string]any `json:"frontmatter"`
	Resources   []struct {
		URI    string `json:"uri"`
		Digest string `json:"digest"`
		Size   int    `json:"size"`
	} `json:"resources"`
}

func requireWireSkill(t *testing.T, skill wireSkill, name, content string) {
	t.Helper()

	uri := mcpskills.SkillURI(name)
	require.Equal(t, uri, skill.URI)
	require.Equal(t, name, skill.Frontmatter["name"])
	require.Equal(t, "Serves "+name+".", skill.Frontmatter["description"])
	require.NotContains(t, skill.Frontmatter, "allowed-tools")
	require.Len(t, skill.Resources, 1)
	require.Equal(t, uri, skill.Resources[0].URI)
	require.Equal(t, "sha256:"+rawSHA256(content), skill.Resources[0].Digest)
	require.Equal(t, len(content), skill.Resources[0].Size)
}

func TestServePublic_Skills_InitializeAdvertisesExtensionWhenFlagOn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	requireSkillsCapability(t, result["capabilities"])
}

func TestServePublic_Skills_InitializeOmitsExtensionWhenFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, f.organizationID, false)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)

	var capabilities map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result["capabilities"], &capabilities))
	require.Contains(t, capabilities, "resources")
	require.NotContains(t, capabilities, "extensions")
}

func TestServePublic_Skills_DiscoverAdvertisesExtension(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeDiscoverBody(t), "", map[string]string{
		mcpversions.HTTPHeader: mcpversions.Version20260728,
		"Mcp-Method":           mcpversions.MethodServerDiscover,
	})
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	requireSkillsCapability(t, result["capabilities"])
}

func TestServePublic_Skills_ListServesAssignedPluginSkills(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	_, assignedContent := servedSkill(t, ctx, ti, f, "assigned-skill", urn.PrincipalWildcard)
	servedSkill(t, ctx, ti, f, "unassigned-skill", urn.NewPrincipal(urn.PrincipalTypeEmail, "someone-else@example.test").String())

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `"complete"`, string(result["resultType"]))
	requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")

	var skills []wireSkill
	require.NoError(t, json.Unmarshal(result["skills"], &skills))
	require.Len(t, skills, 1, "only the skill carried by an assigned plugin is served")
	requireWireSkill(t, skills[0], "assigned-skill", assignedContent)
	require.NotContains(t, result, "nextCursor")
}

func TestServePublic_Skills_ListServesSkillsOfLegacyToolsetWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	wrapper, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:         uuid.New(),
		ProjectID:  f.projectID,
		Name:       conv.ToPGText("wrapper-" + f.mcpSlug),
		Slug:       conv.ToPGText("wrapper-" + f.mcpSlug),
		ToolsetID:  conv.ToNullUUID(f.toolsetID),
		Visibility: "public",
	})
	require.NoError(t, err)
	_, content := servedSkillVia(t, ctx, ti, f, "wrapper-skill", urn.PrincipalWildcard, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, conv.ToNullUUID(wrapper.ID))

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)

	var skills []wireSkill
	require.NoError(t, json.Unmarshal(result["skills"], &skills))
	require.Len(t, skills, 1, "a plugin containing the toolset's only wrapper carries skills to the legacy route")
	requireWireSkill(t, skills[0], "wrapper-skill", content)
}

func TestServePublic_Skills_ListAcceptsEmptyCursorAndRejectsUnknownOne(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{"cursor": ""}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `[]`, string(result["skills"]))

	w, err = servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{"cursor": "page-2"}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
}

func TestServePublic_Skills_GetByURI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	_, content := servedSkill(t, ctx, ti, f, "get-skill", urn.PrincipalWildcard)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": mcpskills.SkillURI("get-skill")}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `"complete"`, string(result["resultType"]))
	requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")

	var skill wireSkill
	require.NoError(t, json.Unmarshal(result["skill"], &skill))
	requireWireSkill(t, skill, "get-skill", content)

	w, err = servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": mcpskills.SkillURI("missing-skill")}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
}

func TestServePublic_Skills_ReadServesMarkdownAndRecordsOneActivationPerSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	skillID, content := servedSkill(t, ctx, ti, f, "read-skill", urn.PrincipalWildcard)
	uri := mcpskills.SkillURI("read-skill")
	body := makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri})
	sessionA := "skills-session-" + uuid.NewString()[:8]
	sessionB := "skills-session-" + uuid.NewString()[:8]

	for _, session := range []string{sessionA, sessionA, sessionB} {
		w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, body, "", map[string]string{"Mcp-Session-Id": session})
		require.NoError(t, err)
		result := requireRPCResult(t, w)
		requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")

		var contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		}
		require.NoError(t, json.Unmarshal(result["contents"], &contents))
		require.Len(t, contents, 1)
		require.Equal(t, uri, contents[0].URI)
		require.Equal(t, "text/markdown", contents[0].MimeType)
		require.Equal(t, content, contents[0].Text)
		require.Equal(t, rawSHA256(content), rawSHA256(contents[0].Text), "the served bytes are the ones the digest covers")
	}

	observations, err := mcpskillsrepo.New(ti.conn).ListMCPSkillObservations(ctx, mcpskillsrepo.ListMCPSkillObservationsParams{
		ProjectID: f.projectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Len(t, observations, 2, "a re-read in the same session is not a second activation")
	for _, observation := range observations {
		require.Equal(t, skillID, observation.SkillID.UUID)
		require.True(t, observation.SkillVersionID.Valid)
		require.True(t, observation.ReconciledAt.Valid)
		require.Equal(t, f.authCtx.UserID, observation.UserID.String)
		require.Equal(t, uri, observation.SourcePath.String)
	}
	require.Equal(t, sessionA, observations[0].SessionID.String)
	require.Equal(t, sessionB, observations[1].SessionID.String)
}

func TestServePublic_Skills_ReadUnknownURIIsInvalidParams(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	servedSkill(t, ctx, ti, f, "other-skill", urn.PrincipalWildcard)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": mcpskills.SkillURI("missing-skill")}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)

	observations, err := mcpskillsrepo.New(ti.conn).ListMCPSkillObservations(ctx, mcpskillsrepo.ListMCPSkillObservationsParams{
		ProjectID: f.projectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Empty(t, observations)
}

func TestServePublic_Skills_AnonymousCallerIsServedNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	servedSkill(t, ctx, ti, f, "members-only", urn.PrincipalWildcard)
	uri := mcpskills.SkillURI("members-only")

	// The public toolset answers without credentials; a request context
	// without the test session is an anonymous caller.
	anonymous := t.Context()

	w, err := servePublicHTTP(t, anonymous, ti, f.mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	requireSkillsCapability(t, requireRPCResult(t, w)["capabilities"])

	w, err = servePublicHTTP(t, anonymous, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), "", nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(requireRPCResult(t, w)["skills"]))

	w, err = servePublicHTTP(t, anonymous, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": uri}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)

	w, err = servePublicHTTP(t, anonymous, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
}

func TestServePublic_Skills_APIKeyCallerIsServedNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	// Assigned to everyone: the plugin would reach any caller whose
	// principals were resolved at all.
	servedSkill(t, ctx, ti, f, "keyed-out", urn.PrincipalWildcard)
	uri := mcpskills.SkillURI("keyed-out")

	// A project API key created by the test user. The key authenticates and
	// carries its creator's user id, but it is not that person.
	apiKey := ti.createTestAPIKey(ctx, t)
	keyed := t.Context()

	w, err := servePublicHTTP(t, keyed, ti, f.mcpSlug, makeInitializeBody(), apiKey, nil)
	require.NoError(t, err)
	requireSkillsCapability(t, requireRPCResult(t, w)["capabilities"])

	w, err = servePublicHTTP(t, keyed, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), apiKey, nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(requireRPCResult(t, w)["skills"]), "an API key is not served its creator's assignments")

	w, err = servePublicHTTP(t, keyed, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": uri}), apiKey, nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)

	w, err = servePublicHTTP(t, keyed, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), apiKey, nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)

	observations, err := mcpskillsrepo.New(ti.conn).ListMCPSkillObservations(ctx, mcpskillsrepo.ListMCPSkillObservationsParams{
		ProjectID: f.projectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Empty(t, observations, "nothing is recorded as the key creator's activation")

	// The same skill is served to the user whose session the key was made
	// from, so the exclusion is about the credential, not the fixture.
	w, err = servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), "", nil)
	require.NoError(t, err)
	var skills []wireSkill
	require.NoError(t, json.Unmarshal(requireRPCResult(t, w)["skills"], &skills))
	require.Len(t, skills, 1)
}

func TestServePublic_Skills_FlagOffAnswersMethodNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	servedSkill(t, ctx, ti, f, "gated-skill", urn.PrincipalWildcard)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, f.organizationID, false)
	uri := mcpskills.SkillURI("gated-skill")

	for _, method := range []string{mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet} {
		w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, method, map[string]any{"uri": uri}), "", nil)
		require.NoError(t, err)
		requireRPCErrorCode(t, w, oops.MCPCodeMethodNotFound)
	}

	// With the extension off a skill:// URI is an ordinary resource URI, and
	// the toolset has no resource at it.
	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), "", nil)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeResourceNotFound)

	observations, err := mcpskillsrepo.New(ti.conn).ListMCPSkillObservations(ctx, mcpskillsrepo.ListMCPSkillObservationsParams{
		ProjectID: f.projectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Empty(t, observations)
}

func TestServePublic_Skills_ResourcesListExcludesSkills(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newSkillsFixture(t, ctx, ti)
	servedSkill(t, ctx, ti, f, "listed-skill", urn.PrincipalWildcard)

	w, err := servePublicHTTP(t, ctx, ti, f.mcpSlug, makeMetaRPCBody(t, mcpversions.MethodResourcesList, map[string]any{}), "", nil)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `[]`, string(result["resources"]))
}
