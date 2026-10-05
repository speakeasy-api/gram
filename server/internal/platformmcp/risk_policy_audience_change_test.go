package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/stretchr/testify/require"
)

func TestRiskPolicyAudienceDeltaValidation(t *testing.T) {
	t.Parallel()
	valid := changeRiskPolicyAudienceInput{Confirmed: true, AddPrincipals: []string{"user:new", "role:organization:11111111-1111-4111-8111-111111111111"}, RemovePrincipals: []string{"user:old"}}
	principals, err := validateRiskPolicyAudienceDelta(valid)
	require.NoError(t, err)
	require.Len(t, principals, 3)
	require.Equal(t, "user:old", principals[2].String(), "removals must be included in live org validation")
	for _, test := range []struct {
		name   string
		change func(*changeRiskPolicyAudienceInput)
	}{
		{"unconfirmed", func(v *changeRiskPolicyAudienceInput) { v.Confirmed = false }},
		{"missing add", func(v *changeRiskPolicyAudienceInput) { v.AddPrincipals = nil }},
		{"missing remove", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = nil }},
		{"empty delta", func(v *changeRiskPolicyAudienceInput) { v.AddPrincipals = []string{}; v.RemovePrincipals = []string{} }},
		{"duplicate add", func(v *changeRiskPolicyAudienceInput) { v.AddPrincipals = []string{"user:new", "user:new"} }},
		{"duplicate remove", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = []string{"user:old", "user:old"} }},
		{"overlap", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = []string{"user:new"} }},
		{"everyone add", func(v *changeRiskPolicyAudienceInput) { v.AddPrincipals = []string{"user:all"} }},
		{"everyone remove", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = []string{"user:all"} }},
		{"invalid removal", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = []string{"not-a-principal"} }},
		{"too many adds", func(v *changeRiskPolicyAudienceInput) { v.AddPrincipals = make([]string, 101) }},
		{"too many removals", func(v *changeRiskPolicyAudienceInput) { v.RemovePrincipals = make([]string, 101) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			v := valid
			test.change(&v)
			_, err := validateRiskPolicyAudienceDelta(v)
			require.ErrorIs(t, err, ErrRiskMutationInvalid)
		})
	}
	require.NoError(t, validateRiskPolicyAudienceDeltaTarget("targeted", []string{"user:old", "user:other"}, valid))
	for _, test := range []struct {
		name, kind string
		audience   []string
		input      changeRiskPolicyAudienceInput
	}{
		{"everyone", "everyone", []string{"user:all"}, valid},
		{"missing removal", "targeted", []string{"user:other"}, valid},
		{"existing addition", "targeted", []string{"user:old", "user:new"}, valid},
		{"last principal", "targeted", []string{"user:old"}, changeRiskPolicyAudienceInput{Confirmed: true, AddPrincipals: []string{}, RemovePrincipals: []string{"user:old"}}},
		{"malformed audience", "targeted", []string{"user:old", "user:all"}, valid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, validateRiskPolicyAudienceDeltaTarget(test.kind, test.audience, test.input))
		})
	}
	// Atomic replacement of the last principal is allowed; no transient empty state commits.
	require.NoError(t, validateRiskPolicyAudienceDeltaTarget("targeted", []string{"user:old"}, valid))
}

func TestRiskPolicyAudienceChangeToolContract(t *testing.T) {
	t.Parallel()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	called := false
	handler := func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
		called = true
		return nil, UpdateRiskPolicyToolOutput{}, nil
	}
	registerRiskMutationHandlers(reg, catalog, &RiskMutationHandlers{Controls: &RiskMutationControls{}, ChangeAudience: handler})
	descriptor := descriptorByName(t, reg, operationChangeRiskPolicyAudience)
	require.Equal(t, externalOnly, descriptor.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
	require.True(t, *descriptor.Annotations.DestructiveHint)
	require.True(t, descriptor.Annotations.IdempotentHint)
	for _, entry := range reg.For(AudienceAssistant) {
		require.NotEqual(t, operationChangeRiskPolicyAudience, entry.Name)
	}
	schema := new(jsonschema.Schema)
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	args := map[string]any{"project_slug": "default", "policy_id": "11111111-1111-4111-8111-111111111111", "expected_version": "version", "idempotency_key": "key", "confirmed": true, "add_principals": []any{"user:new"}, "remove_principals": []any{}}
	require.NoError(t, resolved.Validate(args))
	for _, field := range []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "confirmed", "add_principals", "remove_principals"} {
		value := args[field]
		delete(args, field)
		require.Error(t, resolved.Validate(args))
		args[field] = value
	}
	args["confirmed"] = false
	require.Error(t, resolved.Validate(args))
	args["confirmed"] = true
	args["add_principals"] = []any{}
	require.Error(t, resolved.Validate(args))
	args["add_principals"] = []any{"user:new", "user:new"}
	require.Error(t, resolved.Validate(args))
	args["add_principals"] = []any{"user:new"}
	args["patch"] = map[string]any{}
	require.Error(t, resolved.Validate(args))
	delete(args, "patch")
	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("self")), encoded)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRiskPolicyAudienceChangeRejectsAssistantDirectInvocation(t *testing.T) {
	t.Parallel()
	service := new(riskPolicyMutationService)
	// The external identity guard runs before controls or exact policy lookup.
	principal := testRiskPrincipal("self")
	principal.ConnectionID = ""
	principal.Generation = ""
	_, _, err := service.changePolicyAudienceTool(ContextWithPrincipal(t.Context(), principal), nil, map[string]any{})
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "external administrator OAuth")
}

func TestRiskPolicyAudienceReplacementSchemaCombinations(t *testing.T) {
	t.Parallel()
	resolved, err := riskPolicyAudienceReplacementSchema().Resolve(nil)
	require.NoError(t, err)
	for _, test := range []struct {
		kind       string
		principals []any
		valid      bool
	}{
		{"everyone", []any{}, true}, {"everyone", []any{"user:one"}, false}, {"targeted", []any{}, false}, {"targeted", []any{"user:one"}, true},
	} {
		args := map[string]any{"type": test.kind, "principal_urns": test.principals, "confirm": true}
		err := resolved.Validate(args)
		if test.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestRiskPolicyAudienceChangeReceiptResult(t *testing.T) {
	t.Parallel()
	require.True(t, riskMutationOperation(operationChangeRiskPolicyAudience))
	value := ChangeRiskPolicyAudienceReceiptResult{}
	require.Equal(t, operationChangeRiskPolicyAudience, value.riskMutationReceiptOperation())
	_, ok := normalizedRiskMutationReceiptResult(value)
	require.True(t, ok)
	_, ok = normalizedRiskMutationReceiptResult(&value)
	require.True(t, ok)
	require.False(t, validRiskMutationReceiptResult(value))
	value.UpdateRiskPolicyReceiptResult = UpdateRiskPolicyReceiptResult{
		Project: RiskMutationReceiptProject{ID: "11111111-1111-4111-8111-111111111111", Slug: "default"},
		Policy:  RiskPolicyReceiptSummary{ID: "22222222-2222-4222-8222-222222222222", PolicyType: "standard", Action: "flag"},
		Version: "opaque", ResultCategory: "updated",
	}
	payload, err := encodeRiskMutationResult(operationChangeRiskPolicyAudience, value)
	require.NoError(t, err)
	var replay UpdateRiskPolicyReceiptResult
	require.NoError(t, json.Unmarshal(payload, &replay))
	require.Equal(t, value.UpdateRiskPolicyReceiptResult, replay)
	_, err = encodeRiskMutationResult(operationUpdateRiskPolicy, value)
	require.Error(t, err)
	require.NotContains(t, string(payload), "principal")
}

func TestRiskPolicyAudienceChangeRequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	called := false
	registerRiskMutationHandlers(reg, catalog, &RiskMutationHandlers{Controls: &RiskMutationControls{}, ChangeAudience: func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
		called = true
		return nil, UpdateRiskPolicyToolOutput{}, nil
	}})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "self-removal-authz-test", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: operationChangeRiskPolicyAudience, Arguments: map[string]any{"project_slug": "default", "policy_id": "11111111-1111-4111-8111-111111111111", "expected_version": "version", "idempotency_key": "key", "confirmed": true, "add_principals": []string{"user:new"}, "remove_principals": []string{}}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "org:admin")
	require.False(t, called)
}

func TestRiskPolicyAudienceDeltaResultBound(t *testing.T) {
	t.Parallel()
	audience := make([]string, 100)
	for i := range audience {
		audience[i] = fmt.Sprintf("user:member-%d", i)
	}
	input := changeRiskPolicyAudienceInput{Confirmed: true, AddPrincipals: []string{"user:new"}, RemovePrincipals: []string{}}
	require.NoError(t, validateRiskPolicyAudienceDeltaTarget("targeted", audience[:99], input))
	err := validateRiskPolicyAudienceDeltaTarget("targeted", audience, input)
	require.ErrorIs(t, err, ErrRiskMutationInvalid)
	require.ErrorContains(t, err, "between 1 and 100")
	input.RemovePrincipals = []string{audience[0]}
	require.NoError(t, validateRiskPolicyAudienceDeltaTarget("targeted", audience, input))
	// A removal-only request may shrink an existing oversized audience to 100.
	oversized := append(append([]string{}, audience...), "user:extra")
	input.AddPrincipals = []string{}
	require.NoError(t, validateRiskPolicyAudienceDeltaTarget("targeted", oversized, input))
}
