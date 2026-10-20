package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type testSkillsGate struct {
	enabled bool
	orgs    []string
}

func (g *testSkillsGate) SkillsEnabled(_ context.Context, organizationID string) bool {
	g.orgs = append(g.orgs, organizationID)
	return g.enabled
}

type skillsRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newSkillsTestHandler serves /platform-mcp to an authenticated OAuth caller,
// with the embedded skill catalog behind the given gate.
func newSkillsTestHandler(t *testing.T, gate SkillsGate) http.Handler {
	t.Helper()

	catalog, err := mcpskills.NewEmbeddedCatalog()
	require.NoError(t, err)
	runtime, err := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, &testAuthorizer{}, "", "test-cursor-key", nil, nil, nil, nil, nil).WithSkills(catalog, gate)
	require.NoError(t, err)
	return runtime.Handler()
}

func postSkillsRPC(t *testing.T, handler http.Handler, method string, params any) skillsRPCResponse {
	t.Helper()

	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	bs, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(string(bs)))
	req.Header.Set("Authorization", "Bearer access-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var out skillsRPCResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out), res.Body.String())
	return out
}

func initializeParams() map[string]any {
	return map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "skills-test", "version": "0.0.1"},
	}
}

func firstEmbeddedSkill(t *testing.T) (string, []byte) {
	t.Helper()

	sources, err := plugins.PlatformMCPSkills()
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	for name, content := range sources {
		return name, content
	}
	return "", nil
}

func TestRuntimeServesEmbeddedSkillsWhenFlagOn(t *testing.T) {
	t.Parallel()

	gate := &testSkillsGate{enabled: true}
	handler := newSkillsTestHandler(t, gate)

	initialize := postSkillsRPC(t, handler, "initialize", initializeParams())
	require.Nil(t, initialize.Error)
	var initialized struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(initialize.Result, &initialized))
	require.Contains(t, initialized.Capabilities, "tools")
	require.Contains(t, initialized.Capabilities, "resources")
	var extensions map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(initialized.Capabilities["extensions"], &extensions))
	require.Contains(t, extensions, mcpversions.ExtensionSkills)

	sources, err := plugins.PlatformMCPSkills()
	require.NoError(t, err)

	list := postSkillsRPC(t, handler, mcpversions.MethodSkillsList, nil)
	require.Nil(t, list.Error)
	var listed struct {
		Skills []mcpskills.Skill `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(list.Result, &listed))
	require.Len(t, listed.Skills, len(sources))

	name, content := firstEmbeddedSkill(t)
	uri := mcpskills.SkillURI(name)
	digest := sha256.Sum256(content)

	get := postSkillsRPC(t, handler, mcpversions.MethodSkillsGet, map[string]any{"uri": uri})
	require.Nil(t, get.Error)
	var got struct {
		Skill mcpskills.Skill `json:"skill"`
	}
	require.NoError(t, json.Unmarshal(get.Result, &got))
	require.Equal(t, uri, got.Skill.URI)
	require.Equal(t, name, got.Skill.Frontmatter["name"])
	require.Equal(t, []mcpskills.SkillResource{{URI: uri, Digest: "sha256:" + hex.EncodeToString(digest[:]), Size: len(content)}}, got.Skill.Resources)

	read := postSkillsRPC(t, handler, "resources/read", map[string]any{"uri": uri})
	require.Nil(t, read.Error)
	var contents struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(read.Result, &contents))
	require.Len(t, contents.Contents, 1)
	require.Equal(t, uri, contents.Contents[0].URI)
	require.Equal(t, "text/markdown", contents.Contents[0].MIMEType)
	require.Equal(t, string(content), contents.Contents[0].Text, "served bytes must equal the packaged SKILL.md")

	resources := postSkillsRPC(t, handler, "resources/list", nil)
	require.Nil(t, resources.Error)
	require.NotContains(t, string(resources.Result), "skill://", "skills are listed by skills/list, never by resources/list")

	unknown := postSkillsRPC(t, handler, mcpversions.MethodSkillsGet, map[string]any{"uri": mcpskills.SkillURI("no-such-skill")})
	require.NotNil(t, unknown.Error)
	require.Equal(t, -32602, unknown.Error.Code)

	for _, uri := range []string{mcpskills.SkillURI("no-such-skill"), "skill://no-such-skill/references/example.md"} {
		unknownRead := postSkillsRPC(t, handler, "resources/read", map[string]any{"uri": uri})
		require.NotNil(t, unknownRead.Error, uri)
		require.Equal(t, -32602, unknownRead.Error.Code, uri)
	}

	cursor := postSkillsRPC(t, handler, mcpversions.MethodSkillsList, map[string]any{"cursor": "next"})
	require.NotNil(t, cursor.Error)
	require.Equal(t, -32602, cursor.Error.Code)

	for _, org := range gate.orgs {
		require.Equal(t, testPrincipal().OrganizationID, org, "the flag is evaluated for the caller's organization")
	}
}

func TestRuntimeHidesSkillsWhenFlagOff(t *testing.T) {
	t.Parallel()

	handler := newSkillsTestHandler(t, &testSkillsGate{enabled: false})

	initialize := postSkillsRPC(t, handler, "initialize", initializeParams())
	require.Nil(t, initialize.Error)
	var initialized struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(initialize.Result, &initialized))
	require.NotContains(t, initialized.Capabilities, "extensions")

	for _, method := range []string{mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet} {
		res := postSkillsRPC(t, handler, method, map[string]any{"uri": mcpskills.SkillURI("anything")})
		require.NotNil(t, res.Error, method)
		require.Equal(t, -32601, res.Error.Code, method)
	}

	name, _ := firstEmbeddedSkill(t)
	read := postSkillsRPC(t, handler, "resources/read", map[string]any{"uri": mcpskills.SkillURI(name)})
	require.NotNil(t, read.Error, "a skill read is an ordinary unknown resource while the flag is off")
	require.Nil(t, read.Result)
}
