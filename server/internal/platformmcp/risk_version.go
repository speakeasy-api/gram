package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
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
// optimistic-concurrency token. Grant-backed URLs, standing-decision state, and
// canonical analyzer JSON are included because they can change enforcement
// without changing the policy's public summary. They remain inside the HMAC.
type RiskPolicyVersionState struct {
	Policy                policycore.Policy
	AnalyzerConfig        json.RawMessage
	AllowedURLs           []string
	BlockedURLs           []string
	StandingDecisionState []string
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
	state.AllowedURLs = sortedStrings(state.AllowedURLs)
	state.BlockedURLs = sortedStrings(state.BlockedURLs)
	state.StandingDecisionState = sortedStrings(state.StandingDecisionState)
	slices.SortFunc(state.Policy.DetectionScopes, func(a, b policycore.DetectionScope) int {
		if a.Category < b.Category {
			return -1
		}
		if a.Category > b.Category {
			return 1
		}
		return 0
	})
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
