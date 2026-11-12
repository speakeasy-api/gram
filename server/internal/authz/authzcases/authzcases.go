// Package authzcases writes the cases the dashboard's tests replay against its
// own matcher: small sets of grants, and the server's answer for every check
// the dashboard can make against them.
//
// The answers come from the evaluator compiled into this binary, so unlike
// authzgen this package imports authz and needs the generated files to be
// current. `mise run gen:authz` runs authzgen first for that reason.
package authzcases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/authzgen"
	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// Path is where the cases are written, relative to the server directory.
const Path = "../client/dashboard/src/hooks/authz-cases.gen.json"

// File validates the registry and renders the cases for it.
func File() ([]byte, error) {
	if err := registry.Definitions.Validate(); err != nil {
		return nil, fmt.Errorf("the authorization registry is invalid: %w", err)
	}
	return Render(registry.Definitions)
}

// effectiveGrant is one entry of the effective grants response, as the
// dashboard receives it.
type effectiveGrant struct {
	Scope           string              `json:"scope"`
	SubScopes       []string            `json:"subScopes"`
	Selectors       []map[string]string `json:"selectors,omitempty"`
	DirectSelectors []map[string]string `json:"directSelectors,omitempty"`
}

// dashboardCase is one set of grants and the server's answers for checks
// against it. Each check is [scope, resourceId, projectId, allowed], with an
// empty projectId when the kind has no project.
type dashboardCase struct {
	Name   string           `json:"name"`
	Grants []effectiveGrant `json:"grants"`
	Checks [][4]any         `json:"checks"`
}

// apiSelectorKey spells a selector key the way the API returns it:
// resource_id becomes resourceId.
func apiSelectorKey(key string) string {
	parts := strings.Split(key, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func apiSelector(selector authz.Selector) map[string]string {
	out := make(map[string]string, len(selector))
	for key, value := range selector {
		out[apiSelectorKey(key)] = value
	}
	return out
}

// toEffectiveGrants converts grants the way the effective grants endpoint
// does (access.listRoleGrantsFromGrants).
func toEffectiveGrants(grants []authz.Grant) []effectiveGrant {
	direct := authz.DirectSelectorsByScope(grants)
	scoped := authz.GrantsToScopedGrants(grants)
	out := make([]effectiveGrant, 0, len(scoped))
	for _, g := range scoped {
		eg := effectiveGrant{Scope: g.Scope, SubScopes: g.SubScopes, Selectors: nil, DirectSelectors: nil}
		if eg.SubScopes == nil {
			eg.SubScopes = []string{}
		}
		for _, selector := range g.Selectors {
			eg.Selectors = append(eg.Selectors, apiSelector(selector))
		}
		for _, selector := range direct[authz.Scope(g.Scope)] {
			eg.DirectSelectors = append(eg.DirectSelectors, apiSelector(selector))
		}
		out = append(out, eg)
	}
	return out
}

const (
	caseRole      = "role:organization:role-1"
	caseUser      = "user:user-1"
	caseProject   = "project-1"
	caseProject2  = "project-2"
	caseResource  = "resource-1"
	caseResource2 = "resource-2"
)

// caseAtom is one grant a case can hold, with a name for the case title.
type caseAtom struct {
	name  string
	grant authz.Grant
}

func caseGrant(principal string, scope authz.Scope, resourceID, projectID string) authz.Grant {
	selector := authz.NewSelector(scope, resourceID)
	if projectID != "" {
		selector[authz.SelectorKeyProjectID] = projectID
	}
	return authz.Grant{PrincipalUrn: principal, Scope: scope, Selector: selector, Inferred: false}
}

// atomsFor lists, for scope, a grant on everything, on one project, on one
// resource, and a direct grant on that resource.
func atomsFor(scope authz.Scope, inProject bool) []caseAtom {
	project := ""
	if inProject {
		project = caseProject
	}
	atoms := []caseAtom{{"role " + string(scope) + " on all", caseGrant(caseRole, scope, authz.WildcardResource, "")}}
	if inProject {
		atoms = append(atoms, caseAtom{"role " + string(scope) + " on the project", caseGrant(caseRole, scope, authz.WildcardResource, caseProject)})
	}
	return append(atoms,
		caseAtom{"role " + string(scope) + " on the resource", caseGrant(caseRole, scope, caseResource, project)},
		caseAtom{"direct " + string(scope) + " on the resource", caseGrant(caseUser, scope, caseResource, project)},
	)
}

// dashboardCases answers, with the server's evaluator, every check the
// dashboard can make on a resource against small sets of grants: each allow
// on its own, and each allow next to each exclusion of the same kind. Kinds
// the dashboard cannot check (mcp_auth_client binds the connection owner on
// the server) and checks it cannot express (other required dimensions) are
// left out.
func dashboardCases(r registry.Registry) ([]dashboardCase, error) {
	allowedKeys := r.DeriveAllowedSelectorKeys()
	required := r.DeriveRequiredCheckDimensions()
	exclusions := r.DeriveScopeExclusions()
	checkExclusions := authzgen.DashboardExclusions(r)
	// implied maps a scope to the scopes a grant of it also satisfies.
	implied := make(map[registry.Scope][]registry.Scope)
	for checked, satisfiedBy := range r.DeriveScopeExpansions() {
		for _, scope := range satisfiedBy {
			implied[scope] = append(implied[scope], checked)
		}
	}
	for scope := range implied {
		slices.Sort(implied[scope])
	}

	var cases []dashboardCase
	for _, t := range r.Types {
		kind := string(t.Kind)
		if kind == authz.ResourceKindMCPAuthClient {
			continue
		}
		if slices.ContainsFunc(required[kind], func(key string) bool { return key != authz.SelectorKeyProjectID }) {
			continue
		}
		inProject := allowedKeys[kind][authz.SelectorKeyProjectID]

		var allows, blocks []registry.Scope
		for _, s := range r.Scopes() {
			if string(s.Type.Kind) != kind || s.Block {
				continue
			}
			allows = append(allows, s.Scope)
			if exclusion := exclusions[s.Scope]; exclusion != "" && !slices.Contains(blocks, exclusion) {
				blocks = append(blocks, exclusion)
			}
		}

		type target struct{ resource, project string }
		targets := []target{{caseResource, ""}, {caseResource2, ""}}
		if inProject {
			targets = []target{{caseResource, caseProject}, {caseResource2, caseProject}, {caseResource2, caseProject2}}
		}

		// An allow is paired only with the exclusions that can take something
		// it grants away: those of its own scope and of the scopes it implies.
		// An allow on its own is checked against every scope of the kind, so
		// the cases also say what it does not grant; next to an exclusion, only
		// the scopes it grants are checked.
		type caseSet struct {
			atoms  []caseAtom
			checks []registry.Scope
		}
		var sets []caseSet
		for _, scope := range allows {
			covered := append([]registry.Scope{scope}, implied[scope]...)
			var blockAtoms []caseAtom
			for _, block := range blocks {
				if slices.ContainsFunc(covered, func(s registry.Scope) bool { return slices.Contains(checkExclusions[s], block) }) {
					blockAtoms = append(blockAtoms, atomsFor(authz.Scope(block), inProject)...)
				}
			}
			for _, allow := range atomsFor(authz.Scope(scope), inProject) {
				sets = append(sets, caseSet{atoms: []caseAtom{allow}, checks: allows})
				for _, block := range blockAtoms {
					sets = append(sets, caseSet{atoms: []caseAtom{allow, block}, checks: covered})
				}
			}
		}

		for _, set := range sets {
			names := make([]string, 0, len(set.atoms))
			grants := make([]authz.Grant, 0, len(set.atoms))
			for _, atom := range set.atoms {
				names = append(names, atom.name)
				grants = append(grants, atom.grant)
			}
			c := dashboardCase{Name: strings.Join(names, ", "), Grants: toEffectiveGrants(grants), Checks: nil}
			for _, scope := range set.checks {
				for _, tg := range targets {
					check := authz.Check{Scope: authz.Scope(scope), ResourceKind: "", ResourceID: tg.resource, Dimensions: nil}
					if tg.project != "" {
						check.Dimensions = map[string]string{authz.SelectorKeyProjectID: tg.project}
					}
					allowed, err := authz.GrantsAuthorize(grants, check)
					if err != nil {
						return nil, fmt.Errorf("case %q, check %s on %s: %w", c.Name, scope, tg.resource, err)
					}
					c.Checks = append(c.Checks, [4]any{string(scope), tg.resource, tg.project, allowed})
				}
			}
			cases = append(cases, c)
		}
	}
	return cases, nil
}

// Render writes the cases for r, one per line, so a change shows up as the
// cases it changes.
func Render(r registry.Registry) ([]byte, error) {
	cases, err := dashboardCases(r)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, c := range cases {
		line, err := json.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("encode case %q: %w", c.Name, err)
		}
		b.Write(line)
		if i < len(cases)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]\n")
	return b.Bytes(), nil
}
