package risk

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AudienceTypeEveryone is the default policy audience: the policy applies to
// every caller and audience grants are not consulted.
const AudienceTypeEveryone = "everyone"

// AudienceTypeTargeted means the policy applies only to principals holding a
// risk_policy:evaluate grant for it (plus the anonymous fail-safe).
const AudienceTypeTargeted = "targeted"

// PolicyApplicability bundles the two RBAC relations that decide whether a
// policy applies to a caller at realtime: the audience (evaluate) grants and
// the bypass (exemption) grants for one policy.
type PolicyApplicability struct {
	// Audience holds risk_policy:evaluate grants — "this policy targets me".
	Audience []authz.Grant
	// Bypass holds risk_policy:bypass grants — "I am exempt from this policy".
	Bypass []authz.Grant
}

// LoadPolicyApplicability loads the evaluate + bypass grants for one policy.
// Spike: two reads; production folds these into a single resource query.
func LoadPolicyApplicability(ctx context.Context, db *pgxpool.Pool, orgID, policyID string) (PolicyApplicability, error) {
	audience, err := authz.ListGrantsForResource(ctx, db, orgID, authz.ScopeRiskPolicyEvaluate, policyID)
	if err != nil {
		return PolicyApplicability{}, fmt.Errorf("load audience grants: %w", err)
	}
	bypass, err := authz.ListGrantsForResource(ctx, db, orgID, authz.ScopeRiskPolicyBypass, policyID)
	if err != nil {
		return PolicyApplicability{}, fmt.Errorf("load bypass grants: %w", err)
	}
	return PolicyApplicability{Audience: audience, Bypass: bypass}, nil
}

// InAudience reports whether a policy's audience includes the caller — the
// "is this policy evaluated for me at all" question (RFC §2.1):
//
//  1. anonymous (no principals) -> TRUE (fail-safe: always scan)
//  2. everyone-tier             -> TRUE
//  3. targeted + in audience    -> TRUE
//  4. targeted + not in audience -> FALSE
//
// Bypass is NOT consulted here — bypass is per-server and is checked at the
// block site (IsBypassed), where the server URL is known. Callers must collapse
// a principal-resolution error to the anonymous case (empty principals).
func InAudience(principals []urn.Principal, everyoneTier bool, audience []authz.Grant) bool {
	if len(principals) == 0 {
		return true
	}
	if everyoneTier {
		return true
	}
	return principalInGrants(principals, audience)
}

// IsBypassed reports whether any caller principal holds a risk_policy:bypass
// grant whose selector matches the check. The check carries the policy id and,
// for shadow-MCP policies, the server URL host; a bypass grant with no
// server_url matches any server (whole-policy bypass), while one with a
// server_url matches only that server. Anonymous callers are never bypassed
// (the fail-safe in InAudience already scans them).
//
// Correctness invariant: for a shadow-MCP policy the check MUST carry the
// server_url dimension, otherwise a server-narrowed grant would match a bare
// check (Selector.Matches skips keys absent from the check). The hook builds
// the check from policy.source, so a shadow-policy check always carries it.
func IsBypassed(principals []urn.Principal, bypass []authz.Grant, check authz.Selector) bool {
	if len(principals) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(principals))
	for _, p := range principals {
		set[p.String()] = struct{}{}
	}
	for _, g := range bypass {
		if _, ok := set[g.PrincipalUrn]; !ok {
			continue
		}
		if _, grantNarrowsServer := g.Selector[authz.SelectorKeyServerURL]; grantNarrowsServer {
			if _, checkHasServer := check[authz.SelectorKeyServerURL]; !checkHasServer {
				continue
			}
		}
		if g.Selector.Matches(check) {
			return true
		}
	}
	return false
}

// Applies is a convenience combiner used in tests: in-audience AND NOT bypassed,
// with the anonymous fail-safe. Production splits these across two call sites
// (audience at policy lookup, bypass at the block site once the server is known).
func Applies(principals []urn.Principal, everyoneTier bool, pa PolicyApplicability, bypassCheck authz.Selector) bool {
	if len(principals) == 0 {
		return true
	}
	if IsBypassed(principals, pa.Bypass, bypassCheck) {
		return false
	}
	return InAudience(principals, everyoneTier, pa.Audience)
}

func principalInGrants(principals []urn.Principal, grants []authz.Grant) bool {
	if len(grants) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(principals))
	for _, p := range principals {
		set[p.String()] = struct{}{}
	}
	for _, g := range grants {
		if _, ok := set[g.PrincipalUrn]; ok {
			return true
		}
	}
	return false
}
