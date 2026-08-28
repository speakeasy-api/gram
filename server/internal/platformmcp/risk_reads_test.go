package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

type riskProjectCall struct {
	organizationID string
	projectID      string
	projectSlug    string
}

type stubRiskProjects struct {
	project  ResolvedProject
	expected []riskProjectCall
	calls    []riskProjectCall
	err      error
}

func (s *stubRiskProjects) Resolve(_ context.Context, organizationID, projectID, projectSlug string) (ResolvedProject, error) {
	call := riskProjectCall{organizationID: organizationID, projectID: projectID, projectSlug: projectSlug}
	s.calls = append(s.calls, call)
	if s.err != nil {
		return ResolvedProject{}, s.err
	}
	index := len(s.calls) - 1
	if index >= len(s.expected) || s.expected[index] != call {
		return ResolvedProject{}, fmt.Errorf("unexpected project resolution call: %+v", call)
	}
	return s.project, nil
}

type stubRiskPolicies struct {
	policies []policycore.Policy
	policy   policycore.Policy
	cursor   *policycore.PageCursor
	getErr   error
}

func (s *stubRiskPolicies) ListPage(_ context.Context, _ string, _ uuid.UUID, cursor *policycore.PageCursor, _ int32) ([]policycore.Policy, error) {
	s.cursor = cursor
	return s.policies, nil
}

func (s *stubRiskPolicies) loadDetail(_ context.Context, _, _ uuid.UUID) (policycore.Policy, string, error) {
	if s.getErr != nil {
		return policycore.Policy{}, "", fmt.Errorf("load test risk policy detail: %w", s.getErr)
	}
	return s.policy, "opaque-version", nil
}

type stubRiskExclusions struct {
	exclusions []exclusioncore.Exclusion
	cursor     *exclusioncore.PageCursor
}

func (s *stubRiskExclusions) ListPage(_ context.Context, _ uuid.UUID, _ uuid.NullUUID, cursor *exclusioncore.PageCursor, _ int32) ([]exclusioncore.Exclusion, error) {
	s.cursor = cursor
	return s.exclusions, nil
}

func testRiskReadService(t *testing.T, projects riskProjectResolver, policies *stubRiskPolicies, exclusions riskExclusionReader) *RiskReadService {
	t.Helper()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	fingerprint, err := policycatalog.Fingerprint(catalog)
	require.NoError(t, err)
	cursor, err := newRiskCursorCodec("test-key")
	require.NoError(t, err)
	return &RiskReadService{
		projects: projects, policies: policies, exclusions: exclusions,
		cursor: cursor, catalog: catalog, catalogFingerprint: fingerprint,
		redactionKey:     []byte("0123456789abcdef0123456789abcdef"),
		loadPolicyDetail: policies.loadDetail,
	}
}

func testRiskPrincipal(user string) Principal {
	return Principal{UserID: user, OrganizationID: "<ORG_ID>", ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "test", Surface: SurfacePlatformMCP}
}

func TestRiskReadCursorBindingAndPagination(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Default", Slug: "default"}
	createdAt := time.Date(2026, time.August, 25, 1, 2, 3, 0, time.UTC)
	policies := make([]policycore.Policy, 0, 3)
	for i := range 3 {
		policies = append(policies, policycore.Policy{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Name: string(rune('a' + i)), PolicyType: "standard", Enabled: true, Action: "flag", Sources: []string{"gitleaks"}, Score: 5, CreatedAt: createdAt.Add(-time.Duration(i) * time.Minute), UpdatedAt: createdAt})
	}
	projectStub := &stubRiskProjects{project: project, expected: []riskProjectCall{
		{organizationID: "<ORG_ID>"},
		{organizationID: "<ORG_ID>"},
		{organizationID: "<ORG_ID>"},
		{organizationID: "<ORG_ID>"},
		{organizationID: "<ORG_ID>"},
	}}
	policyStub := &stubRiskPolicies{policies: policies}
	service := testRiskReadService(t, projectStub, policyStub, &stubRiskExclusions{})
	principal := testRiskPrincipal("user-one")

	first, err := service.ListPolicies(t.Context(), principal, ListRiskPoliciesInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []riskProjectCall{{organizationID: "<ORG_ID>"}}, projectStub.calls)
	require.Len(t, first.Policies, 2)
	require.NotEmpty(t, first.NextCursor)

	policyStub.policies = nil
	_, err = service.ListPolicies(t.Context(), principal, ListRiskPoliciesInput{Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.NotNil(t, policyStub.cursor)
	require.Equal(t, policies[1].ID, policyStub.cursor.ID)

	_, err = service.ListPolicies(t.Context(), testRiskPrincipal("user-two"), ListRiskPoliciesInput{Limit: 2, Cursor: first.NextCursor})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
	_, err = service.ListPolicies(t.Context(), principal, ListRiskPoliciesInput{Limit: 2, Cursor: first.NextCursor + "tampered"})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)

	incomplete := principal
	incomplete.ConnectionID = ""
	_, err = service.ListPolicies(t.Context(), incomplete, ListRiskPoliciesInput{Limit: 2, Cursor: first.NextCursor})
	require.ErrorIs(t, err, ErrRiskCursorInvalid)
}

func TestRiskReadProjectionsOmitSensitivePolicyFields(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	prompt := "authorized prompt content"
	model := "private-model"
	scope := `kind == "user_message"`
	policy := policycore.Policy{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Name: "legacy", PolicyType: "prompt_based",
		Sources: []string{"gitleaks", "unknown"}, PresidioEntities: []string{"EMAIL_ADDRESS"}, DisabledRules: []string{"secret.aws_secret_access_key"}, CustomRuleIDs: []string{"custom.rule"},
		MessageTypes: []string{"user_message"}, ScopeInclude: &scope, Enabled: true, Action: "quarantine", AudienceType: "targeted", AudiencePrincipalURNs: []string{"user:<USER_ID>"},
		Prompt: &prompt, ModelConfig: &policycore.ModelConfig{Model: &model}, Score: 5, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	service := testRiskReadService(t, &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectSlug: "project"}}}, &stubRiskPolicies{policy: policy}, &stubRiskExclusions{})
	output, err := service.GetPolicy(t.Context(), testRiskPrincipal("user"), GetRiskPolicyInput{ProjectSlug: "project", PolicyID: policy.ID.String()})
	require.NoError(t, err)
	require.Equal(t, &prompt, output.Policy.Prompt)
	require.Empty(t, output.Policy.ApprovedEmailDomains)
	require.NotNil(t, output.Policy.ApprovedEmailDomains)
	require.Equal(t, "opaque-version", output.Policy.Version)
	require.Empty(t, output.Policy.Action)
	require.ElementsMatch(t, []string{"custom_rules", "model_config", "raw_scope", "targeted_audience", "unknown_detector_value", "unsupported_action"}, output.Policy.Compatibility.UnsupportedFields)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	text := string(encoded)
	require.NotContains(t, text, model)
	require.NotContains(t, text, "user:<USER_ID>")
	require.NotContains(t, text, scope)
	require.NotContains(t, text, "custom.rule")
}

func TestRiskExclusionProjectionRedactsExactAndRegex(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	now := time.Now()
	exclusions := []exclusioncore.Exclusion{
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", MatchType: "exact", MatchValue: "sensitive-exact", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", MatchType: "regex", MatchValue: "^sensitive.*", Enabled: false, CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", MatchType: "source", MatchValue: "gitleaks", Enabled: true, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now},
		{ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", MatchType: "entity_type", MatchValue: "EMAIL_ADDRESS", SourceFilter: "gitleaks", Enabled: true, CreatedAt: now.Add(-3 * time.Minute), UpdatedAt: now},
	}
	service := testRiskReadService(t, &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectID: project.ID.String()}}}, &stubRiskPolicies{}, &stubRiskExclusions{exclusions: exclusions})
	output, err := service.ListExclusions(t.Context(), testRiskPrincipal("user"), ListRiskExclusionsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.Len(t, output.Exclusions, 4)
	require.Empty(t, output.Exclusions[0].MatchValue)
	require.NotEmpty(t, output.Exclusions[0].MatchFingerprint)
	require.Equal(t, len([]rune("sensitive-exact")), output.Exclusions[0].MatchLength)
	require.Contains(t, output.Exclusions[1].Compatibility.UnsupportedFields, "legacy_regex")
	require.Equal(t, "gitleaks", output.Exclusions[2].MatchValue)
	require.Empty(t, output.Exclusions[3].SourceFilter)
	require.Contains(t, output.Exclusions[3].Compatibility.UnsupportedFields, "unsupported_source_filter")

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sensitive-exact")
	require.NotContains(t, string(encoded), "^sensitive")
}

func TestRiskPolicyProjectionRecognizesOnlyCanonicalD3ScopesAndActions(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	include, err := policycatalog.EncodeDetectionScope([]string{"tool_response", "user_message"}, catalog)
	require.NoError(t, err)
	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	policy := policycore.Policy{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: "<ORG_ID>", Name: "scoped", PolicyType: "standard",
		Sources: []string{"destructive_tool"}, DetectionScopes: []policycore.DetectionScope{{Category: "destructive_tool", ScopeInclude: &include}},
		Enabled: true, Action: "block", AudienceType: "everyone", Score: 5, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	service := testRiskReadService(t, &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectSlug: "project"}}}, &stubRiskPolicies{policy: policy}, &stubRiskExclusions{})
	output, err := service.GetPolicy(t.Context(), testRiskPrincipal("user"), GetRiskPolicyInput{ProjectSlug: "project", PolicyID: policy.ID.String()})
	require.NoError(t, err)
	require.Empty(t, output.Policy.Action)
	require.Contains(t, output.Policy.Compatibility.UnsupportedFields, "unsupported_action")
	require.NotContains(t, output.Policy.Compatibility.UnsupportedFields, "raw_scope")
	require.Equal(t, []RiskDetectionScope{{Category: "destructive_tool", MessageTypes: []string{"tool_response", "user_message"}}}, output.Policy.DetectionScopes)

	legacy := `kind == "user_message"`
	policy.DetectionScopes[0].ScopeInclude = &legacy
	projectStub := &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectSlug: "project"}}}
	service = testRiskReadService(t, projectStub, &stubRiskPolicies{policy: policy}, &stubRiskExclusions{})
	output, err = service.GetPolicy(t.Context(), testRiskPrincipal("user"), GetRiskPolicyInput{ProjectSlug: "project", PolicyID: policy.ID.String()})
	require.NoError(t, err)
	require.Empty(t, output.Policy.DetectionScopes)
	require.Contains(t, output.Policy.Compatibility.UnsupportedFields, "raw_scope")
}

func TestRiskPolicyGetDistinguishesNotFoundFromInfrastructureFailure(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	input := GetRiskPolicyInput{ProjectSlug: "project", PolicyID: uuid.NewString()}
	service := testRiskReadService(t, &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectSlug: "project"}}}, &stubRiskPolicies{getErr: fmt.Errorf("%w: %w", policycore.ErrLoadPolicy, pgx.ErrNoRows)}, &stubRiskExclusions{})
	_, err := service.GetPolicy(t.Context(), testRiskPrincipal("user"), input)
	require.ErrorIs(t, err, ErrRiskReadNotFound)

	infrastructureErr := errors.New("database unavailable")
	service = testRiskReadService(t, &stubRiskProjects{project: project, expected: []riskProjectCall{{organizationID: "<ORG_ID>", projectSlug: "project"}}}, &stubRiskPolicies{getErr: infrastructureErr}, &stubRiskExclusions{})
	_, err = service.GetPolicy(t.Context(), testRiskPrincipal("user"), input)
	require.ErrorIs(t, err, infrastructureErr)
	require.NotErrorIs(t, err, ErrRiskReadNotFound)
}

func TestUnavailableRiskToolRegistrationSurvivesCatalogFailure(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-unavailable-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	buildCalls := 0
	require.NotPanics(t, func() {
		registerUnavailableRiskToolsWithCatalog(reg, func() (policycatalog.Catalog, error) {
			buildCalls++
			return policycatalog.Catalog{}, errors.New("catalog unavailable")
		})
	})
	require.Equal(t, 1, buildCalls)
	require.Len(t, reg.Descriptors(), 7)

	create := descriptorByName(t, reg, "create_risk_policy")
	_, err := create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","policy_type":"standard","name":"policy","enabled":true,"sources":["gitleaks"],"idempotency_key":"key"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)

	_, err = create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","policy_type":"standard","name":"policy","enabled":true,"sources":["gitleaks"],"idempotency_key":"key","unknown":true}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
	_, err = create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","policy_type":"standard","name":"policy","enabled":true,"sources":["`+strings.Repeat("x", 257)+`"],"idempotency_key":"key"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")

	for _, test := range []struct {
		name      string
		arguments string
	}{
		{name: "prompt policy branch", arguments: `{"project_slug":"project","policy_type":"prompt_based","name":"policy","enabled":true,"prompt":"instruction","idempotency_key":"key"}`},
		{name: "policy update patch", arguments: `{"project_slug":"project","policy_id":"11111111-1111-4111-8111-111111111111","expected_version":"version","idempotency_key":"key","patch":{"action":"catalog-unavailable-value","sources":["catalog-unavailable-value"]}}`},
		{name: "source exclusion branch", arguments: `{"project_slug":"project","match_type":"source","match_value":"catalog-unavailable-value","enabled":true,"rule_id_filter":"catalog-unavailable-value","idempotency_key":"key"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			toolName := map[string]string{"prompt policy branch": "create_risk_policy", "policy update patch": "update_risk_policy", "source exclusion branch": "create_risk_exclusion"}[test.name]
			_, err := descriptorByName(t, reg, toolName).Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(test.arguments))
			var refusal *ToolRefusalError
			require.ErrorAs(t, err, &refusal)
			require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
		})
	}
}

func TestRiskToolRegistrationAndStableStubs(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerUnavailableRiskTools(reg)

	wanted := map[string]ProjectScope{
		"list_risk_policies": ProjectScopeDefaultable, "get_risk_policy": ProjectScopeDefaultable, "list_risk_exclusions": ProjectScopeDefaultable,
		"create_risk_policy": ProjectScopeExplicit, "update_risk_policy": ProjectScopeExplicit, "create_risk_exclusion": ProjectScopeExplicit, "update_risk_exclusion": ProjectScopeExplicit,
	}
	require.Len(t, reg.Descriptors(), len(wanted))
	for _, descriptor := range reg.Descriptors() {
		require.Equal(t, wanted[descriptor.Name], descriptor.Meta.ProjectScope, descriptor.Name)
		require.ElementsMatch(t, bothAudiences, descriptor.Meta.Audiences, descriptor.Name)
		require.NotEmpty(t, descriptor.InputSchema, descriptor.Name)
		if strings.HasPrefix(descriptor.Name, "list_") || strings.HasPrefix(descriptor.Name, "get_") {
			require.NotNil(t, descriptor.Annotations)
			require.True(t, descriptor.Annotations.ReadOnlyHint)
		}
	}

	listPolicies := descriptorByName(t, reg, "list_risk_policies")
	_, err := listPolicies.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_id":"11111111-1111-4111-8111-111111111111","project_slug":"project"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")

	createPolicy := descriptorByName(t, reg, "create_risk_policy")
	_, err = createPolicy.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","policy_type":"standard","name":"policy","enabled":true,"sources":["presidio"],"presidio_entities":["not-pinned"],"idempotency_key":"key"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
	domains := make([]string, 51)
	for i := range domains {
		domains[i] = fmt.Sprintf("domain-%d.example", i)
	}
	payload, err := json.Marshal(map[string]any{"project_slug": "project", "policy_type": "standard", "name": "policy", "enabled": true, "sources": []string{"gitleaks"}, "approved_email_domains": domains, "idempotency_key": "key"})
	require.NoError(t, err)
	_, err = createPolicy.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), payload)
	require.ErrorContains(t, err, "arguments do not match the tool schema")

	create := descriptorByName(t, reg, "create_risk_exclusion")
	_, err = create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","match_type":"regex","match_value":"x","enabled":true,"idempotency_key":"key"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
	_, err = create.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("user")), json.RawMessage(`{"project_slug":"project","match_type":"exact","match_value":"x","enabled":true,"idempotency_key":"key"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
}
