package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const riskVersionCodecVersion = 1

var errRiskVersionInvalid = errors.New("invalid platform mcp risk version")

type riskVersionCodec struct {
	key []byte
}

type riskVersionEnvelope struct {
	Version int    `json:"v"`
	Kind    string `json:"k"`
	MAC     string `json:"m"`
}

func newRiskVersionCodec(keyMaterial string) (*riskVersionCodec, error) {
	if keyMaterial == "" {
		return nil, ErrRiskMutationUnavailable
	}
	key := sha256.Sum256([]byte("platform-mcp-risk-version:" + keyMaterial))
	return &riskVersionCodec{key: key[:]}, nil
}

// RiskPolicyVersionState is the complete locked policy state needed for an
// optimistic-concurrency token. Grant-backed URL selectors and audiences,
// standing-decision state, and canonical analyzer JSON are included because
// they can change enforcement without changing the policy's public summary.
// They remain inside the HMAC.
type RiskPolicyVersionState struct {
	Policy                policycore.Policy
	AnalyzerConfig        json.RawMessage
	AllowedURLGrants      []RiskPolicyVersionGrant
	BlockedURLGrants      []RiskPolicyVersionGrant
	StandingDecisionState []string
}

// RiskPolicyVersionGrant captures the complete enforcement identity of one URL
// grant. The principal and canonical selector remain inside the opaque HMAC.
type RiskPolicyVersionGrant struct {
	PrincipalURN string          `json:"principal_urn"`
	Selector     json.RawMessage `json:"selector"`
}

// PolicyVersion authenticates the complete transport-neutral persisted policy
// state. Sensitive prompt, URL-derived scope, model configuration, and audience
// values contribute only inside the HMAC and never appear in the token.
func (c *riskVersionCodec) PolicyVersion(input RiskPolicyVersionState) (string, error) {
	state := input
	// Progress is computed when a policy is read and changes as background
	// analysis advances; it is not part of the locked policy definition. Keep
	// persisted score, version, and timestamps, which do represent admin-visible
	// policy state and must invalidate stale writes.
	state.Policy.PendingMessages = nil
	state.Policy.TotalMessages = nil
	canonicalAnalyzer, err := canonicalJSON(state.AnalyzerConfig)
	if err != nil {
		return "", errRiskVersionInvalid
	}
	state.AnalyzerConfig = canonicalAnalyzer
	state.Policy.Sources = sortedStrings(state.Policy.Sources)
	state.Policy.PresidioEntities = sortedStrings(state.Policy.PresidioEntities)
	state.Policy.ApprovedEmailDomains = sortedStrings(state.Policy.ApprovedEmailDomains)
	state.Policy.PromptInjectionRules = sortedStrings(state.Policy.PromptInjectionRules)
	state.Policy.DisabledRules = sortedStrings(state.Policy.DisabledRules)
	state.Policy.CustomRuleIDs = sortedStrings(state.Policy.CustomRuleIDs)
	state.Policy.MessageTypes = sortedStrings(state.Policy.MessageTypes)
	state.Policy.AudiencePrincipalURNs = sortedStrings(state.Policy.AudiencePrincipalURNs)
	state.Policy.DetectionScopes = slices.Clone(state.Policy.DetectionScopes)
	state.AllowedURLGrants, err = canonicalRiskPolicyVersionGrants(state.AllowedURLGrants)
	if err != nil {
		return "", errRiskVersionInvalid
	}
	state.BlockedURLGrants, err = canonicalRiskPolicyVersionGrants(state.BlockedURLGrants)
	if err != nil {
		return "", errRiskVersionInvalid
	}
	state.StandingDecisionState = sortedStrings(state.StandingDecisionState)
	slices.SortFunc(state.Policy.DetectionScopes, compareDetectionScopes)
	return c.encode("policy", state)
}

// ExclusionVersion authenticates every persisted exclusion field. Exact match
// values remain inside the HMAC and cannot be recovered from the opaque token.
func (c *riskVersionCodec) ExclusionVersion(exclusion exclusioncore.Exclusion) (string, error) {
	return c.encode("exclusion", exclusion)
}

func (c *riskVersionCodec) ValidPolicyVersion(state RiskPolicyVersionState, token string) bool {
	expected, err := c.PolicyVersion(state)
	return err == nil && hmac.Equal([]byte(expected), []byte(token))
}

func (c *riskVersionCodec) ValidExclusionVersion(exclusion exclusioncore.Exclusion, token string) bool {
	expected, err := c.ExclusionVersion(exclusion)
	return err == nil && hmac.Equal([]byte(expected), []byte(token))
}

func canonicalRiskPolicyVersionGrants(grants []RiskPolicyVersionGrant) ([]RiskPolicyVersionGrant, error) {
	result := slices.Clone(grants)
	for index := range result {
		selector, err := canonicalJSON(result[index].Selector)
		if err != nil {
			return nil, err
		}
		result[index].Selector = selector
	}
	slices.SortFunc(result, func(a, b RiskPolicyVersionGrant) int {
		if order := compareStrings(a.PrincipalURN, b.PrincipalURN); order != 0 {
			return order
		}
		return compareStrings(string(a.Selector), string(b.Selector))
	})
	return slices.CompactFunc(result, func(a, b RiskPolicyVersionGrant) bool {
		return a.PrincipalURN == b.PrincipalURN && string(a.Selector) == string(b.Selector)
	}), nil
}

func compareDetectionScopes(a, b policycore.DetectionScope) int {
	if order := compareStrings(a.Category, b.Category); order != 0 {
		return order
	}
	if order := compareOptionalStrings(a.ScopeInclude, b.ScopeInclude); order != 0 {
		return order
	}
	return compareOptionalStrings(a.ScopeExempt, b.ScopeExempt)
}

func compareOptionalStrings(a, b *string) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return compareStrings(*a, *b)
	}
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// riskPolicyVersionState loads every grant-backed part of a policy definition
// from the same database snapshot as the policy row. Sensitive URLs, principals,
// selectors, and standing decisions contribute only to the HMAC and are never
// returned. Mutation validation requests the enforcement lock before any of
// those reads; repeatable-read projections already have one stable snapshot.
func riskPolicyVersionState(ctx context.Context, db riskrepo.DBTX, policy policycore.Policy, lockEnforcement bool) (RiskPolicyVersionState, error) {
	state := RiskPolicyVersionState{Policy: policy, AnalyzerConfig: nil, AllowedURLGrants: []RiskPolicyVersionGrant{}, BlockedURLGrants: []RiskPolicyVersionGrant{}, StandingDecisionState: []string{}}
	approvalQueries := approvalrepo.New(db)
	if lockEnforcement {
		if err := approvalQueries.LockProjectEnforcementState(ctx, policy.ProjectID.String()); err != nil {
			return RiskPolicyVersionState{}, fmt.Errorf("lock risk policy enforcement state: %w", err)
		}
	}
	row, err := riskrepo.New(db).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: policy.ID, ProjectID: policy.ProjectID})
	if err != nil {
		return RiskPolicyVersionState{}, fmt.Errorf("load risk policy version row: %w", err)
	}
	state.AnalyzerConfig = row.AnalyzerConfig
	allowedGrants, err := riskPolicyVersionURLGrants(ctx, db, policy, authz.ScopeRiskPolicyBypass)
	if err != nil {
		return RiskPolicyVersionState{}, err
	}
	blockedGrants, err := riskPolicyVersionURLGrants(ctx, db, policy, authz.ScopeRiskPolicyBlock)
	if err != nil {
		return RiskPolicyVersionState{}, err
	}
	state.AllowedURLGrants = allowedGrants
	state.BlockedURLGrants = blockedGrants
	standing, err := approvalQueries.ListStandingServerDecisionsForProject(ctx, policy.ProjectID)
	if err != nil {
		return RiskPolicyVersionState{}, fmt.Errorf("load risk policy standing decisions: %w", err)
	}
	for _, decision := range standing {
		encoded, err := json.Marshal(struct {
			ID         string   `json:"id"`
			TargetKey  string   `json:"target_key"`
			Decision   string   `json:"decision"`
			Principals []string `json:"principals"`
		}{ID: decision.ID.String(), TargetKey: decision.TargetKey, Decision: decision.Decision, Principals: sortedStrings(decision.GrantedPrincipalUrns)})
		if err != nil {
			return RiskPolicyVersionState{}, fmt.Errorf("encode risk policy standing decision: %w", err)
		}
		state.StandingDecisionState = append(state.StandingDecisionState, string(encoded))
	}
	return state, nil
}

func riskPolicyVersionURLGrants(ctx context.Context, db riskrepo.DBTX, policy policycore.Policy, scope authz.Scope) ([]RiskPolicyVersionGrant, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{OrganizationID: policy.OrganizationID, Scope: scope, ResourceID: policy.ID.String()})
	if err != nil {
		return nil, fmt.Errorf("load risk policy version grants: %w", err)
	}
	state := make([]RiskPolicyVersionGrant, 0, len(grants))
	for _, grant := range grants {
		if grant.Selector[authz.SelectorKeyServerURL] == "" {
			continue
		}
		selector, err := grant.Selector.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("encode risk policy version grant selector: %w", err)
		}
		state = append(state, RiskPolicyVersionGrant{PrincipalURN: grant.PrincipalUrn, Selector: selector})
	}
	return state, nil
}

func (c *riskVersionCodec) encode(kind string, state any) (string, error) {
	if c == nil || len(c.key) == 0 || kind == "" {
		return "", errRiskVersionInvalid
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode risk version state: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte("platform-mcp-risk-version-v1\x00" + kind + "\x00"))
	_, _ = mac.Write(payload)
	envelope, err := json.Marshal(riskVersionEnvelope{Version: riskVersionCodecVersion, Kind: kind, MAC: base64.RawURLEncoding.EncodeToString(mac.Sum(nil))})
	if err != nil {
		return "", fmt.Errorf("encode risk version envelope: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(envelope), nil
}

func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return slices.Compact(result)
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode analyzer configuration: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical analyzer configuration: %w", err)
	}
	return canonical, nil
}
