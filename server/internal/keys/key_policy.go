package keys

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// keyPolicySelector converts a policy selector, rewriting ids to canonical
// UUIDs: grants match by text, so any other spelling would never apply.
func keyPolicySelector(input *gen.APIKeyPolicySelector) (authz.Selector, error) {
	selector := authz.Selector{authz.SelectorKeyResourceKind: input.ResourceKind}
	resourceID, err := canonicalPolicyID(input.ResourceID, true)
	if err != nil {
		return nil, fmt.Errorf("resource_id: %w", err)
	}
	selector[authz.SelectorKeyResourceID] = resourceID
	if input.ProjectID != nil {
		projectID, err := canonicalPolicyID(*input.ProjectID, false)
		if err != nil {
			return nil, fmt.Errorf("project_id: %w", err)
		}
		selector[authz.SelectorKeyProjectID] = projectID
	}
	if input.ClientType != nil {
		selector[authz.SelectorKeyClientType] = *input.ClientType
	}
	return selector, nil
}

func canonicalPolicyID(raw string, wildcard bool) (string, error) {
	if raw == authz.WildcardResource {
		if wildcard {
			return raw, nil
		}
		return "", errors.New("must name a concrete project")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", errors.New("must be a UUID")
	}
	return id.String(), nil
}

// keyPolicyGrants validates an ordinary key's requested policy into role
// grants for its api_key principal, and returns the projects it names. A
// project-bound key's grants must stay in its project, and server grants
// spanning every server must still name a project.
func keyPolicyGrants(forms []*gen.APIKeyPolicyGrant, keyProject uuid.NullUUID) ([]*authz.RoleGrant, []string, error) {
	byScope := make(map[authz.Scope]*authz.RoleGrant)
	seen := make(map[string]struct{})
	var grants []*authz.RoleGrant
	var projects []string
	for _, form := range forms {
		if form == nil || form.Selector == nil {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "each key policy grant needs a selector")
		}
		scope := authz.Scope(form.Scope)
		selector, err := keyPolicySelector(form.Selector)
		if err != nil {
			return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid key policy grant: %s", err.Error())
		}
		if err := authz.ValidateAPIKeyPolicyGrant(scope, selector); err != nil {
			return nil, nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
		}
		project, named := selector[authz.SelectorKeyProjectID]
		if keyProject.Valid && project != keyProject.UUID.String() {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "a project-bound key's policy must name its project")
		}
		serverScope := scope == authz.ScopeMCPConnect || scope == authz.ScopeMCPBlockedConnect
		if serverScope && selector.ResourceID() == authz.WildcardResource && !named {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "a key's grant on every server must name a project")
		}
		encoded, err := selector.MarshalJSON()
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "encode key policy selector")
		}
		if _, dup := seen[string(scope)+string(encoded)]; dup {
			continue
		}
		seen[string(scope)+string(encoded)] = struct{}{}
		if named && !slices.Contains(projects, project) {
			projects = append(projects, project)
		}
		grant, ok := byScope[scope]
		if !ok {
			grant = &authz.RoleGrant{Scope: string(scope), Selectors: nil}
			byScope[scope] = grant
			grants = append(grants, grant)
		}
		grant.Selectors = append(grant.Selectors, selector)
	}
	return grants, projects, nil
}

func grantsToRoleGrants(grants []authz.Grant) []*authz.RoleGrant {
	byScope := make(map[authz.Scope]*authz.RoleGrant)
	var out []*authz.RoleGrant
	for _, grant := range grants {
		role, ok := byScope[grant.Scope]
		if !ok {
			role = &authz.RoleGrant{Scope: string(grant.Scope), Selectors: nil}
			byScope[grant.Scope] = role
			out = append(out, role)
		}
		role.Selectors = append(role.Selectors, grant.Selector)
	}
	return out
}

func keyPolicySelectorModel(selector authz.Selector) *gen.APIKeyPolicySelector {
	model := &gen.APIKeyPolicySelector{
		ResourceKind: selector[authz.SelectorKeyResourceKind],
		ResourceID:   selector[authz.SelectorKeyResourceID],
		ProjectID:    nil,
		ClientType:   nil,
	}
	if value, ok := selector[authz.SelectorKeyProjectID]; ok {
		model.ProjectID = &value
	}
	if value, ok := selector[authz.SelectorKeyClientType]; ok {
		model.ClientType = &value
	}
	return model
}

// keyPolicies loads the policy each ordinary key holds, in one query.
func keyPolicies(ctx context.Context, db accessrepo.DBTX, organizationID string, keyIDs []uuid.UUID) (map[string][]*gen.APIKeyPolicyGrant, error) {
	urns := make([]string, 0, len(keyIDs))
	for _, id := range keyIDs {
		urns = append(urns, urn.NewAPIKeyPrincipal(id).String())
	}
	policies := make(map[string][]*gen.APIKeyPolicyGrant, len(keyIDs))
	if len(urns) == 0 {
		return policies, nil
	}
	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{OrganizationID: organizationID, PrincipalUrns: urns})
	if err != nil {
		return nil, fmt.Errorf("load key policies: %w", err)
	}
	slices.SortFunc(rows, func(a, b accessrepo.GetPrincipalGrantsRow) int {
		return strings.Compare(a.Scope+string(a.Selectors), b.Scope+string(b.Selectors))
	})
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, fmt.Errorf("parse key policy selector: %w", err)
		}
		key := row.PrincipalUrn.String()
		policies[key] = append(policies[key], &gen.APIKeyPolicyGrant{Scope: row.Scope, Selector: keyPolicySelectorModel(selector)})
	}
	return policies, nil
}

func policyModel(grants []*authz.RoleGrant) []*gen.APIKeyPolicyGrant {
	var model []*gen.APIKeyPolicyGrant
	for _, grant := range grants {
		for _, selector := range grant.Selectors {
			model = append(model, &gen.APIKeyPolicyGrant{Scope: grant.Scope, Selector: keyPolicySelectorModel(selector)})
		}
	}
	return model
}

type auditedPolicyGrant struct {
	Scope    string         `json:"scope"`
	Selector authz.Selector `json:"selector"`
}

func policyAuditPayload(grants []*authz.RoleGrant) (json.RawMessage, error) {
	if len(grants) == 0 {
		return nil, nil
	}
	var audited []auditedPolicyGrant
	for _, grant := range grants {
		for _, selector := range grant.Selectors {
			audited = append(audited, auditedPolicyGrant{Scope: grant.Scope, Selector: selector})
		}
	}
	encoded, err := json.Marshal(audited)
	if err != nil {
		return nil, fmt.Errorf("encode key policy for audit: %w", err)
	}
	return encoded, nil
}
