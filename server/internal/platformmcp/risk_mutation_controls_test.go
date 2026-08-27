package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

type riskMutationFlagProvider struct {
	evaluation feature.Evaluation
	err        error
	flag       feature.Flag
	groups     map[string]string
}

func (p *riskMutationFlagProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}
func (p *riskMutationFlagProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}
func (p *riskMutationFlagProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}
func (p *riskMutationFlagProvider) EvaluateFlag(_ context.Context, flag feature.Flag, _ string, groups map[string]string) (feature.Evaluation, error) {
	p.flag = flag
	p.groups = groups
	return p.evaluation, p.err
}

type riskMutationOrganizationResolver struct {
	slug string
	err  error
}

func (r riskMutationOrganizationResolver) OrganizationSlug(context.Context, string) (string, error) {
	return r.slug, r.err
}

func TestRiskMutationAdmissionRequiresExactEnabledProjectBeforeBudget(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	projects := &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "organization", projectSlug: project.Slug}}}
	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	flags := &riskMutationFlagProvider{evaluation: feature.EvaluationDisabled}
	controls := &RiskMutationControls{
		flags: flags, organizations: riskMutationOrganizationResolver{slug: "organization-slug"}, projects: projects,
		budget: OperationBudget{Connection: connection, Organization: organization}, receipts: &RiskMutationReceiptStore{}, versions: &riskVersionCodec{key: []byte("key")},
	}
	principal := Principal{UserID: "user", OrganizationID: "organization", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}

	_, err := controls.Admit(t.Context(), principal, project.Slug)

	require.ErrorIs(t, err, ErrRiskMutationUnavailable)
	require.Equal(t, feature.FlagPlatformMCPRiskMutations, flags.flag)
	require.Equal(t, feature.OrgProjectGroups("organization-slug", project.Slug), flags.groups)
	require.Empty(t, connection.keys, "a disabled kill switch must refuse before consuming budget")
	require.Empty(t, organization.keys)
}

func TestRiskMutationAdmissionDistinguishesMissingProjectFromResolverOutage(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("database unavailable")
	for _, test := range []struct {
		name     string
		resolve  error
		wantCode string
		wantErr  error
	}{
		{name: "missing project", resolve: ErrRiskReadNotFound, wantCode: "not_found", wantErr: ErrRiskMutationNotFound},
		{name: "resolver outage", resolve: backendFailure, wantCode: unavailableCode, wantErr: ErrRiskMutationUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controls := &RiskMutationControls{
				flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, organizations: riskMutationOrganizationResolver{slug: "organization-slug"},
				projects: &stubRiskProjects{err: test.resolve}, budget: OperationBudget{Connection: &recordingOperationLimiter{}, Organization: &recordingOperationLimiter{}}, receipts: &RiskMutationReceiptStore{}, versions: &riskVersionCodec{key: []byte("key")},
			}
			_, err := controls.Admit(t.Context(), Principal{UserID: "user", OrganizationID: "organization"}, "project")
			var mutationErr *RiskMutationError
			require.ErrorAs(t, err, &mutationErr)
			require.Equal(t, test.wantCode, mutationErr.Code)
			require.ErrorIs(t, err, test.wantErr)
			if errors.Is(test.resolve, backendFailure) {
				require.ErrorIs(t, err, backendFailure)
			}
		})
	}
}

func TestRiskMutationAdmissionConnectionlessAssistantUsesOrganizationBudget(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	controls := &RiskMutationControls{
		flags: &riskMutationFlagProvider{evaluation: feature.EvaluationEnabled}, organizations: riskMutationOrganizationResolver{slug: "organization-slug"},
		projects: &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "organization", projectSlug: project.Slug}}},
		budget:   OperationBudget{Connection: &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}, Organization: organization}, receipts: &RiskMutationReceiptStore{}, versions: &riskVersionCodec{key: []byte("key")},
	}
	principal := Principal{UserID: "user", OrganizationID: "organization", ClientID: AssistantClientID, Surface: SurfaceProjectAssistant}

	resolved, err := controls.Admit(t.Context(), principal, project.Slug)

	require.NoError(t, err)
	require.Equal(t, project, resolved)
	require.Equal(t, []string{"organization"}, organization.keys)
}

func TestRiskExclusionMutationErrorPreservesConflict(t *testing.T) {
	t.Parallel()

	conflict := riskMutationConflict("The risk exclusion changed after it was read.")
	mapped := mapRiskExclusionMutationError(conflict)

	require.Same(t, conflict, mapped)
	var mutation *RiskMutationError
	require.ErrorAs(t, mapped, &mutation)
	require.Equal(t, "conflict", mutation.Code)
}

func TestRiskMutationInputHashIsCanonicalAndOperationSeparated(t *testing.T) {
	t.Parallel()

	first, err := riskMutationInputHash(operationCreateRiskPolicy, map[string]any{"enabled": true, "name": "policy"})
	require.NoError(t, err)
	second, err := riskMutationInputHash(operationCreateRiskPolicy, map[string]any{"name": "policy", "enabled": true})
	require.NoError(t, err)
	other, err := riskMutationInputHash(operationUpdateRiskPolicy, map[string]any{"enabled": true, "name": "policy"})
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.NotEqual(t, first, other)
}

func TestRiskVersionTokensAreOpaqueAndStateSensitive(t *testing.T) {
	t.Parallel()

	codec, err := newRiskVersionCodec("test-key")
	require.NoError(t, err)
	prompt := "sensitive prompt material"
	includeA, includeB := "message.type == 'a'", "message.type == 'b'"
	exemptA, exemptB := "message.source == 'a'", "message.source == 'b'"
	policy := policycore.Policy{
		ID: uuid.New(), ProjectID: uuid.New(), OrganizationID: "organization", Name: "policy", PolicyType: "prompt_based", Prompt: &prompt,
		AudiencePrincipalURNs: []string{"user:two", "user:one"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		DetectionScopes: []policycore.DetectionScope{
			{Category: "tool_output", ScopeInclude: &includeB, ScopeExempt: &exemptB},
			{Category: "tool_output", ScopeInclude: &includeA, ScopeExempt: &exemptA},
			{Category: "prompt", ScopeInclude: nil, ScopeExempt: nil},
		},
	}
	state := RiskPolicyVersionState{
		Policy: policy, AnalyzerConfig: json.RawMessage(`{"threshold":0.5,"nested":{"enabled":true}}`),
		AllowedURLGrants: []RiskPolicyVersionGrant{
			{PrincipalURN: "user:two", Selector: json.RawMessage(`{"server_url":"https://example.test/mcp","resource_id":"policy"}`)},
			{PrincipalURN: "user:one", Selector: json.RawMessage(`{"resource_id":"policy","server_url":"https://example.test/mcp"}`)},
		},
		BlockedURLGrants:      []RiskPolicyVersionGrant{},
		StandingDecisionState: []string{"decision:two", "decision:one"},
	}
	token, err := codec.PolicyVersion(state)
	require.NoError(t, err)
	canonicalPolicy := policy
	canonicalPolicy.DetectionScopes = []policycore.DetectionScope{
		{Category: "prompt", ScopeInclude: nil, ScopeExempt: nil},
		{Category: "tool_output", ScopeInclude: &includeA, ScopeExempt: &exemptA},
		{Category: "tool_output", ScopeInclude: &includeB, ScopeExempt: &exemptB},
	}
	canonical, err := codec.PolicyVersion(RiskPolicyVersionState{
		Policy: canonicalPolicy, AnalyzerConfig: json.RawMessage(`{ "nested": { "enabled": true }, "threshold": 0.5 }`),
		AllowedURLGrants: []RiskPolicyVersionGrant{
			{PrincipalURN: "user:one", Selector: json.RawMessage(`{ "server_url": "https://example.test/mcp", "resource_id": "policy" }`)},
			{PrincipalURN: "user:two", Selector: json.RawMessage(`{"resource_id":"policy","server_url":"https://example.test/mcp"}`)},
		},
		BlockedURLGrants:      []RiskPolicyVersionGrant{},
		StandingDecisionState: []string{"decision:one", "decision:two"},
	})
	require.NoError(t, err)

	require.Equal(t, token, canonical, "canonical ordering must include the complete detection scope state")
	require.NotContains(t, token, prompt)
	require.True(t, codec.ValidPolicyVersion(state, token))
	pending, total := int64(3), int64(10)
	progressOnly := state
	progressOnly.Policy.PendingMessages = &pending
	progressOnly.Policy.TotalMessages = &total
	progressToken, err := codec.PolicyVersion(progressOnly)
	require.NoError(t, err)
	require.Equal(t, token, progressToken, "read-time analysis progress is not policy concurrency state")
	policy.Enabled = true
	require.False(t, codec.ValidPolicyVersion(RiskPolicyVersionState{Policy: policy, AnalyzerConfig: state.AnalyzerConfig, AllowedURLGrants: state.AllowedURLGrants, BlockedURLGrants: state.BlockedURLGrants, StandingDecisionState: state.StandingDecisionState}, token))
	grantChanged := state
	grantChanged.AllowedURLGrants = []RiskPolicyVersionGrant{
		{PrincipalURN: "user:three", Selector: json.RawMessage(`{"resource_id":"policy","server_url":"https://example.test/mcp"}`)},
		{PrincipalURN: "user:one", Selector: json.RawMessage(`{"resource_id":"policy","server_url":"https://example.test/mcp"}`)},
	}
	require.False(t, codec.ValidPolicyVersion(grantChanged, token), "grant audiences are policy concurrency state")
	grantChanged = state
	grantChanged.AllowedURLGrants = slices.Clone(state.AllowedURLGrants)
	grantChanged.AllowedURLGrants[0].Selector = json.RawMessage(`{"resource_id":"policy","server_url":"https://example.test/mcp","server_identity":"other"}`)
	require.False(t, codec.ValidPolicyVersion(grantChanged, token), "complete grant selectors are policy concurrency state")

	exclusion := exclusioncore.Exclusion{ID: uuid.New(), ProjectID: policy.ProjectID, OrganizationID: "organization", MatchType: "exact", MatchValue: "sensitive exact match", Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	exclusionToken, err := codec.ExclusionVersion(exclusion)
	require.NoError(t, err)
	require.NotContains(t, exclusionToken, exclusion.MatchValue)
	require.True(t, codec.ValidExclusionVersion(exclusion, exclusionToken))
	exclusion.Enabled = false
	require.False(t, codec.ValidExclusionVersion(exclusion, exclusionToken))
}

func TestRiskMutationReceiptResultIsClosedAndOperationBound(t *testing.T) {
	t.Parallel()

	result := CreateRiskPolicyReceiptResult{Project: RiskMutationReceiptProject{ID: "11111111-1111-4111-8111-111111111111", Slug: "default"}, Policy: RiskPolicyReceiptSummary{ID: "22222222-2222-4222-8222-222222222222", PolicyType: "standard", Enabled: true, Action: "flag"}, Version: "opaque", MatchedExisting: false, ResultCategory: "created"}
	payload, err := encodeRiskMutationResult(operationCreateRiskPolicy, result)
	require.NoError(t, err)
	require.JSONEq(t, `{"project":{"id":"11111111-1111-4111-8111-111111111111","slug":"default"},"policy":{"id":"22222222-2222-4222-8222-222222222222","policy_type":"standard","enabled":true,"action":"flag"},"version":"opaque","matched_existing":false,"result_category":"created"}`, string(payload))
	require.NotContains(t, string(payload), "prompt")
	require.NotContains(t, string(payload), "match_value")

	_, err = encodeRiskMutationResult(operationUpdateRiskPolicy, result)
	require.ErrorIs(t, err, ErrRiskMutationUnavailable)

	result.Policy.Action = "prompt material smuggled as action"
	_, err = encodeRiskMutationResult(operationCreateRiskPolicy, result)
	require.ErrorIs(t, err, ErrRiskMutationUnavailable)

	result.Policy.Action = "flag"
	result.Project.Slug = "prompt material smuggled as slug"
	_, err = encodeRiskMutationResult(operationCreateRiskPolicy, result)
	require.ErrorIs(t, err, ErrRiskMutationUnavailable)
}

func TestRiskMutationReceiptResultAllowlistCoversEveryOperation(t *testing.T) {
	t.Parallel()

	project := RiskMutationReceiptProject{ID: "11111111-1111-4111-8111-111111111111", Slug: "default"}
	policy := RiskPolicyReceiptSummary{ID: "22222222-2222-4222-8222-222222222222", PolicyType: "standard", Enabled: true, Action: "flag"}
	exclusion := RiskExclusionReceiptSummary{ID: "33333333-3333-4333-8333-333333333333", MatchType: "source", Enabled: true}
	for _, action := range []string{"flag", "warn", "block", "quarantine"} {
		t.Run("policy action "+action, func(t *testing.T) {
			t.Parallel()
			candidate := policy
			candidate.Action = action
			_, err := encodeRiskMutationResult(operationUpdateRiskPolicy, UpdateRiskPolicyReceiptResult{Project: project, Policy: candidate, Version: "opaque", ResultCategory: "updated"})
			require.NoError(t, err)
		})
	}
	for _, test := range []struct {
		name      string
		operation string
		valid     RiskMutationReceiptResult
		invalid   RiskMutationReceiptResult
	}{
		{name: "create policy", operation: operationCreateRiskPolicy, valid: CreateRiskPolicyReceiptResult{Project: project, Policy: policy, Version: "opaque", ResultCategory: "created"}, invalid: CreateRiskPolicyReceiptResult{Project: project, Policy: RiskPolicyReceiptSummary{ID: policy.ID, PolicyType: "unsupported", Enabled: true, Action: "flag"}, Version: "opaque", ResultCategory: "created"}},
		{name: "update policy", operation: operationUpdateRiskPolicy, valid: UpdateRiskPolicyReceiptResult{Project: project, Policy: policy, Version: "opaque", ResultCategory: "updated"}, invalid: UpdateRiskPolicyReceiptResult{Project: project, Policy: policy, Version: "opaque", ResultCategory: "user content"}},
		{name: "create exclusion", operation: operationCreateRiskExclusion, valid: CreateRiskExclusionReceiptResult{Project: project, Exclusion: exclusion, Version: "opaque", ResultCategory: "created", Reconciliation: "scheduled"}, invalid: CreateRiskExclusionReceiptResult{Project: project, Exclusion: RiskExclusionReceiptSummary{ID: exclusion.ID, MatchType: "user content", Enabled: true}, Version: "opaque", ResultCategory: "created", Reconciliation: "scheduled"}},
		{name: "update exclusion", operation: operationUpdateRiskExclusion, valid: UpdateRiskExclusionReceiptResult{Project: project, Exclusion: exclusion, Version: "opaque", ResultCategory: "updated", Reconciliation: "scheduled"}, invalid: UpdateRiskExclusionReceiptResult{Project: project, Exclusion: exclusion, Version: "opaque", ResultCategory: "updated", Reconciliation: "complete"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := encodeRiskMutationResult(test.operation, test.valid)
			require.NoError(t, err)
			_, err = encodeRiskMutationResult(test.operation, receiptResultPointer(test.valid))
			require.NoError(t, err, "non-nil pointers to closed receipt projections remain safe")
			_, err = encodeRiskMutationResult(test.operation, test.invalid)
			require.ErrorIs(t, err, ErrRiskMutationUnavailable)
		})
	}

	for _, test := range []struct {
		name      string
		operation string
		result    RiskMutationReceiptResult
	}{
		{name: "create policy", operation: operationCreateRiskPolicy, result: (*CreateRiskPolicyReceiptResult)(nil)},
		{name: "update policy", operation: operationUpdateRiskPolicy, result: (*UpdateRiskPolicyReceiptResult)(nil)},
		{name: "create exclusion", operation: operationCreateRiskExclusion, result: (*CreateRiskExclusionReceiptResult)(nil)},
		{name: "update exclusion", operation: operationUpdateRiskExclusion, result: (*UpdateRiskExclusionReceiptResult)(nil)},
	} {
		t.Run("typed nil "+test.name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, func() {
				_, err := encodeRiskMutationResult(test.operation, test.result)
				require.ErrorIs(t, err, ErrRiskMutationUnavailable)
			})
		})
	}
}

func receiptResultPointer(result RiskMutationReceiptResult) RiskMutationReceiptResult {
	switch typed := result.(type) {
	case CreateRiskPolicyReceiptResult:
		return &typed
	case UpdateRiskPolicyReceiptResult:
		return &typed
	case CreateRiskExclusionReceiptResult:
		return &typed
	case UpdateRiskExclusionReceiptResult:
		return &typed
	default:
		return nil
	}
}

func TestRiskMutationHandlerSelectionAcceptsExportedSuccessContract(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registrar := newRegistrar(server)
	want := CreateRiskPolicyToolOutput{
		CreateRiskPolicyReceiptResult: CreateRiskPolicyReceiptResult{Project: RiskMutationReceiptProject{ID: "11111111-1111-4111-8111-111111111111", Slug: "default"}, Policy: RiskPolicyReceiptSummary{ID: "22222222-2222-4222-8222-222222222222", PolicyType: "standard", Enabled: true, Action: "flag"}, Version: "opaque", ResultCategory: "created"},
		Receipt:                       RiskMutationToolReceipt{ID: "33333333-3333-4333-8333-333333333333", Replayed: false},
	}
	registerRiskMutationHandlers(registrar, catalog, true, &RiskMutationHandlers{
		Controls: &RiskMutationControls{},
		CreatePolicy: func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, CreateRiskPolicyToolOutput, error) {
			return nil, want, nil
		},
	})
	require.NotContains(t, descriptorByName(t, registrar, operationCreateRiskPolicy).Description, "not enabled")
	require.Contains(t, descriptorByName(t, registrar, operationUpdateRiskPolicy).Description, "not enabled")
	for _, descriptor := range registrar.Descriptors() {
		if descriptor.Name != operationCreateRiskPolicy {
			continue
		}
		got, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"default","policy_type":"standard","name":"policy","enabled":true,"sources":["gitleaks"],"idempotency_key":"key"}`))
		require.NoError(t, err)
		require.Equal(t, want, got)
		return
	}
	require.Fail(t, "create risk policy descriptor was not registered")
}

func TestRiskMutationHandlerSelectionRequiresAvailableCatalogForLiveCallbacks(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registrar := newRegistrar(server)
	called := false
	registerRiskMutationHandlers(registrar, policycatalog.Catalog{}, false, &RiskMutationHandlers{
		Controls: &RiskMutationControls{},
		CreatePolicy: func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, CreateRiskPolicyToolOutput, error) {
			called = true
			return nil, CreateRiskPolicyToolOutput{}, nil
		},
	})

	create := descriptorByName(t, registrar, operationCreateRiskPolicy)
	_, err := create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"default","policy_type":"prompt_based","name":"policy","enabled":true,"prompt":"instruction","idempotency_key":"key"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
	require.Contains(t, create.Description, "not enabled")
	require.False(t, called, "catalog failure must keep live callbacks unavailable")
}

func TestRiskMutationHandlerSelectionDefaultsEveryWriteToStableRefusal(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registrar := newRegistrar(server)
	registerRiskMutationHandlers(registrar, catalog, true, &RiskMutationHandlers{})
	for _, name := range []string{operationCreateRiskPolicy, operationUpdateRiskPolicy, operationCreateRiskExclusion, operationUpdateRiskExclusion} {
		require.Contains(t, descriptorByName(t, registrar, name).Description, "not enabled")
	}
	arguments := map[string]json.RawMessage{
		"create_risk_policy":    json.RawMessage(`{"project_slug":"default","policy_type":"standard","name":"policy","enabled":true,"sources":["gitleaks"],"idempotency_key":"key"}`),
		"update_risk_policy":    json.RawMessage(`{"project_slug":"default","policy_id":"11111111-1111-4111-8111-111111111111","expected_version":"version","idempotency_key":"key","patch":{"enabled":true}}`),
		"create_risk_exclusion": json.RawMessage(`{"project_slug":"default","match_type":"source","match_value":"gitleaks","enabled":true,"idempotency_key":"key"}`),
		"update_risk_exclusion": json.RawMessage(`{"project_slug":"default","exclusion_id":"11111111-1111-4111-8111-111111111111","enabled":true,"expected_version":"version","idempotency_key":"key"}`),
	}
	for _, descriptor := range registrar.Descriptors() {
		input, ok := arguments[descriptor.Name]
		if !ok || (!strings.HasPrefix(descriptor.Name, "create_risk_") && !strings.HasPrefix(descriptor.Name, "update_risk_")) {
			continue
		}
		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), input)
		require.Error(t, err)
		require.Contains(t, err.Error(), `"code":"feature_unavailable"`)
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "risk-stub-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	refused, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: operationCreateRiskPolicy, Arguments: map[string]any{"project_slug": "default", "policy_type": "standard", "name": "policy", "enabled": true, "sources": []string{"gitleaks"}, "idempotency_key": "key"}})
	require.NoError(t, err)
	require.True(t, refused.IsError)
	require.Nil(t, refused.StructuredContent, "disabled tools must not emit a zero-valued structured success output")
	require.Len(t, refused.Content, 1)
	text, ok := refused.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"feature_unavailable","feature":"risk_mutations","message":"This Platform MCP capability is not enabled for the current rollout."}`, text.Text)
}
