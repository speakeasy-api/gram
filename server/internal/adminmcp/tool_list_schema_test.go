package adminmcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonschemav6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// listAllAdminTools returns the raw tools/list entries of a runtime with every
// implemented write operation attached, following every page.
func listAllAdminTools(t *testing.T) []json.RawMessage {
	t.Helper()
	operations := make(map[WriteOperation]bool)
	for _, op := range AllWriteOperations {
		if op.implemented() {
			operations[op] = true
		}
	}
	principal := staffPrincipal()
	principal.Scopes = append(principal.Scopes, ScopeWrite)
	runtime := NewRuntime(&testAuthenticator{principal: principal}, "https://admin.example.test/admin-mcp")
	runtime.catalogPromotions = &catalogpromotion.Service{}
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil), ImageUploads: &fakeImageUploads{}}
	require.NoError(t, AttachWrites(runtime, &StaffOAuth{Approval: &StaffProposalApproval{}}, deps, WriteConfig{Enabled: true, Operations: operations}))
	handler := runtime.Handler()

	tools := []json.RawMessage{}
	cursor := ""
	for page := 0; ; page++ {
		require.Less(t, page, 20, "tools/list pagination does not terminate")
		params := `{}`
		if cursor != "" {
			encoded, err := json.Marshal(map[string]string{"cursor": cursor})
			require.NoError(t, err)
			params = string(encoded)
		}
		request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":`+params+`}`))
		request.Header.Set("Authorization", "Bearer test-token")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		var message struct {
			Result struct {
				Tools      []json.RawMessage `json:"tools"`
				NextCursor string            `json:"nextCursor"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
		tools = append(tools, message.Result.Tools...)
		if message.Result.NextCursor == "" {
			return tools
		}
		cursor = message.Result.NextCursor
	}
}

// checkMCPToolDefinition mirrors the MCP TypeScript SDK's Tool validation,
// which rejects the whole tools/list result when one tool fails: a non-empty
// name, object input and output schemas with type "object", object values
// under properties, and a string array for required. Each schema must also
// compile as JSON Schema 2020-12.
func checkMCPToolDefinition(raw json.RawMessage) error {
	var tool map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tool); err != nil {
		return fmt.Errorf("tool is not an object: %w", err)
	}
	var name string
	if err := json.Unmarshal(tool["name"], &name); err != nil || name == "" {
		return fmt.Errorf("tool has no name")
	}
	if _, ok := tool["inputSchema"]; !ok {
		return fmt.Errorf("%s: inputSchema is missing", name)
	}
	for _, field := range []string{"inputSchema", "outputSchema"} {
		schemaJSON, ok := tool[field]
		if !ok {
			continue
		}
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(schemaJSON, &schema); err != nil || schema == nil {
			return fmt.Errorf("%s: %s is not an object", name, field)
		}
		if string(schema["type"]) != `"object"` {
			return fmt.Errorf("%s: %s.type is %s, want \"object\"", name, field, schema["type"])
		}
		if properties, ok := schema["properties"]; ok {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(properties, &values); err != nil || values == nil {
				return fmt.Errorf("%s: %s.properties is not an object", name, field)
			}
			for property, value := range values {
				trimmed := bytes.TrimSpace(value)
				if len(trimmed) == 0 || trimmed[0] != '{' {
					return fmt.Errorf("%s: %s.properties.%s is %s, want a JSON object", name, field, property, trimmed)
				}
			}
		}
		if required, ok := schema["required"]; ok {
			var names []string
			if err := json.Unmarshal(required, &names); err != nil {
				return fmt.Errorf("%s: %s.required is not a string array", name, field)
			}
		}
		document, err := jsonschemav6.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err != nil {
			return fmt.Errorf("%s: decode %s: %w", name, field, err)
		}
		compiler := jsonschemav6.NewCompiler()
		compiler.DefaultDraft(jsonschemav6.Draft2020)
		url := "mem://" + name + "/" + field
		if err := compiler.AddResource(url, document); err != nil {
			return fmt.Errorf("%s: add %s: %w", name, field, err)
		}
		if _, err := compiler.Compile(url); err != nil {
			return fmt.Errorf("%s: %s is not valid JSON Schema: %w", name, field, err)
		}
	}
	return nil
}

func TestToolListSatisfiesMCPToolSchema(t *testing.T) {
	t.Parallel()
	tools := listAllAdminTools(t)
	names := map[string]bool{}
	for _, raw := range tools {
		require.NoError(t, checkMCPToolDefinition(raw))
		var tool struct {
			Name string `json:"name"`
		}
		require.NoError(t, json.Unmarshal(raw, &tool))
		names[tool.Name] = true
	}
	// Every write tool is present, including those past the first page.
	for _, name := range []string{"execute_admin_proposal", "get_admin_proposal_status", "prepare_set_organization_feature", "prepare_create_registry_entry", "prepare_update_support_matrix", "create_image_upload_url"} {
		require.True(t, names[name], "%s missing from tools/list", name)
	}
}

func TestToolDefinitionCheckRejectsNonObjectPropertySchemas(t *testing.T) {
	t.Parallel()
	require.NoError(t, checkMCPToolDefinition(json.RawMessage(`{"name":"ok","inputSchema":{"type":"object","properties":{"any":{}}},"outputSchema":{"type":"object","properties":{"preview":{"type":"object"}}}}`)))
	for _, value := range []string{"true", "false", "null"} {
		for _, field := range []string{"inputSchema", "outputSchema"} {
			tool := `{"name":"bad","inputSchema":{"type":"object"},"` + field + `":{"type":"object","properties":{"preview":` + value + `}}}`
			require.ErrorContains(t, checkMCPToolDefinition(json.RawMessage(tool)), "want a JSON object", "%s under %s", value, field)
		}
	}
}

func TestAuditLogEntriesListedSchema(t *testing.T) {
	t.Parallel()
	for _, raw := range listAllAdminTools(t) {
		var tool struct {
			Name         string         `json:"name"`
			InputSchema  map[string]any `json:"inputSchema"`
			OutputSchema map[string]any `json:"outputSchema"`
			Annotations  struct {
				ReadOnly bool `json:"readOnlyHint"`
			} `json:"annotations"`
		}
		require.NoError(t, json.Unmarshal(raw, &tool))
		if tool.Name != "list_audit_log_entries" {
			continue
		}
		require.True(t, tool.Annotations.ReadOnly)
		inputs, ok := tool.InputSchema["properties"].(map[string]any)
		require.True(t, ok)
		require.Len(t, inputs, 10)
		for _, name := range []string{"organization_id", "cursor", "project_id", "actor_id", "action", "subject_type", "subject_id", "acting_surface", "from", "to"} {
			require.Contains(t, inputs, name)
			field, ok := inputs[name].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "string", field["type"])
		}
		require.Equal(t, []any{"organization_id"}, tool.InputSchema["required"])
		properties, ok := tool.OutputSchema["properties"].(map[string]any)
		require.True(t, ok)
		require.Len(t, properties, 3)
		entries, ok := properties["entries"].(map[string]any)
		require.True(t, ok)
		item, ok := entries["items"].(map[string]any)
		require.True(t, ok)
		fields, ok := item["properties"].(map[string]any)
		require.True(t, ok)
		require.Len(t, fields, 18)
		for _, name := range []string{"id", "project_id", "project_slug", "actor_id", "actor_type", "actor_display_name", "actor_slug", "action", "acting_surface", "acting_client_id", "subject_id", "subject_type", "subject_display_name", "subject_slug", "before_snapshot", "after_snapshot", "metadata", "created_at"} {
			require.Contains(t, fields, name)
		}
		for _, name := range []string{"before_snapshot", "after_snapshot"} {
			// An unconstrained JSON schema accepts objects, arrays and scalar JSON,
			// unlike the byte-array/string schemas that would corrupt snapshots.
			snapshot, ok := fields[name].(map[string]any)
			require.True(t, ok)
			require.NotContains(t, snapshot, "type")
			require.NotContains(t, snapshot, "contentEncoding")
		}
		return
	}
	t.Fatal("list_audit_log_entries missing from tools/list")
}
