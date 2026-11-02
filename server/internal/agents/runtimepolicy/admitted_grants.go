package runtimepolicy

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// mcpProjectResolver maps MCP resource ids to their owning projects in one
// lookup, leaving out ids the organization has no resource for.
type mcpProjectResolver func(resourceIDs []string) (map[string]string, error)

// admitStoredPolicy decodes a credential's stored policy for admission and
// reports whether admission derived an auth client ceiling (see admittedGrants).
func admitStoredPolicy(ctx context.Context, tx accessrepo.DBTX, organizationID string, version int32, raw []byte) (grants []authz.Grant, legacyAuthCeiling bool, err error) {
	v := DelegatedPolicyVersion(version)
	policy, err := DecodeDelegatedPolicy(v, raw)
	if err != nil {
		return nil, false, err
	}
	resolve := func(resourceIDs []string) (map[string]string, error) {
		ids := make([]uuid.UUID, 0, len(resourceIDs))
		requested := make(map[uuid.UUID][]string, len(resourceIDs))
		for _, resourceID := range resourceIDs {
			if id, err := uuid.Parse(resourceID); err == nil {
				ids = append(ids, id)
				requested[id] = append(requested[id], resourceID)
			}
		}
		projects := make(map[string]string, len(ids))
		if len(ids) == 0 {
			return projects, nil
		}
		rows, err := accessrepo.New(tx).FindMCPResourceProjects(ctx, accessrepo.FindMCPResourceProjectsParams{OrganizationID: organizationID, ResourceIds: ids})
		if err != nil {
			return nil, fmt.Errorf("resolve mcp resource projects: %w", err)
		}
		for _, row := range rows {
			for _, resourceID := range requested[row.ResourceID] {
				projects[resourceID] = row.ProjectID.String()
			}
		}
		return projects, nil
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
	var unresolved []string
	for _, grant := range grants {
		if grant.Scope == authz.ScopeMCPConnect && needsServerProject(grant.Selector) {
			unresolved = append(unresolved, grant.Selector[authz.SelectorKeyResourceID])
		}
	}
	var serverProjects map[string]string
	if len(unresolved) > 0 {
		var err error
		if serverProjects, err = resolve(unresolved); err != nil {
			return nil, err
		}
	}
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if grant.Scope != authz.ScopeMCPConnect {
			continue
		}
		ceiling := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource}
		project := grant.Selector[authz.SelectorKeyProjectID]
		if needsServerProject(grant.Selector) {
			resolved, ok := serverProjects[grant.Selector[authz.SelectorKeyResourceID]]
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

// needsServerProject reports a grant on one server that names no project.
func needsServerProject(selector authz.Selector) bool {
	project, resource := selector[authz.SelectorKeyProjectID], selector[authz.SelectorKeyResourceID]
	return (project == "" || project == authz.WildcardResource) && resource != "" && resource != authz.WildcardResource
}
