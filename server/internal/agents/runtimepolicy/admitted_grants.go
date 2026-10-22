package runtimepolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// mcpProjectResolver returns the project owning an MCP resource id, and false
// when the organization has no such resource.
type mcpProjectResolver func(resourceID string) (string, bool, error)

// admitStoredPolicy decodes a credential's stored policy for admission and
// reports whether admission derived an auth client ceiling (see admittedGrants).
func admitStoredPolicy(ctx context.Context, tx accessrepo.DBTX, organizationID string, version int32, raw []byte) (grants []authz.Grant, legacyAuthCeiling bool, err error) {
	v := DelegatedPolicyVersion(version)
	policy, err := DecodeDelegatedPolicy(v, raw)
	if err != nil {
		return nil, false, err
	}
	resolve := func(resourceID string) (string, bool, error) {
		id, err := uuid.Parse(resourceID)
		if err != nil {
			return "", false, nil
		}
		project, err := accessrepo.New(tx).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{OrganizationID: organizationID, ResourceID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("resolve mcp resource project: %w", err)
		}
		return project.String(), true, nil
	}
	grants, err = admittedGrants(v, policy, resolve)
	if err != nil {
		return nil, false, err
	}
	return grants, len(grants) > len(policy.RuntimeGrants()), nil
}

// admittedGrants reads each mcp:connect grant of a policy older than version 5
// with an mcp:auth ceiling of its project reach, since such policies cannot
// name auth clients. A grant on one server that names no project reaches that
// server's project; an unknown server reaches nothing. It is only a ceiling:
// live policies and the paired connect check still bound it. Stored policies
// are never rewritten.
func admittedGrants(version DelegatedPolicyVersion, policy DelegatedPolicy, resolve mcpProjectResolver) ([]authz.Grant, error) {
	grants := policy.RuntimeGrants()
	if version >= DelegatedPolicyVersion5 {
		return grants, nil
	}
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if grant.Scope != authz.ScopeMCPConnect {
			continue
		}
		ceiling := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource}
		project := grant.Selector[authz.SelectorKeyProjectID]
		resource := grant.Selector[authz.SelectorKeyResourceID]
		if (project == "" || project == authz.WildcardResource) && resource != "" && resource != authz.WildcardResource {
			resolved, ok, err := resolve(resource)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			project = resolved
		}
		if project != "" && project != authz.WildcardResource {
			ceiling[authz.SelectorKeyProjectID] = project
		}
		key := ceiling[authz.SelectorKeyProjectID]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		grants = append(grants, authz.Grant{PrincipalUrn: "", Scope: authz.ScopeMCPAuth, Selector: ceiling, Inferred: false})
	}
	return grants, nil
}
