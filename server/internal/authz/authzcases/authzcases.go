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
	"maps"
	"slices"
	"strings"

	"github.com/ettle/strcase"

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
//
// Calls ask the same through the dashboard's check constructors: each is
// [constructor, args, method, probe, allowed], answered by the Go function of
// the same name, so authz.mcpServer("resource-1", "project-1").write() must
// get the answer authz.MCPServer(...).Write() gets.
type dashboardCase struct {
	Name   string           `json:"name"`
	Grants []effectiveGrant `json:"grants"`
	Checks [][4]any         `json:"checks"`
	Calls  [][5]any         `json:"calls"`
}

// constructor is the Go twin of one dashboard check constructor method,
// listed in constructors_gen.go.
type constructor struct {
	name, method, scope string
	// parent is "project" or "org" for a collection, whose one argument is
	// that parent's id, and empty for a constructor naming one resource.
	parent string
	// ref is, for a collection, the constructor naming one of its resources.
	ref string
	// keys has one entry per argument: the selector key it fills in, empty
	// for the resource id or a collection's parent.
	keys  []string
	build func(args []string) authz.Check
}

// caseTarget is one resource a case asks about, and its project.
type caseTarget struct{ resource, project string }

// caseOtherResource is a resource no case grant names.
const caseOtherResource = "resource-other"

// caseOrg is the organization collections of org resources are asked about.
const caseOrg = "org-1"

func apiSelector(selector authz.Selector) map[string]string {
	out := make(map[string]string, len(selector))
	for key, value := range selector {
		// The API spells selector keys in camel case: resource_id is resourceId.
		out[strcase.ToCamel(key)] = value
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

// Checks also ask about caseResource2 and caseProject2, so each case records
// what a grant must not reach.
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

// The dashboard never sends these dimensions, so atomsFor's limited grants pin
// how a grant limited to one answers a check that leaves it out. Any valid,
// non-empty value works.
var dimensionValues = map[string]string{
	authz.SelectorKeyDisposition:    authz.DispositionReadOnly,
	authz.SelectorKeyServerURL:      "https://example.com",
	authz.SelectorKeyServerIdentity: "identity-1",
	authz.SelectorKeyTool:           "tool-1",
}

// atomsFor lists, for scope, a grant on everything, on one project, on one
// resource, a direct grant on that resource, and, for each optional dimension
// of the kind, a grant on everything limited by that dimension. The dashboard
// never sends optional dimensions and asks as a probe, so those grants test
// that a limited allow still matches its question and a limited exclusion
// does not.
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
		var dimensions []string
		for _, key := range slices.Sorted(maps.Keys(allowedKeys[kind])) {
			if key == authz.SelectorKeyProjectID {
				continue
			}
			if _, ok := dimensionValues[key]; !ok {
				return nil, fmt.Errorf("kind %s: no case value for dimension %q; add one to dimensionValues", kind, key)
			}
			dimensions = append(dimensions, key)
		}

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

		targets := []caseTarget{{caseResource, ""}, {caseResource2, ""}}
		if inProject {
			targets = []caseTarget{{caseResource, caseProject}, {caseResource2, caseProject}, {caseResource2, caseProject2}}
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
					blockAtoms = append(blockAtoms, atomsFor(authz.Scope(block), inProject, dimensions)...)
				}
			}
			for _, allow := range atomsFor(authz.Scope(scope), inProject, dimensions) {
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
			c := dashboardCase{Name: strings.Join(names, ", "), Grants: toEffectiveGrants(grants), Checks: nil, Calls: nil}
			for _, scope := range set.checks {
				for _, tg := range targets {
					check := authz.Check{Scope: authz.Scope(scope), ResourceKind: "", ResourceID: tg.resource, Dimensions: nil} //nolint:glint // norawauthzchecks: generated cases pair every scope with every target to test the dashboard matcher
					if tg.project != "" {
						check.Dimensions = map[string]string{authz.SelectorKeyProjectID: tg.project}
					}
					// The dashboard asks whether to show a control, not
					// whether to perform an action, so its questions are
					// probes: a grant key it does not send still matches.
					check = check.Probe()
					allowed, err := authz.GrantsAuthorize(grants, check)
					if err != nil {
						return nil, fmt.Errorf("case %q, check %s on %s: %w", c.Name, scope, tg.resource, err)
					}
					c.Checks = append(c.Checks, [4]any{string(scope), tg.resource, tg.project, allowed})
				}
			}
			calls, err := constructorCalls(grants, set.checks, targets)
			if err != nil {
				return nil, fmt.Errorf("case %q: %w", c.Name, err)
			}
			c.Calls = calls
			cases = append(cases, c)
		}
	}
	return cases, nil
}

// constructorCalls answers, for one set of grants, every constructor method
// whose scope the set checks: a ref on each target, a collection on each
// parent, each also as a probe.
func constructorCalls(grants []authz.Grant, scopes []registry.Scope, targets []caseTarget) ([][5]any, error) {
	calls := [][5]any{}
	answer := func(c constructor, args []string, probe bool) error {
		allowed, err := constructorAnswer(grants, c, args, probe)
		if err != nil {
			return fmt.Errorf("%s(%s).%s: %w", c.name, strings.Join(args, ", "), c.method, err)
		}
		calls = append(calls, [5]any{c.name, args, c.method, probe, allowed})
		return nil
	}
	for _, c := range constructors {
		if !slices.Contains(scopes, registry.Scope(c.scope)) {
			continue
		}
		var argSets [][]string
		switch c.parent {
		case "project":
			argSets = [][]string{{caseProject}, {caseProject2}}
		case "org":
			argSets = [][]string{{caseOrg}}
		default:
			for _, tg := range targets {
				argSets = append(argSets, refArgs(c, tg.resource, tg.project))
			}
		}
		for _, args := range argSets {
			for _, probe := range []bool{false, true} {
				if err := answer(c, args, probe); err != nil {
					return nil, err
				}
			}
		}
	}
	return calls, nil
}

// refArgs fills in a constructor's arguments for one resource in project. A
// kind without a project still gets one for a method that takes it.
func refArgs(c constructor, resource, project string) []string {
	if project == "" {
		project = caseProject
	}
	args := make([]string, 0, len(c.keys))
	for _, key := range c.keys {
		if key == authz.SelectorKeyProjectID {
			args = append(args, project)
		} else {
			args = append(args, resource)
		}
	}
	return args
}

// constructorAnswer is the server's answer to one constructor call. A probe
// on a collection asks whether the caller could act on any resource of the
// kind in that parent, which the server answers by listing them: the
// collection itself, each case resource, and one no grant names.
func constructorAnswer(grants []authz.Grant, c constructor, args []string, probe bool) (bool, error) {
	check := c.build(args)
	if probe {
		check = check.Probe()
	}
	allowed, err := authz.GrantsAuthorize(grants, check)
	if err != nil || allowed || !probe || c.parent == "" {
		return allowed, err
	}
	ref := findConstructor(c.ref, c.method)
	if ref == nil {
		return false, fmt.Errorf("collection %s has no %s.%s", c.name, c.ref, c.method)
	}
	project := ""
	if c.parent == "project" {
		project = args[0]
	}
	for _, resource := range []string{caseResource, caseResource2, caseOtherResource} {
		allowed, err := authz.GrantsAuthorize(grants, ref.build(refArgs(*ref, resource, project)).Probe())
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

func findConstructor(name, method string) *constructor {
	for i := range constructors {
		if constructors[i].name == name && constructors[i].method == method {
			return &constructors[i]
		}
	}
	return nil
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
