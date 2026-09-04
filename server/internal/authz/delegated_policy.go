package authz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
)

// DelegatedPolicyVersion identifies the persisted delegated-policy format and
// the agent-runtime scope registry used to validate it.
type DelegatedPolicyVersion int32

const (
	DelegatedPolicyVersion1 DelegatedPolicyVersion = 1

	CurrentDelegatedPolicyVersion = DelegatedPolicyVersion1
)

// ErrInvalidDelegatedPolicy marks a policy that must fail closed.
var ErrInvalidDelegatedPolicy = errors.New("invalid delegated policy")

// DelegatedPolicyGrant is one canonical allow grant. The absence of an effect
// field is intentional: delegated policies cannot contain deny-effect rows.
type DelegatedPolicyGrant struct {
	Scope    Scope    `json:"scope"`
	Selector Selector `json:"selector"`
}

// DelegatedPolicy is the versioned credential-policy envelope. Requested is
// retained for display and audit; authorization uses only Effective.
type DelegatedPolicy struct {
	Requested []DelegatedPolicyGrant `json:"requested"`
	Effective []DelegatedPolicyGrant `json:"effective"`

	runtimeGrants []Grant
}

// NewDelegatedPolicyV1 constructs the current canonical policy from requested
// grants and records their explicit implication closure in Effective.
func NewDelegatedPolicyV1(requested []Grant) (DelegatedPolicy, error) {
	return NewDelegatedPolicy(DelegatedPolicyVersion1, requested)
}

// NewDelegatedPolicy constructs a canonical policy suitable for persistence by
// a future credential issuer. Issuers may only use active agent-runtime scopes.
func NewDelegatedPolicy(version DelegatedPolicyVersion, requested []Grant) (DelegatedPolicy, error) {
	if err := validateDelegatedPolicyVersion(version); err != nil {
		return DelegatedPolicy{}, err
	}

	wireRequested := make([]DelegatedPolicyGrant, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, grant := range requested {
		if err := ValidateAgentRuntimeScope(AgentRuntimeScopeRegistryVersion(version), grant.Scope); err != nil {
			return DelegatedPolicy{}, invalidDelegatedPolicy("validate requested scope %q: %v", grant.Scope, err)
		}
		if err := ValidateSelector(grant.Scope, grant.Selector); err != nil {
			return DelegatedPolicy{}, invalidDelegatedPolicy("validate requested selector for %q: %v", grant.Scope, err)
		}

		wireGrant := DelegatedPolicyGrant{Scope: grant.Scope, Selector: cloneSelector(grant.Selector)}
		key, err := delegatedPolicyGrantKey(wireGrant)
		if err != nil {
			return DelegatedPolicy{}, err
		}
		if _, ok := seen[key]; ok {
			return DelegatedPolicy{}, invalidDelegatedPolicy("duplicate requested grant")
		}
		seen[key] = struct{}{}
		wireRequested = append(wireRequested, wireGrant)
	}
	sortDelegatedPolicyGrants(wireRequested)

	effective, err := delegatedPolicyClosure(wireRequested)
	if err != nil {
		return DelegatedPolicy{}, err
	}
	policy := DelegatedPolicy{
		Requested:     wireRequested,
		Effective:     effective,
		runtimeGrants: runtimeGrants(version, effective),
	}
	return policy, nil
}

// EncodeDelegatedPolicy returns canonical JSON for a policy produced by the
// constructor. Mutated, stale, or otherwise noncanonical policies are rejected.
func EncodeDelegatedPolicy(version DelegatedPolicyVersion, policy DelegatedPolicy) ([]byte, error) {
	validated, err := validateDecodedDelegatedPolicy(version, policy)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(struct {
		Requested []DelegatedPolicyGrant `json:"requested"`
		Effective []DelegatedPolicyGrant `json:"effective"`
	}{Requested: validated.Requested, Effective: validated.Effective})
	if err != nil {
		return nil, invalidDelegatedPolicy("encode: %v", err)
	}
	return encoded, nil
}

// DecodeDelegatedPolicy strictly decodes policy JSON loaded with its credential
// row. Unknown and retired scopes are retained for compatibility but omitted
// from RuntimeGrants, so each such entry fails closed without disabling valid
// entries. All other malformed or noncanonical profiles are rejected.
func DecodeDelegatedPolicy(version DelegatedPolicyVersion, raw []byte) (DelegatedPolicy, error) {
	if err := validateDelegatedPolicyVersion(version); err != nil {
		return DelegatedPolicy{}, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return DelegatedPolicy{}, invalidDelegatedPolicy("decode: %v", err)
	}

	var wire struct {
		Requested []DelegatedPolicyGrant `json:"requested"`
		Effective []DelegatedPolicyGrant `json:"effective"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return DelegatedPolicy{}, invalidDelegatedPolicy("decode: %v", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return DelegatedPolicy{}, err
	}
	if wire.Requested == nil || wire.Effective == nil {
		return DelegatedPolicy{}, invalidDelegatedPolicy("requested and effective must be arrays")
	}

	return validateDecodedDelegatedPolicy(version, DelegatedPolicy{
		Requested:     wire.Requested,
		Effective:     wire.Effective,
		runtimeGrants: nil,
	})
}

// RuntimeGrants returns a defensive copy of the stored effective policy entries
// that are active and agent-runtime-safe for the policy version.
func (p DelegatedPolicy) RuntimeGrants() []Grant {
	grants := make([]Grant, len(p.runtimeGrants))
	for i, grant := range p.runtimeGrants {
		grants[i] = Grant{PrincipalUrn: "", Scope: grant.Scope, Selector: cloneSelector(grant.Selector)}
	}
	return grants
}

func validateDecodedDelegatedPolicy(version DelegatedPolicyVersion, policy DelegatedPolicy) (DelegatedPolicy, error) {
	if err := validateDelegatedPolicyVersion(version); err != nil {
		return DelegatedPolicy{}, err
	}
	if policy.Requested == nil || policy.Effective == nil {
		return DelegatedPolicy{}, invalidDelegatedPolicy("requested and effective must be arrays")
	}
	if err := validateCanonicalDelegatedPolicyGrants(version, policy.Requested, "requested"); err != nil {
		return DelegatedPolicy{}, err
	}
	if err := validateCanonicalDelegatedPolicyGrants(version, policy.Effective, "effective"); err != nil {
		return DelegatedPolicy{}, err
	}

	expected, err := delegatedPolicyClosure(policy.Requested)
	if err != nil {
		return DelegatedPolicy{}, err
	}
	if !equalDelegatedPolicyGrants(expected, policy.Effective) {
		return DelegatedPolicy{}, invalidDelegatedPolicy("effective grants are not the requested implication closure")
	}

	validated := DelegatedPolicy{
		Requested:     cloneDelegatedPolicyGrants(policy.Requested),
		Effective:     cloneDelegatedPolicyGrants(policy.Effective),
		runtimeGrants: runtimeGrants(version, policy.Effective),
	}
	return validated, nil
}

func validateCanonicalDelegatedPolicyGrants(version DelegatedPolicyVersion, grants []DelegatedPolicyGrant, field string) error {
	previous := ""
	for i, grant := range grants {
		if grant.Scope == "" {
			return invalidDelegatedPolicy("%s grant %d has an empty scope", field, i)
		}
		if err := validateStoredDelegatedPolicyGrant(version, grant); err != nil {
			return invalidDelegatedPolicy("validate %s grant %d: %v", field, i, err)
		}
		key, err := delegatedPolicyGrantKey(grant)
		if err != nil {
			return err
		}
		if i > 0 && key <= previous {
			return invalidDelegatedPolicy("%s grants are unordered or duplicated", field)
		}
		previous = key
	}
	return nil
}

func validateStoredDelegatedPolicyGrant(version DelegatedPolicyVersion, grant DelegatedPolicyGrant) error {
	if grant.Selector == nil {
		return errors.New("selector must be an object")
	}
	if _, ok := grant.Selector[SelectorKeyResourceKind]; !ok {
		return errors.New("selector must include resource_kind")
	}
	if _, ok := grant.Selector[SelectorKeyResourceID]; !ok {
		return errors.New("selector must include resource_id")
	}

	definition, known := scopeDefinitions[grant.Scope]
	if !known || definition.lifecycle == ScopeLifecycleRetired {
		return nil
	}
	if definition.agentRuntimeSafeSince == 0 || definition.agentRuntimeSafeSince > AgentRuntimeScopeRegistryVersion(version) {
		return fmt.Errorf("scope %q is not agent-runtime-safe", grant.Scope)
	}
	return ValidateSelector(grant.Scope, grant.Selector)
}

func delegatedPolicyClosure(requested []DelegatedPolicyGrant) ([]DelegatedPolicyGrant, error) {
	effective := make([]DelegatedPolicyGrant, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, grant := range requested {
		for _, scope := range AgentRuntimeScopeImplicationClosure(grant.Scope) {
			implied := DelegatedPolicyGrant{Scope: scope, Selector: cloneSelector(grant.Selector)}
			key, err := delegatedPolicyGrantKey(implied)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			effective = append(effective, implied)
		}
	}
	sortDelegatedPolicyGrants(effective)
	return effective, nil
}

func runtimeGrants(version DelegatedPolicyVersion, effective []DelegatedPolicyGrant) []Grant {
	grants := make([]Grant, 0, len(effective))
	for _, grant := range effective {
		definition, known := scopeDefinitions[grant.Scope]
		if !known || definition.lifecycle != ScopeLifecycleActive || definition.agentRuntimeSafeSince == 0 || definition.agentRuntimeSafeSince > AgentRuntimeScopeRegistryVersion(version) {
			continue
		}
		grants = append(grants, Grant{PrincipalUrn: "", Scope: grant.Scope, Selector: cloneSelector(grant.Selector)})
	}
	return grants
}

func sortDelegatedPolicyGrants(grants []DelegatedPolicyGrant) {
	slices.SortFunc(grants, func(a, b DelegatedPolicyGrant) int {
		aKey, _ := delegatedPolicyGrantKey(a)
		bKey, _ := delegatedPolicyGrantKey(b)
		if aKey < bKey {
			return -1
		}
		if aKey > bKey {
			return 1
		}
		return 0
	})
}

func delegatedPolicyGrantKey(grant DelegatedPolicyGrant) (string, error) {
	selector, err := json.Marshal(grant.Selector)
	if err != nil {
		return "", invalidDelegatedPolicy("marshal selector: %v", err)
	}
	return string(grant.Scope) + "\x00" + string(selector), nil
}

func equalDelegatedPolicyGrants(a, b []DelegatedPolicyGrant) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aKey, err := delegatedPolicyGrantKey(a[i])
		if err != nil {
			return false
		}
		bKey, err := delegatedPolicyGrantKey(b[i])
		if err != nil || aKey != bKey {
			return false
		}
	}
	return true
}

func cloneDelegatedPolicyGrants(grants []DelegatedPolicyGrant) []DelegatedPolicyGrant {
	cloned := make([]DelegatedPolicyGrant, len(grants))
	for i, grant := range grants {
		cloned[i] = DelegatedPolicyGrant{Scope: grant.Scope, Selector: cloneSelector(grant.Selector)}
	}
	return cloned
}

func cloneSelector(selector Selector) Selector {
	if selector == nil {
		return nil
	}
	cloned := make(Selector, len(selector))
	maps.Copy(cloned, selector)
	return cloned
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read JSON token: %w", err)
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("read JSON object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := visit(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read JSON closing delimiter: %w", err)
		}
		expected := json.Delim('}')
		if delim == '[' {
			expected = ']'
		}
		if closing != expected {
			return fmt.Errorf("unexpected JSON closing delimiter %q", closing)
		}
		return nil
	}
	return visit()
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return invalidDelegatedPolicy("decode: trailing JSON value")
		}
		return invalidDelegatedPolicy("decode trailing data: %v", err)
	}
	return nil
}

func validateDelegatedPolicyVersion(version DelegatedPolicyVersion) error {
	if version != DelegatedPolicyVersion1 {
		return invalidDelegatedPolicy("unsupported version %d", version)
	}
	return nil
}

func invalidDelegatedPolicy(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDelegatedPolicy, fmt.Sprintf(format, args...))
}
