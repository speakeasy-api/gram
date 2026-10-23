// Asserts the platform toolset serves the embedded reviewed skills over the
// Skills extension (SEP-2640) behind the skills-over-mcp flag: the handshake
// advertises the extension, the listing is exactly the embedded set with
// verifying digests, reads return the embedded bytes without recording an
// activation, and with the flag off the surface is unchanged.
package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// platformSkillsFixture is a project whose managed assistant can reach the
// managed-assistant platform toolset, with the Skills extension flag on for
// the owning organization.
type platformSkillsFixture struct {
	authCtx *contextvalues.AuthContext
	token   string
	slug    string
}

func newPlatformSkillsFixture(t *testing.T, ctx context.Context, ti *testInstance) *platformSkillsFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID := createAssistant(t, ti, authCtx, "Managed")
	err := assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	})
	require.NoError(t, err)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, authCtx.ActiveOrganizationID, true)

	return &platformSkillsFixture{
		authCtx: authCtx,
		token:   mintAssistantToken(t, ti, authCtx, managedID),
		slug:    platformtools.ManagedAssistantPlatformToolsetSlug,
	}
}

// servePlatformDiscover sends a 2026-07-28 server/discover to the platform
// toolset, which carries its declaration in the header and `_meta` and
// mirrors the method in the Mcp-Method header.
func servePlatformDiscover(t *testing.T, ti *testInstance, f *platformSkillsFixture) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/platform/mcp/"+f.slug, bytes.NewReader(makeDiscoverBody(t)))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20260728)
	req.Header.Set("Mcp-Method", mcpversions.MethodServerDiscover)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("toolsetSlug", f.slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePlatformToolset(w, req))
	return w
}

// embeddedPlatformSkills returns the reviewed skills the server embeds, which
// is what the platform toolset must serve.
func embeddedPlatformSkills(t *testing.T) map[string][]byte {
	t.Helper()

	sources, err := plugins.PlatformMCPSkills()
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	return sources
}

func TestServePlatformToolset_Skills_InitializeAdvertisesExtensionWhenFlagOn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)

	w, err := servePlatformHTTP(t, ti, f.slug, makeInitializeBody(), f.token)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	requireSkillsCapability(t, result["capabilities"])
}

func TestServePlatformToolset_Skills_InitializeOmitsExtensionWhenFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, f.authCtx.ActiveOrganizationID, false)

	w, err := servePlatformHTTP(t, ti, f.slug, makeInitializeBody(), f.token)
	require.NoError(t, err)
	result := requireRPCResult(t, w)

	var capabilities map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result["capabilities"], &capabilities))
	require.Contains(t, capabilities, "tools")
	require.NotContains(t, capabilities, "resources", "resources exist on this surface only to carry skill reads")
	require.NotContains(t, capabilities, "extensions")
}

func TestServePlatformToolset_Skills_DiscoverAdvertisesExtension(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)

	w := servePlatformDiscover(t, ti, f)
	result := requireRPCResult(t, w)
	requireSkillsCapability(t, result["capabilities"])
}

func TestServePlatformToolset_Skills_ListIsTheEmbeddedSet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)
	embedded := embeddedPlatformSkills(t)

	w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{}), f.token)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `"complete"`, string(result["resultType"]))
	requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")
	require.NotContains(t, result, "nextCursor")

	var skills []wireSkill
	require.NoError(t, json.Unmarshal(result["skills"], &skills))
	require.Len(t, skills, len(embedded))

	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		projectSlug, name, ok := mcpskills.ParseSkillURI(skill.URI)
		require.True(t, ok, "uri=%s", skill.URI)
		require.Equal(t, mcpskills.EmbeddedProjectSlug, projectSlug, "uri=%s", skill.URI)
		names = append(names, name)

		content, ok := embedded[name]
		require.True(t, ok, "listed skill %q is not embedded", name)
		digest := sha256.Sum256(content)
		require.Equal(t, name, skill.Frontmatter["name"])
		require.NotEmpty(t, skill.Frontmatter["description"])
		require.Len(t, skill.Resources, 1)
		require.Equal(t, skill.URI, skill.Resources[0].URI)
		require.Equal(t, "sha256:"+hex.EncodeToString(digest[:]), skill.Resources[0].Digest)
		require.Equal(t, len(content), skill.Resources[0].Size)
	}
	require.Equal(t, slices.Sorted(maps.Keys(embedded)), names, "listing is every embedded skill once, sorted by name")
}

func TestServePlatformToolset_Skills_ListRejectsUnknownCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)

	w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodSkillsList, map[string]any{"cursor": "page-2"}), f.token)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
}

func TestServePlatformToolset_Skills_GetByURI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)
	embedded := embeddedPlatformSkills(t)
	name := slices.Sorted(maps.Keys(embedded))[0]
	uri := mcpskills.SkillURI(mcpskills.EmbeddedProjectSlug, name)

	w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": uri}), f.token)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	requireCacheHints(t, decodeRPCResponse(t, w)["result"], "private")

	var skill wireSkill
	require.NoError(t, json.Unmarshal(result["skill"], &skill))
	digest := sha256.Sum256(embedded[name])
	require.Equal(t, uri, skill.URI)
	require.Equal(t, name, skill.Frontmatter["name"])
	require.Len(t, skill.Resources, 1)
	require.Equal(t, "sha256:"+hex.EncodeToString(digest[:]), skill.Resources[0].Digest)

	w, err = servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodSkillsGet, map[string]any{"uri": mcpskills.SkillURI(mcpskills.EmbeddedProjectSlug, "not-an-embedded-skill")}), f.token)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
}

func TestServePlatformToolset_Skills_ReadServesEmbeddedBytesWithoutActivation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)
	embedded := embeddedPlatformSkills(t)

	for name, content := range embedded {
		uri := mcpskills.SkillURI(mcpskills.EmbeddedProjectSlug, name)
		w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), f.token)
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
		require.Equal(t, string(content), contents[0].Text, "the served bytes are the embedded ones")
	}

	// Embedded skills belong to no stored version, so a read is no activation.
	observations, err := testrepo.New(ti.conn).ListMCPSkillObservations(ctx, testrepo.ListMCPSkillObservationsParams{
		ProjectID: *f.authCtx.ProjectID, Provider: mcpskills.ObservationProvider,
	})
	require.NoError(t, err)
	require.Empty(t, observations)
}

func TestServePlatformToolset_Skills_ReadUnknownURIIsInvalidParams(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)

	w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": mcpskills.SkillURI(mcpskills.EmbeddedProjectSlug, "not-an-embedded-skill")}), f.token)
	require.NoError(t, err)
	requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)

	// Any other URI the surface does not serve is an unknown resource, not an
	// unknown method, while the extension declares resources.
	for _, uri := range []string{"skill://not-an-embedded-skill/references/example.md", "file:///etc/passwd"} {
		w, err = servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodResourcesRead, map[string]any{"uri": uri}), f.token)
		require.NoError(t, err)
		requireRPCErrorCode(t, w, oops.MCPCodeInvalidParams)
	}
}

func TestServePlatformToolset_Skills_ResourcesListIsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)

	w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, mcpversions.MethodResourcesList, map[string]any{}), f.token)
	require.NoError(t, err)
	result := requireRPCResult(t, w)
	require.JSONEq(t, `[]`, string(result["resources"]), "skills are listed through skills/list, never as resources")
}

func TestServePlatformToolset_Skills_FlagOffLeavesSurfaceUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newPlatformSkillsFixture(t, ctx, ti)
	ti.features.SetFlag(feature.FlagSkillsOverMCP, f.authCtx.ActiveOrganizationID, false)
	grantProjectLogRead(t, ti, f.authCtx)
	name := slices.Sorted(maps.Keys(embeddedPlatformSkills(t)))[0]
	uri := mcpskills.SkillURI(mcpskills.EmbeddedProjectSlug, name)

	for _, method := range []string{mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet, mcpversions.MethodResourcesRead, mcpversions.MethodResourcesList} {
		w, err := servePlatformHTTP(t, ti, f.slug, makeMetaRPCBody(t, method, map[string]any{"uri": uri}), f.token)
		require.NoError(t, err, "method=%s", method)
		requireRPCErrorCode(t, w, oops.MCPCodeMethodNotFound)
	}

	w, err := servePlatformHTTP(t, ti, f.slug, toolsListBody(), f.token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "tools stay served with the extension off: %s", w.Body.String())
	require.Contains(t, w.Body.String(), platformtools.ToolNameSearchLogs)
}
