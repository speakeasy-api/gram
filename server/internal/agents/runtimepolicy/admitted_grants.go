package runtimepolicy

import (
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// admitStoredPolicy decodes a credential's stored policy for admission and
// reports whether admission derived an auth client ceiling (see admittedGrants).
func admitStoredPolicy(version int32, raw []byte) (grants []authz.Grant, legacyAuthCeiling bool, err error) {
	v := DelegatedPolicyVersion(version)
	policy, err := DecodeDelegatedPolicy(v, raw)
	if err != nil {
		return nil, false, err
	}
	grants = admittedGrants(v, policy)
	return grants, len(grants) > len(policy.RuntimeGrants()), nil
}

// admittedGrants reads each mcp:connect grant of a policy older than version 5
// with an mcp:auth ceiling of its project reach, since such policies cannot
// name auth clients. It is only a ceiling: live policies and the paired
// connect check still bound it. Stored policies are never rewritten.
func admittedGrants(version DelegatedPolicyVersion, policy DelegatedPolicy) []authz.Grant {
	grants := policy.RuntimeGrants()
	if version >= DelegatedPolicyVersion5 {
		return grants
	}
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if grant.Scope != authz.ScopeMCPConnect {
			continue
		}
		ceiling := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource}
		if project := grant.Selector[authz.SelectorKeyProjectID]; project != "" && project != authz.WildcardResource {
			ceiling[authz.SelectorKeyProjectID] = project
		}
		key := ceiling[authz.SelectorKeyProjectID]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		grants = append(grants, authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPAuth, Selector: ceiling, Inferred: false})
	}
	return grants
}
