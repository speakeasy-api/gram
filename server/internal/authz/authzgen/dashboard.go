package authzgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// The dashboard reads the user's effective grants once and decides what to
// show on its own (client/dashboard/src/hooks/useRBAC.ts). Two files keep it
// in step with the server: the tables it needs, generated from the registry,
// and a list of cases answered by the server's evaluator, which the
// dashboard's tests replay against its own matcher.
const (
	dashboardTablesPath = "../client/dashboard/src/lib/authz.gen.ts"
	dashboardCasesPath  = "../client/dashboard/src/hooks/authz-cases.gen.json"
)

const tsHeader = "// Code generated from registry.Definitions by authzgen. DO NOT EDIT.\n// Regenerate with `mise run gen:authz`.\n\n"

// dashboardExclusions maps each scope to the exclusion scopes a check of it
// must clear: its own exclusion, then the broader blocks that also cover it,
// the way Check.exclusionCheck expands on the server.
func dashboardExclusions(r registry.Registry) map[authz.Scope][]authz.Scope {
	exclusions := r.DeriveScopeExclusions()
	expansions := r.DeriveScopeExpansions()
	out := make(map[authz.Scope][]authz.Scope)
	for scope, exclusion := range exclusions {
		if exclusion == "" {
			continue
		}
		out[scope] = append([]authz.Scope{exclusion}, expansions[exclusion]...)
	}
	return out
}

func renderDashboardTables(r registry.Registry) []byte {
	var b strings.Builder
	b.WriteString(tsHeader)

	b.WriteString("/** The resource kind each scope's checks and grant selectors name. */\n")
	b.WriteString("export const scopeResourceKinds: Readonly<Record<string, string>> = {\n")
	kinds := r.DeriveResourceKinds()
	for _, scope := range sortedKeys(kinds) {
		fmt.Fprintf(&b, "  %s: %s,\n", strconv.Quote(string(scope)), strconv.Quote(kinds[scope]))
	}
	b.WriteString("};\n\n")

	b.WriteString("/**\n * The exclusion scopes a check of each scope must clear: its own exclusion,\n * then the broader blocks that also cover it. A block on read also takes\n * write away.\n */\n")
	b.WriteString("export const scopeExclusions: Readonly<\n  Record<string, readonly string[]>\n> = {\n")
	exclusions := dashboardExclusions(r)
	for _, scope := range sortedKeys(exclusions) {
		fmt.Fprintf(&b, "  %s: [\n", strconv.Quote(string(scope)))
		for _, exclusion := range exclusions[scope] {
			fmt.Fprintf(&b, "    %s,\n", strconv.Quote(string(exclusion)))
		}
		b.WriteString("  ],\n")
	}
	b.WriteString("};\n")
	return []byte(b.String())
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

// dimensionValues are the values case grants use for optional dimensions.
var dimensionValues = map[string]string{
	authz.SelectorKeyDisposition:    authz.DispositionReadOnly,
	authz.SelectorKeyServerURL:      "https://example.com",
	authz.SelectorKeyServerIdentity: "identity-1",
	authz.SelectorKeyTool:           "tool-1",
}

// atomsFor lists, for scope, a grant on everything, on one project, on one
// resource, a direct grant on that resource, and, for each optional dimension
// of the kind, a grant on everything limited by that dimension. The dashboard
// never sends optional dimensions, so those grants test that a loose allow
// still matches and a strict exclusion does not.
func atomsFor(scope authz.Scope, inProject bool, dimensions []string) []caseAtom {
	project := ""
	if inProject {
		project = caseProject
	}
	atoms := []caseAtom{{"role " + string(scope) + " on all", caseGrant(caseRole, scope, authz.WildcardResource, "")}}
	if inProject {
		atoms = append(atoms, caseAtom{"role " + string(scope) + " on the project", caseGrant(caseRole, scope, authz.WildcardResource, caseProject)})
	}
	atoms = append(atoms,
		caseAtom{"role " + string(scope) + " on the resource", caseGrant(caseRole, scope, caseResource, project)},
		caseAtom{"direct " + string(scope) + " on the resource", caseGrant(caseUser, scope, caseResource, project)},
	)
	for _, key := range dimensions {
		grant := caseGrant(caseRole, scope, authz.WildcardResource, project)
		grant.Selector[key] = dimensionValues[key]
		atoms = append(atoms, caseAtom{"role " + string(scope) + " on all with " + key + " " + dimensionValues[key], grant})
	}
	return atoms
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
	checkExclusions := dashboardExclusions(r)
	// implied maps a scope to the scopes a grant of it also satisfies.
	implied := make(map[authz.Scope][]authz.Scope)
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
		var dimensions []string
		for _, key := range sortedKeys(allowedKeys[kind]) {
			if key == authz.SelectorKeyProjectID {
				continue
			}
			if _, ok := dimensionValues[key]; !ok {
				return nil, fmt.Errorf("kind %s: no case value for dimension %q; add one to dimensionValues", kind, key)
			}
			dimensions = append(dimensions, key)
		}

		var allows, blocks []authz.Scope
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
			checks []authz.Scope
		}
		var sets []caseSet
		for _, scope := range allows {
			covered := append([]authz.Scope{scope}, implied[scope]...)
			var blockAtoms []caseAtom
			for _, block := range blocks {
				if slices.ContainsFunc(covered, func(s authz.Scope) bool { return slices.Contains(checkExclusions[s], block) }) {
					blockAtoms = append(blockAtoms, atomsFor(block, inProject, dimensions)...)
				}
			}
			for _, allow := range atomsFor(scope, inProject, dimensions) {
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
					check := authz.Check{Scope: scope, ResourceKind: "", ResourceID: tg.resource, Dimensions: nil}
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

// renderDashboardCases writes one case per line, so a change shows up as the
// cases it changes.
func renderDashboardCases(r registry.Registry) ([]byte, error) {
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
