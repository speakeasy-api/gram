//nolint:exhaustruct // MCP SDK manifests and JSON schemas rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/presets"
)

// registerRiskToolsWithMutations is the single handler-selection seam used by
// the external endpoint and assistant catalogue. Policy callbacks may be live
// while exclusion callbacks remain stable unavailable stubs during rollout.
func registerRiskToolsWithMutations(reg *Registrar, risk *RiskReadService, status *RiskAnalysisStatusService, mutations *RiskMutationHandlers) {
	// The analysis status read does not depend on the policy reader, so it is
	// registered on its own: a deployment whose policy reads are unavailable
	// still answers "when did the Watchdog last run" when it can.
	registerRiskAnalysisStatusTool(reg, status)
	if risk == nil || !risk.valid() {
		registerUnavailableRiskToolsWithMutations(reg, mutations)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        "list_risk_policies",
		Title:       "List Risk Policies",
		Description: "List bounded, privacy-safe risk policy summaries in an exact project or the organization's literal default project. Blocking Shadow MCP policies include their configured default posture and distinct explicit target counts, not effective access.",
		Annotations: readOnlyAnnotations(),
		InputSchema: riskListSchema(false),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListRiskPoliciesInput) (*mcp.CallToolResult, ListRiskPoliciesOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, "list_risk_policies", func(principal Principal) (ListRiskPoliciesOutput, error) {
			return risk.ListPolicies(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        "get_risk_policy",
		Title:       "Get Risk Policy",
		Description: "Read one risk policy from an exact project or the organization's literal default project, with closed compatibility metadata. Shadow target counts describe stored policy configuration, not effective access.",
		Annotations: readOnlyAnnotations(),
		InputSchema: riskGetPolicySchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetRiskPolicyInput) (*mcp.CallToolResult, GetRiskPolicyOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, "get_risk_policy", func(principal Principal) (GetRiskPolicyOutput, error) {
			return risk.GetPolicy(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        "list_risk_exclusions",
		Title:       "List Risk Exclusions",
		Description: "List bounded, privacy-safe risk exclusions in an exact project or the organization's literal default project. Exact and legacy regex values are never returned.",
		Annotations: readOnlyAnnotations(),
		InputSchema: riskListSchema(true),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListRiskExclusionsInput) (*mcp.CallToolResult, ListRiskExclusionsOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, "list_risk_exclusions", func(principal Principal) (ListRiskExclusionsOutput, error) {
			return risk.ListExclusions(ctx, principal, input)
		})
	})
	registerRiskMutationHandlers(reg, risk.catalog, true, mutations)
}

const (
	riskAnalysisStatusToolName  = "get_risk_analysis_status"
	riskAnalysisStatusToolTitle = "Get Watchdog Analysis Status"
	// riskAnalysisStatusToolStub is the description served when the analysis
	// run state cannot be described in this deployment.
	riskAnalysisStatusToolStub = "Report when the Watchdog analysis last ran for a project. Analysis status is unavailable in this deployment."
)

// registerRiskAnalysisStatusTool serves get_risk_analysis_status live when the
// analysis run state can be described, and as a stub otherwise, so the tool
// always exists in the manifest.
func registerRiskAnalysisStatusTool(reg *Registrar, status *RiskAnalysisStatusService) {
	if !status.valid() {
		addTool(reg, &mcp.Tool{Name: riskAnalysisStatusToolName, Title: riskAnalysisStatusToolTitle, Description: riskAnalysisStatusToolStub, Annotations: readOnlyAnnotations(), InputSchema: riskAnalysisStatusSchema()}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, unavailableRiskReadTool(reg, riskAnalysisStatusToolName))
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        riskAnalysisStatusToolName,
		Title:       riskAnalysisStatusToolTitle,
		Description: "Report whether the Watchdog analysis is running for an exact project or the organization's literal default project, and when it last ran. The analysis is event-driven: it starts shortly after new chat traffic is captured rather than on a schedule, so a project with no recent traffic has no recent run. A run that hit a failure waits about five minutes before retrying and still reports as running during that wait. Use this when an administrator asks whether risk findings are up to date or why none have appeared.",
		Annotations: readOnlyAnnotations(),
		InputSchema: riskAnalysisStatusSchema(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetRiskAnalysisStatusInput) (*mcp.CallToolResult, GetRiskAnalysisStatusOutput, error) {
		return riskReadToolCall(ctx, reg.riskTelemetry, riskAnalysisStatusToolName, func(principal Principal) (GetRiskAnalysisStatusOutput, error) {
			return status.Get(ctx, principal, input)
		})
	})
}

func registerUnavailableRiskTools(reg *Registrar) {
	registerRiskAnalysisStatusTool(reg, nil)
	registerUnavailableRiskToolsWithMutations(reg, nil)
}

func registerUnavailableRiskToolsWithMutations(reg *Registrar, mutations *RiskMutationHandlers) {
	registerUnavailableRiskToolsWithCatalogAndMutations(reg, policycatalog.Build, mutations)
}

func registerUnavailableRiskToolsWithCatalog(reg *Registrar, buildCatalog func() (policycatalog.Catalog, error)) {
	registerUnavailableRiskToolsWithCatalogAndMutations(reg, buildCatalog, nil)
}

func registerUnavailableRiskToolsWithCatalogAndMutations(reg *Registrar, buildCatalog func() (policycatalog.Catalog, error), mutations *RiskMutationHandlers) {
	for _, tool := range []struct {
		name, title, description string
		schema                   *jsonschema.Schema
	}{
		{"list_risk_policies", "List Risk Policies", "List risk policy summaries. Risk reads are unavailable in this deployment.", riskListSchema(false)},
		{"get_risk_policy", "Get Risk Policy", "Read one risk policy. Risk reads are unavailable in this deployment.", riskGetPolicySchema()},
		{"list_risk_exclusions", "List Risk Exclusions", "List risk exclusions. Risk reads are unavailable in this deployment.", riskListSchema(true)},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description, Annotations: readOnlyAnnotations(), InputSchema: tool.schema}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}, unavailableRiskReadTool(reg, tool.name))
	}
	catalog, err := buildCatalog()
	registerRiskMutationHandlers(reg, catalog, err == nil, mutations)
}

type RiskMutationToolReceipt struct {
	ID       string `json:"id"`
	Replayed bool   `json:"replayed"`
}

type CreateRiskPolicyToolOutput struct {
	CreateRiskPolicyReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type UpdateRiskPolicyToolOutput struct {
	UpdateRiskPolicyReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type CreateRiskExclusionToolOutput struct {
	CreateRiskExclusionReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type UpdateRiskExclusionToolOutput struct {
	UpdateRiskExclusionReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

// RiskMutationHandlers names the four independently selectable callbacks.
// Every callback has an exported success type that composition code can
// construct, while the schemas remain owned by this package.
type RiskMutationHandlers struct {
	Controls        *RiskMutationControls
	CreatePolicy    mcp.ToolHandlerFor[map[string]any, CreateRiskPolicyToolOutput]
	UpdatePolicy    mcp.ToolHandlerFor[map[string]any, UpdateRiskPolicyToolOutput]
	CreateExclusion mcp.ToolHandlerFor[map[string]any, CreateRiskExclusionToolOutput]
	UpdateExclusion mcp.ToolHandlerFor[map[string]any, UpdateRiskExclusionToolOutput]
}

func registerRiskMutationHandlers(reg *Registrar, catalog policycatalog.Catalog, catalogAvailable bool, handlers *RiskMutationHandlers) {
	createPolicySchema := fallbackCreateRiskPolicySchema()
	updatePolicySchema := fallbackUpdateRiskPolicySchema()
	createExclusionSchema := fallbackCreateRiskExclusionSchema()
	if catalogAvailable {
		createPolicySchema = createRiskPolicySchema(catalog)
		updatePolicySchema = updateRiskPolicySchema(catalog)
		createExclusionSchema = createRiskExclusionSchema(catalog)
	}
	createPolicy := unavailableRiskMutationTool[CreateRiskPolicyToolOutput]()
	updatePolicy := unavailableRiskMutationTool[UpdateRiskPolicyToolOutput]()
	createExclusion := unavailableRiskMutationTool[CreateRiskExclusionToolOutput]()
	updateExclusion := unavailableRiskMutationTool[UpdateRiskExclusionToolOutput]()
	createPolicyDescription := "Create a risk policy in an explicit project. Risk policy mutations are not enabled in this rollout."
	updatePolicyDescription := "Patch a risk policy in an explicit project. Risk policy mutations are not enabled in this rollout."
	createExclusionDescription := "Create a non-regex risk exclusion in an explicit project. Exclusion mutations are not enabled in this rollout."
	updateExclusionDescription := "Enable or disable one risk exclusion without changing its definition. Exclusion mutations are not enabled in this rollout."
	if catalogAvailable && handlers != nil && handlers.Controls != nil {
		if handlers.CreatePolicy != nil {
			createPolicy = handlers.CreatePolicy
			createPolicyDescription = "Create a risk policy in an explicit project with idempotent replay safety: a standard policy from allowlisted detectors, a prompt-based policy from a judge instruction, or a policy expanded from a use-case preset (see list_risk_presets and suggest_risk_policy) with optional overrides. A preset policy is enabled unless enabled is false."
		}
		if handlers.UpdatePolicy != nil {
			updatePolicy = handlers.UpdatePolicy
			updatePolicyDescription = "Patch allowlisted fields on a risk policy in an explicit project using an opaque expected version; omitted fields are preserved."
		}
		if handlers.CreateExclusion != nil {
			createExclusion = handlers.CreateExclusion
			createExclusionDescription = "Create a non-regex risk exclusion in an explicit project with idempotent replay safety."
		}
		if handlers.UpdateExclusion != nil {
			updateExclusion = handlers.UpdateExclusion
			updateExclusionDescription = "Enable or disable one risk exclusion without changing its definition using an opaque expected version."
		}
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{Name: "create_risk_policy", Title: "Create Risk Policy", Description: createPolicyDescription, InputSchema: createPolicySchema}, meta, instrumentRiskMutation(reg, "create_risk_policy", createPolicy))
	addTool(reg, &mcp.Tool{Name: "update_risk_policy", Title: "Update Risk Policy", Description: updatePolicyDescription, InputSchema: updatePolicySchema}, meta, instrumentRiskMutation(reg, "update_risk_policy", updatePolicy))
	addTool(reg, &mcp.Tool{Name: "create_risk_exclusion", Title: "Create Risk Exclusion", Description: createExclusionDescription, InputSchema: createExclusionSchema}, meta, instrumentRiskMutation(reg, "create_risk_exclusion", createExclusion))
	addTool(reg, &mcp.Tool{Name: "update_risk_exclusion", Title: "Update Risk Exclusion", Description: updateExclusionDescription, InputSchema: updateRiskExclusionSchema()}, meta, instrumentRiskMutation(reg, "update_risk_exclusion", updateExclusion))
}

func unavailableRiskMutationTool[Out any]() mcp.ToolHandlerFor[map[string]any, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, Out, error) {
		var zero Out
		result := featureUnavailableResult{Code: unavailableCode, Feature: "risk_mutations", Message: "This Platform MCP capability is not enabled for the current rollout."}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, zero, fmt.Errorf("encode unavailable risk mutation result: %w", err)
		}
		// Return a tool error rather than an existing IsError result. The MCP SDK
		// short-circuits on errors before marshaling the typed zero Out value, so
		// disabled tools emit no empty structured success fields.
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: string(content)}
	}
}

func instrumentRiskMutation[Out any](reg *Registrar, tool string, handler mcp.ToolHandlerFor[map[string]any, Out]) mcp.ToolHandlerFor[map[string]any, Out] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input map[string]any) (*mcp.CallToolResult, Out, error) {
		started := time.Now()
		result, output, err := handler(ctx, request, input)
		event := riskTelemetryEvent(tool, riskMutationTelemetryOutcome(err))
		if err == nil {
			event = riskMutationSuccessEvent(tool, output)
		}
		reg.riskTelemetry.Record(ctx, event, time.Since(started))
		return result, output, err
	}
}

func riskMutationTelemetryOutcome(err error) string {
	if err == nil {
		return "succeeded"
	}
	var refusal *ToolRefusalError
	if errors.As(err, &refusal) && validRiskTelemetryOutcome(refusal.Code) {
		return refusal.Code
	}
	var mutation *RiskMutationError
	if errors.As(err, &mutation) && validRiskTelemetryOutcome(mutation.Code) {
		return mutation.Code
	}
	return "unavailable"
}

func riskMutationSuccessEvent[Out any](tool string, output Out) RiskToolEvent {
	event := riskTelemetryEvent(tool, "succeeded")
	event.Replay = riskTelemetryFresh
	switch typed := any(output).(type) {
	case CreateRiskPolicyToolOutput:
		event.Replay = riskMutationReplayState(typed.Receipt.Replayed, typed.MatchedExisting)
	case UpdateRiskPolicyToolOutput:
		event.Replay = riskMutationReplayState(typed.Receipt.Replayed, false)
	case CreateRiskExclusionToolOutput:
		event.Replay = riskMutationReplayState(typed.Receipt.Replayed, typed.MatchedExisting)
		event.Reconciliation = typed.Reconciliation
	case UpdateRiskExclusionToolOutput:
		event.Replay = riskMutationReplayState(typed.Receipt.Replayed, false)
		event.Reconciliation = typed.Reconciliation
	}
	return event
}

func riskMutationReplayState(replayed, matched bool) string {
	switch {
	case replayed:
		return riskTelemetryReceiptReplay
	case matched:
		return riskTelemetryMatched
	default:
		return riskTelemetryFresh
	}
}

func unavailableRiskReadTool(reg *Registrar, tool string) mcp.ToolHandlerFor[map[string]any, featureUnavailableResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, featureUnavailableResult, error) {
		started := time.Now()
		result := featureUnavailableResult{Code: unavailableCode, Feature: "risk_reads", Message: "This is not switched on for your organization yet."}
		reg.riskTelemetry.Record(ctx, riskTelemetryEvent(tool, result.Code), time.Since(started))
		content, err := json.Marshal(result)
		if err != nil {
			return nil, featureUnavailableResult{}, fmt.Errorf("encode unavailable risk read result: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, result, nil
	}
}

func riskReadToolCall[Out any](ctx context.Context, telemetry RiskTelemetry, tool string, call func(principal Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	started := time.Now()
	event := riskTelemetryEvent(tool, "unavailable")
	defer func() { telemetry.Record(ctx, event, time.Since(started)) }()

	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err == nil {
		event.Outcome = "succeeded"
		return nil, output, nil
	}
	if errors.Is(err, ErrOperationRateLimited) || errors.Is(err, ErrOperationBudgetUnavailable) {
		if errors.Is(err, ErrOperationRateLimited) {
			event.Outcome = "rate_limited"
		}
		result, _ := operationBudgetToolResult(err)
		return result, zero, nil
	}
	var refusal featureUnavailableResult
	switch {
	case errors.Is(err, ErrRiskReadInvalid), errors.Is(err, ErrRiskCursorInvalid):
		refusal = featureUnavailableResult{Code: "invalid_request", Feature: "risk_reads", Message: "The risk read input or cursor is invalid. Re-read the tool schema and restart pagination."}
	case errors.Is(err, ErrRiskReadNotFound):
		refusal = featureUnavailableResult{Code: "not_found", Feature: "risk_reads", Message: "The requested project or risk resource is not available to this organization."}
	case errors.Is(err, ErrRiskFeatureNotEnabled):
		capability := "risk_reads"
		if tool == riskAnalysisStatusToolName {
			capability = "risk_analysis_status"
		}
		refusal = featureUnavailableResult{Code: unavailableCode, Feature: capability, Message: "This is not switched on for your organization yet."}
	case errors.Is(err, ErrUnavailable):
		refusal = featureUnavailableResult{Code: unavailableCode, Feature: "risk_reads", Message: "Risk reads are temporarily unavailable."}
	default:
		return nil, zero, err
	}
	event.Outcome = refusal.Code
	if errors.Is(err, ErrUnavailable) {
		event.Outcome = "unavailable"
	}
	content, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		event.Outcome = "unavailable"
		return nil, zero, fmt.Errorf("encode risk read refusal: %w", marshalErr)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, zero, nil
}

func riskListSchema(withPolicy bool) *jsonschema.Schema {
	properties := map[string]*jsonschema.Schema{
		"cursor": {Type: "string", Description: "Opaque cursor returned by the previous page."},
		"limit":  {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(riskReadPageSize)), Description: "Page size; defaults to 50 and cannot exceed 50."},
	}
	if withPolicy {
		properties["policy_id"] = uuidSchema("Optional exact policy ID filter.")
	}
	return projectSelectorSchema(properties, nil)
}

func riskAnalysisStatusSchema() *jsonschema.Schema {
	return projectSelectorSchema(map[string]*jsonschema.Schema{}, nil)
}

func riskGetPolicySchema() *jsonschema.Schema {
	return projectSelectorSchema(map[string]*jsonschema.Schema{
		"policy_id": uuidSchema("Exact policy ID to read."),
	}, []string{"policy_id"})
}

func createRiskPolicySchema(catalog policycatalog.Catalog) *jsonschema.Schema {
	standard := riskPolicyCreateCommonProperties(catalog)
	standard["policy_type"] = describedConstSchema("standard", policycatalog.PolicyTypeDescriptions["standard"])
	standard["sources"] = describedArraySchema(describedCatalogEnumSchema(catalog, catalog.Sources, "Detector sources to enable. One of:", func(value string) string { return policycatalog.SourceDescriptions[value] }), 1)
	standard["presidio_entities"] = describedArraySchema(describedCatalogEnumSchema(catalog, catalog.PresidioEntities, "Personal-data entities to detect; required when sources include presidio. One of:", policycatalog.PresidioEntityDescription), 0)
	standard["presidio_score_threshold"] = &jsonschema.Schema{Type: "number", Minimum: new(float64(0)), Maximum: new(float64(1)), Description: "Minimum Presidio confidence (0-1) a personal-data match must clear; default 0.5."}
	standard["prompt_injection_rules"] = arraySchema(catalogEnumSchema(catalog, catalog.PromptInjectionRules), 0, true)
	standard["disabled_rules"] = arraySchema(catalogEnumSchema(catalog, catalog.DisabledRules), 0, true)
	standard["disabled_rules"].Description = "Individual rules to switch off within the enabled sources, such as secret.aws-access-key or pii.email_address. Leave empty to keep every rule on."
	standard["approved_email_domains"] = boundedArraySchema(stringSchema("Canonical email domain.", 1, 253), 0, 50, true)
	standard["approved_email_domains"].Description = "Email domains treated as corporate by the account_identity source; sessions from other domains are flagged."
	standard["detection_scopes"] = detectionScopesSchema(catalog)

	prompt := riskPolicyCreateCommonProperties(catalog)
	prompt["policy_type"] = describedConstSchema("prompt_based", policycatalog.PolicyTypeDescriptions["prompt_based"])
	prompt["prompt"] = stringSchema("Prompt-policy instruction the judge applies to each in-scope message; never logged or placed in receipts.", 1, 4000)

	preset := riskPolicyCreateCommonProperties(catalog)
	preset["preset"] = describedEnumSchema(presets.IDs(), "Use-case preset to expand into a complete policy; see list_risk_presets. One of:", riskPresetDescription)
	preset["name"] = stringSchema("Policy name; defaults to the preset label.", 1, 100)
	preset["presidio_entities"] = describedArraySchema(describedCatalogEnumSchema(catalog, catalog.PresidioEntities, "Replaces the preset's personal-data entities. One of:", policycatalog.PresidioEntityDescription), 1)
	preset["approved_email_domains"] = boundedArraySchema(stringSchema("Canonical email domain.", 1, 253), 0, 50, true)
	preset["approved_email_domains"].Description = "Email domains treated as corporate; required for the non_corporate_accounts preset to detect anything."
	preset["prompt"] = stringSchema("Replaces the preset's judge instruction; only for prompt-based presets.", 1, 4000)
	delete(preset, "enabled")
	preset["enabled"] = &jsonschema.Schema{Type: "boolean", Description: "Defaults to true."}
	return &jsonschema.Schema{Type: "object", OneOf: []*jsonschema.Schema{
		closedObject(standard, []string{"project_slug", "policy_type", "name", "enabled", "sources", "idempotency_key"}),
		closedObject(prompt, []string{"project_slug", "policy_type", "name", "enabled", "prompt", "idempotency_key"}),
		closedObject(preset, []string{"project_slug", "preset", "idempotency_key"}),
	}}
}

func updateRiskPolicySchema(catalog policycatalog.Catalog) *jsonschema.Schema {
	patch := closedObject(map[string]*jsonschema.Schema{
		"name":                     stringSchema("Policy name.", 1, 100),
		"enabled":                  {Type: "boolean"},
		"action":                   catalogEnumSchema(catalog, catalog.Actions),
		"score":                    {Type: "number", Minimum: new(0.1), Maximum: new(float64(10))},
		"prompt":                   stringSchema("Replacement prompt-policy instruction.", 1, 4000),
		"sources":                  arraySchema(catalogEnumSchema(catalog, catalog.Sources), 0, true),
		"presidio_entities":        arraySchema(catalogEnumSchema(catalog, catalog.PresidioEntities), 0, true),
		"presidio_score_threshold": {Type: "number", Minimum: new(float64(0)), Maximum: new(float64(1))},
		"prompt_injection_rules":   arraySchema(catalogEnumSchema(catalog, catalog.PromptInjectionRules), 0, true),
		"disabled_rules":           arraySchema(catalogEnumSchema(catalog, catalog.DisabledRules), 0, true),
		"approved_email_domains":   boundedArraySchema(stringSchema("Canonical email domain.", 1, 253), 0, 50, true),
		"detection_scopes":         detectionScopesSchema(catalog),
		"user_message":             stringSchema("Optional user-facing enforcement message.", 0, 500),
	}, nil)
	patch.MinProperties = new(1)
	return closedObject(map[string]*jsonschema.Schema{
		"project_slug": stringSchema("Exact project slug; writes never default a project.", 1, 0), "policy_id": uuidSchema("Exact policy ID."),
		"expected_version": stringSchema("Opaque version returned by get_risk_policy.", 1, 0), "idempotency_key": stringSchema("Caller replay key.", 1, 128), "patch": patch,
	}, []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "patch"})
}

func createRiskExclusionSchema(catalog policycatalog.Catalog) *jsonschema.Schema {
	common := func(matchType string, matchValue *jsonschema.Schema) map[string]*jsonschema.Schema {
		return map[string]*jsonschema.Schema{
			"project_slug": stringSchema("Exact project slug; writes never default a project.", 1, 0), "policy_id": uuidSchema("Optional policy binding."),
			"match_type": constSchema(matchType), "match_value": matchValue,
			"enabled": {Type: "boolean"}, "idempotency_key": stringSchema("Caller replay key.", 1, 128),
		}
	}
	exact := common("exact", stringSchema("Exact match value; never returned or logged.", 1, 256))
	exact["rule_id_filter"] = catalogEnumSchema(catalog, catalog.DisabledRules)
	exact["source_filter"] = catalogEnumSchema(catalog, catalog.Sources)
	ruleID := common("rule_id", catalogEnumSchema(catalog, catalog.DisabledRules))
	ruleID["source_filter"] = catalogEnumSchema(catalog, catalog.Sources)
	source := common("source", catalogEnumSchema(catalog, catalog.Sources))
	source["rule_id_filter"] = catalogEnumSchema(catalog, catalog.DisabledRules)
	entityType := common("entity_type", catalogEnumSchema(catalog, catalog.PresidioEntities))
	entityType["source_filter"] = constSchema("presidio")

	required := []string{"project_slug", "match_type", "match_value", "enabled", "idempotency_key"}
	return &jsonschema.Schema{Type: "object", OneOf: []*jsonschema.Schema{
		closedObject(exact, required),
		closedObject(ruleID, required),
		closedObject(source, required),
		closedObject(entityType, required),
	}}
}

func updateRiskExclusionSchema() *jsonschema.Schema {
	return closedObject(map[string]*jsonschema.Schema{
		"project_slug": stringSchema("Exact project slug; writes never default a project.", 1, 0), "exclusion_id": uuidSchema("Exact exclusion ID."),
		"enabled": {Type: "boolean"}, "expected_version": stringSchema("Opaque version returned by list_risk_exclusions.", 1, 0),
		"idempotency_key": stringSchema("Caller replay key.", 1, 128),
	}, []string{"project_slug", "exclusion_id", "enabled", "expected_version", "idempotency_key"})
}

func riskPolicyCreateCommonProperties(catalog policycatalog.Catalog) map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{
		"project_slug":    stringSchema("Exact project slug; writes never default a project.", 1, 0),
		"name":            stringSchema("Policy name.", 1, 100),
		"enabled":         {Type: "boolean"},
		"action":          describedCatalogEnumSchema(catalog, catalog.Actions, "What happens when the policy fires; default flag. One of:", func(value string) string { return policycatalog.ActionDescriptions[value] }),
		"score":           {Type: "number", Minimum: new(0.1), Maximum: new(float64(10)), Description: "CVSS-style severity shown on findings (0.1-10); default 5. Does not change what is detected."},
		"user_message":    stringSchema("Optional user-facing enforcement message.", 0, 500),
		"idempotency_key": stringSchema("Caller key retained for 24-hour replay safety.", 1, 128),
	}
}

func fallbackCreateRiskPolicySchema() *jsonschema.Schema {
	return createRiskPolicySchema(policycatalog.Catalog{})
}

func fallbackUpdateRiskPolicySchema() *jsonschema.Schema {
	return updateRiskPolicySchema(policycatalog.Catalog{})
}

func fallbackCreateRiskExclusionSchema() *jsonschema.Schema {
	return createRiskExclusionSchema(policycatalog.Catalog{})
}

func detectionScopesSchema(catalog policycatalog.Catalog) *jsonschema.Schema {
	categoryDescriptions := policycatalog.CategoryDescriptions()
	scope := closedObject(map[string]*jsonschema.Schema{
		"category":      describedCatalogEnumSchema(catalog, catalog.DetectionScopeCategories, "Detector category the scope applies to. One of:", func(value string) string { return categoryDescriptions[value] }),
		"message_types": arraySchema(catalogEnumSchema(catalog, catalog.PolicyMessageTypes), 1, true),
	}, []string{"category", "message_types"})
	schema := arraySchema(scope, 0, true)
	schema.Description = "Optional per-category override of which message surfaces are scanned. Omit to use the recommended scope for each category."
	return schema
}

func describedArraySchema(items *jsonschema.Schema, minItems int) *jsonschema.Schema {
	schema := arraySchema(items, minItems, true)
	schema.Description = items.Description
	return schema
}

func describedCatalogEnumSchema(catalog policycatalog.Catalog, values []string, lead string, describe func(string) string) *jsonschema.Schema {
	if catalog.Schema == "" {
		return catalogEnumSchema(catalog, values)
	}
	return describedEnumSchema(values, lead, describe)
}

func describedEnumSchema(values []string, lead string, describe func(string) string) *jsonschema.Schema {
	schema := enumSchema(values...)
	schema.Description = lead + "\n" + policycatalog.DescribeValues(values, describe)
	return schema
}

func describedConstSchema(value, description string) *jsonschema.Schema {
	schema := constSchema(value)
	schema.Description = description
	return schema
}

func projectSelectorSchema(common map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	properties := make(map[string]*jsonschema.Schema, len(common)+2)
	maps.Copy(properties, common)
	properties["project_id"] = uuidSchema("Optional exact project ID. Omit both project selectors to use the literal default project.")
	properties["project_slug"] = stringSchema("Optional exact project slug. Omit both project selectors to use the literal default project.", 1, 128)
	schema := closedObject(properties, required)
	schema.Not = &jsonschema.Schema{Required: []string{"project_id", "project_slug"}}
	return schema
}

func closedObject(properties map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "object", Properties: properties, Required: required, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
}

func stringSchema(description string, minLength, maxLength int) *jsonschema.Schema {
	schema := &jsonschema.Schema{Type: "string", Description: description}
	if minLength > 0 {
		schema.MinLength = new(minLength)
	}
	if maxLength > 0 {
		schema.MaxLength = new(maxLength)
	}
	return schema
}

func uuidSchema(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Format: "uuid", Description: description}
}
func catalogEnumSchema(catalog policycatalog.Catalog, values []string) *jsonschema.Schema {
	if catalog.Schema == "" {
		return stringSchema("Pinned catalog value.", 1, 256)
	}
	return enumSchema(values...)
}

func enumSchema(values ...string) *jsonschema.Schema {
	enum := make([]any, len(values))
	for i, value := range values {
		enum[i] = value
	}
	return &jsonschema.Schema{Type: "string", Enum: enum}
}
func constSchema(value string) *jsonschema.Schema {
	constant := any(value)
	return &jsonschema.Schema{Type: "string", Const: &constant}
}
func arraySchema(items *jsonschema.Schema, minItems int, unique bool) *jsonschema.Schema {
	return boundedArraySchema(items, minItems, 0, unique)
}

func boundedArraySchema(items *jsonschema.Schema, minItems, maxItems int, unique bool) *jsonschema.Schema {
	schema := &jsonschema.Schema{Type: "array", Items: items, MinItems: new(minItems), UniqueItems: unique}
	if maxItems > 0 {
		schema.MaxItems = new(maxItems)
	}
	return schema
}
