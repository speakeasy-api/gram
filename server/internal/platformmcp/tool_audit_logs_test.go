package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type customerAuditStub struct {
	page     *auditapi.PlatformMCPAuditPage
	err      error
	requests []auditapi.PlatformMCPAuditRequest
}

func TestCustomerAuditTransportServiceRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"forbidden actor", oops.E(oops.CodeForbidden, nil, "private actor"), "permission_denied"},
		{"unauthorized", oops.E(oops.CodeUnauthorized, nil, "private identity"), "permission_denied"},
		{"missing grant", fmt.Errorf("require org read: %w", authz.ErrDenied), "permission_denied"},
		{"missing project", oops.E(oops.CodeNotFound, nil, "private project"), "not_found"},
		{"backend failure", fmt.Errorf("private backend"), "feature_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := newTestMCPServer()
			bindExternalTestPrincipal(server)
			reg := newRegistrar(server)
			reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
			registerAuditLogTools(reg, &customerAuditStub{err: fmt.Errorf("wrapped: %w", tc.err)})
			session := connectTestClient(t, server)
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_audit_log_entries", Arguments: map[string]any{}})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Nil(t, result.StructuredContent)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.Contains(t, string(encoded), tc.code)
			require.NotContains(t, string(encoded), "private")
		})
	}
}

func (s *customerAuditStub) ListPlatformMCPAuditLogs(_ context.Context, request auditapi.PlatformMCPAuditRequest) (*auditapi.PlatformMCPAuditPage, error) {
	s.requests = append(s.requests, request)
	return s.page, s.err
}

func TestCustomerAuditToolTransport(t *testing.T) {
	t.Parallel()
	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reader := &customerAuditStub{page: &auditapi.PlatformMCPAuditPage{Visibility: "self", Logs: []*gen.AuditLog{{
		ID: "event", ActorID: "actor", ActorType: "user", Action: "project:update", ActingSurface: "platform_mcp", SubjectID: "subject", SubjectType: "project",
		BeforeSnapshot: json.RawMessage(`{"name":"before"}`), AfterSnapshot: json.RawMessage(`[true,"after"]`), Metadata: map[string]any{"nested": map[string]any{"value": true}},
	}}, NextCursor: new(audit.EncodeCursor(10, "event"))}}
	registerAuditLogTools(reg, reader)
	stub := newRegistrar(newTestMCPServer())
	registerAuditLogTools(stub, nil)
	live := descriptorByName(t, reg, "list_audit_log_entries")
	unavailable := descriptorByName(t, stub, live.Name)
	require.Equal(t, live.Meta, unavailable.Meta)
	require.Equal(t, live.InputSchema, unavailable.InputSchema)
	require.Equal(t, live.output, unavailable.output)
	require.Equal(t, externalOnly, live.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationMember, live.Meta.Authorization)
	require.Equal(t, ProjectScopeNone, live.Meta.ProjectScope)
	require.Equal(t, discoveryOrgRead, live.Meta.DiscoveryScopes)
	require.Empty(t, reg.For(AudienceAssistant))
	tools := []*mcp.Tool{{Name: live.Name}}
	ctx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, testPrincipal().OrganizationID)})
	require.Len(t, reg.FilterExternalTools(ctx, testPrincipal(), tools), 1)
	require.Empty(t, reg.FilterExternalTools(authz.GrantsToContext(ctx, nil), testPrincipal(), tools))
	session := connectTestClient(t, server)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"before_snapshot":{"name":"before"}`)
	require.Contains(t, string(encoded), `"after_snapshot":[true,"after"]`)
	require.Contains(t, string(encoded), `"metadata":{"nested":{"value":true}}`)
	require.Len(t, reader.requests, 1)
	require.Equal(t, testPrincipal().OrganizationID, reader.requests[0].OrganizationID)
	require.Equal(t, testPrincipal().UserID, reader.requests[0].UserID)
	for _, cursor := range []string{" ", "not-a-cursor", strings.Repeat("a", 129), " " + audit.EncodeCursor(10, "event")} {
		result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{"cursor": cursor}})
		require.NoError(t, err)
		require.True(t, result.IsError)
	}
	require.Len(t, reader.requests, 1, "invalid cursors never reach the service")
	for _, page := range []*auditapi.PlatformMCPAuditPage{
		nil,
		{Visibility: "invalid"},
		{Visibility: "self", Logs: []*gen.AuditLog{nil}},
		{Visibility: "self", Logs: make([]*gen.AuditLog, 51)},
		{Visibility: "self", Logs: []*gen.AuditLog{{BeforeSnapshot: json.RawMessage(`{`)}}},
		{Visibility: "self", Logs: []*gen.AuditLog{{Metadata: map[string]any{"invalid": make(chan int)}}}},
	} {
		reader.page = page
		result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Nil(t, result.StructuredContent)
	}
	for _, count := range []int{0, 50} {
		reader.page = &auditapi.PlatformMCPAuditPage{Visibility: "self", Logs: make([]*gen.AuditLog, count)}
		for i := range reader.page.Logs {
			reader.page.Logs[i] = &gen.AuditLog{}
		}
		result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, result.IsError)
	}
	reader.page = &auditapi.PlatformMCPAuditPage{Visibility: "self", Logs: []*gen.AuditLog{{}}, NextCursor: new(audit.EncodeCursor(10, "event"))}
	bounded, err := customerAuditOutput(testPrincipal().OrganizationID, reader.page)
	require.NoError(t, err)
	encoded, err = json.Marshal(bounded)
	require.NoError(t, err)
	reader.page.Logs[0].SubjectID = strings.Repeat("x", 65536-len(encoded))
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	encoded, err = json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.Len(t, encoded, 65536)
	reader.page.Logs[0].SubjectID += "x"
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Nil(t, result.StructuredContent)
	reader.err = fmt.Errorf("private backend cause")
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: live.Name, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	encoded, err = json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private backend cause")
	require.NotContains(t, string(encoded), "before_snapshot")
	require.NotContains(t, string(encoded), "next_cursor")
}

func TestCustomerAuditCompletePageValidation(t *testing.T) {
	t.Parallel()
	for _, page := range []*auditapi.PlatformMCPAuditPage{
		nil,
		{Visibility: "invalid"},
		{Visibility: "self", Logs: []*gen.AuditLog{nil}},
		{Visibility: "self", Logs: make([]*gen.AuditLog, 51)},
		{Visibility: "self", NextCursor: new(" ")},
		{Visibility: "self", Logs: []*gen.AuditLog{{BeforeSnapshot: json.RawMessage(`{`)}}},
		{Visibility: "self", Logs: []*gen.AuditLog{{Metadata: map[string]any{"invalid": make(chan int)}}}},
	} {
		output, err := customerAuditOutput("organization", page)
		require.Error(t, err)
		require.Empty(t, output.Entries)
		require.Nil(t, output.NextCursor)
	}
	for _, count := range []int{0, 50} {
		page := &auditapi.PlatformMCPAuditPage{Visibility: "self", Logs: make([]*gen.AuditLog, count)}
		for i := range page.Logs {
			page.Logs[i] = &gen.AuditLog{}
		}
		output, err := customerAuditOutput("organization", page)
		require.NoError(t, err)
		require.Len(t, output.Entries, count)
	}
	page := &auditapi.PlatformMCPAuditPage{Visibility: "self", Logs: []*gen.AuditLog{{SubjectID: ""}}, NextCursor: new(audit.EncodeCursor(10, "event"))}
	output, err := customerAuditOutput("organization", page)
	require.NoError(t, err)
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	page.Logs[0].SubjectID = strings.Repeat("x", 65536-len(encoded))
	output, err = customerAuditOutput("organization", page)
	require.NoError(t, err)
	encoded, err = json.Marshal(output)
	require.NoError(t, err)
	require.Len(t, encoded, 65536)
	page.Logs[0].SubjectID += "x"
	output, err = customerAuditOutput("organization", page)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "page_too_large", refusal.Code)
	require.Empty(t, output.Entries)
	require.Nil(t, output.NextCursor)
}

func TestCustomerAuditUnavailableTransportSchemaParity(t *testing.T) {
	t.Parallel()
	var schemas []string
	for _, reader := range []CustomerAuditReader{nil, &customerAuditStub{page: &auditapi.PlatformMCPAuditPage{Visibility: "self"}}} {
		server := newTestMCPServer()
		bindExternalTestPrincipal(server)
		reg := newRegistrar(server)
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
		registerAuditLogTools(reg, reader)
		session := connectTestClient(t, server)
		listed, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, listed.Tools, 1)
		encoded, err := json.Marshal(listed.Tools[0])
		require.NoError(t, err)
		var schema struct {
			InputSchema struct {
				Properties map[string]any `json:"properties"`
			} `json:"inputSchema"`
		}
		require.NoError(t, json.Unmarshal(encoded, &schema))
		require.Len(t, schema.InputSchema.Properties, 9)
		for _, field := range []string{"cursor", "project_id", "actor_id", "action", "subject_type", "subject_id", "acting_surface", "from", "to"} {
			require.Contains(t, schema.InputSchema.Properties, field)
		}
		schemas = append(schemas, string(encoded))
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_audit_log_entries", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.Equal(t, reader == nil, result.IsError)
	}
	require.JSONEq(t, schemas[0], schemas[1])
}

func TestCustomerAuditTransportTypedFilters(t *testing.T) {
	t.Parallel()
	server := newTestMCPServer()
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reader := &customerAuditStub{page: &auditapi.PlatformMCPAuditPage{Visibility: "self"}}
	registerAuditLogTools(reg, reader)
	session := connectTestClient(t, server)
	project := uuid.New()
	cursor := audit.EncodeCursor(123, "event")
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_audit_log_entries", Arguments: map[string]any{
		"cursor": cursor, "project_id": project.String(), "actor_id": testPrincipal().UserID,
		"action": "future:action", "subject_type": "assistant", "subject_id": "subject", "acting_surface": "future_surface",
		"from": "2026-01-01T02:00:00+02:00", "to": "2026-01-02T02:00:00+02:00",
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, reader.requests, 1)
	require.Equal(t, auditapi.PlatformMCPAuditRequest{
		OrganizationID: testPrincipal().OrganizationID, UserID: testPrincipal().UserID, Cursor: cursor,
		ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ActorID: testPrincipal().UserID,
		Action: "future:action", SubjectType: "assistant", SubjectID: "subject", ActingSurface: "future_surface",
		From: new(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), To: new(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)),
	}, reader.requests[0])
}

func TestCustomerAuditTransportFilterGuards(t *testing.T) {
	t.Parallel()
	canonical := "abcdef12-3456-4789-abcd-0123456789ab"
	type guardCase struct {
		name  string
		args  map[string]any
		valid bool
	}
	cases := []guardCase{
		{"empty", map[string]any{"cursor": "", "project_id": "", "actor_id": "", "action": "", "subject_type": "", "subject_id": "", "acting_surface": "", "from": "", "to": ""}, true},
		{"canonical project", map[string]any{"project_id": canonical}, true},
		{"uppercase project", map[string]any{"project_id": strings.ToUpper(canonical)}, false},
		{"compact project", map[string]any{"project_id": strings.ReplaceAll(canonical, "-", "")}, false},
		{"URN project", map[string]any{"project_id": "urn:uuid:" + canonical}, false},
		{"padded project", map[string]any{"project_id": " " + canonical}, false},
		{"project slug", map[string]any{"project_id": "default"}, false},
		{"blank project", map[string]any{"project_id": " "}, false},
		{"equal times", map[string]any{"from": "2026-01-01T00:00:00Z", "to": "2026-01-01T00:00:00Z"}, false},
		{"inverted times", map[string]any{"from": "2026-01-02T00:00:00Z", "to": "2026-01-01T00:00:00Z"}, false},
		{"invalid cursor", map[string]any{"cursor": "invalid"}, false},
		{"cursor byte boundary", map[string]any{"cursor": audit.EncodeCursor(1, strings.Repeat("x", 94))}, true},
		{"cursor overflow", map[string]any{"cursor": audit.EncodeCursor(1, strings.Repeat("x", 95))}, false},
		{"padded cursor", map[string]any{"cursor": audit.EncodeCursor(1, "event") + " "}, false},
	}
	for _, field := range []string{"actor_id", "action", "subject_type", "subject_id", "acting_surface"} {
		for _, value := range []struct {
			name, value string
			valid       bool
		}{
			{"exact bytes", strings.Repeat("x", 256), true},
			{"multibyte exact", strings.Repeat("\u00e9", 128), true},
			{"overflow", strings.Repeat("x", 257), false},
			{"multibyte overflow", strings.Repeat("\u00e9", 129), false},
			{"blank", " \t", false}, {"leading padding", " value", false}, {"trailing padding", "value ", false},
		} {
			cases = append(cases, guardCase{field + " " + value.name, map[string]any{field: value.value}, value.valid})
		}
	}
	for _, field := range []string{"from", "to"} {
		for _, value := range []struct {
			name, value string
			valid       bool
		}{
			{"exact bytes", "2026-01-01T00:00:00." + strings.Repeat("1", 43) + "Z", true},
			{"parseable overflow", "2026-01-01T00:00:00." + strings.Repeat("1", 44) + "Z", false},
			{"leading padding", " 2026-01-01T00:00:00Z", false},
			{"trailing padding", "2026-01-01T00:00:00Z ", false},
		} {
			// A malformed timestamp would not prove these pre-parser guards work.
			parsed, _, err := conv.ParseOptionalTimeWindow(&value.value, nil)
			require.NoError(t, err)
			require.NotNil(t, parsed)
			cases = append(cases, guardCase{field + " " + value.name, map[string]any{field: value.value, "project_id": canonical}, value.valid})
		}
		cases = append(cases, guardCase{field + " blank", map[string]any{field: " "}, false}, guardCase{field + " malformed", map[string]any{field: "not-a-time"}, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := newTestMCPServer()
			bindExternalTestPrincipal(server)
			reg := newRegistrar(server)
			reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
			reader := &customerAuditStub{page: &auditapi.PlatformMCPAuditPage{Visibility: "self"}}
			registerAuditLogTools(reg, reader)
			session := connectTestClient(t, server)
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_audit_log_entries", Arguments: tc.args})
			require.NoError(t, err)
			require.Equal(t, !tc.valid, result.IsError)
			if tc.valid {
				require.Len(t, reader.requests, 1)
			} else {
				require.Empty(t, reader.requests, "invalid inputs refuse before project or history reads")
				require.Nil(t, result.StructuredContent)
			}
		})
	}
}
