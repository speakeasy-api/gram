package authz

import (
	"maps"
	"slices"
)

// This file uses only long-standing API, so an older evaluator can answer the
// same cases and the snapshots can be compared. API that may differ lives in
// the two helpers at the end of decision_snapshot_test.go.

type snapshotFile struct {
	Grants []snapshotGrant `json:"grants"`
	Checks []snapshotCheck `json:"checks"`
	Groups []snapshotGroup `json:"groups"`
}

type snapshotGrant struct {
	Principal string            `json:"principal"`
	Scope     string            `json:"scope"`
	Selector  map[string]string `json:"selector"`
	Inferred  bool              `json:"inferred,omitempty"`
}

// snapshotCheck records how a caller builds a check, not the Check value, so
// the replay keeps working when the Check type changes.
type snapshotCheck struct {
	Op          string            `json:"op,omitempty"`
	Scope       string            `json:"scope,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	ID          string            `json:"id"`
	Dimensions  map[string]string `json:"dimensions,omitempty"`
	Project     string            `json:"project,omitempty"`
	Tool        string            `json:"tool,omitempty"`
	Disposition string            `json:"disposition,omitempty"`
	Probe       bool              `json:"probe,omitempty"`
}

type snapshotGroup struct {
	Name   string        `json:"name"`
	Checks []int         `json:"checks"`
	Sets   []snapshotSet `json:"sets"`
}

// Answers has one character per check: 1 allowed, 0 refused, e invalid check.
type snapshotSet struct {
	Grants  []int  `json:"grants"`
	Answers string `json:"answers"`
}

const (
	snapshotUser     = "user:u1"
	snapshotRole     = "role:organization:r1"
	snapshotEveryone = "user:all"
	snapshotProjectA = "p1"
	snapshotProjectB = "p2"
)

type snapshotBuilder struct {
	file   snapshotFile
	grants map[string]int
	checks map[string]int
}

func newSnapshotBuilder() *snapshotBuilder {
	return &snapshotBuilder{file: snapshotFile{Grants: nil, Checks: nil, Groups: nil}, grants: map[string]int{}, checks: map[string]int{}}
}

func (b *snapshotBuilder) grant(g snapshotGrant) int {
	key := g.Principal + " " + g.Scope + " " + describeSelectorKey(g.Selector)
	if g.Inferred {
		key += " inferred"
	}
	if i, ok := b.grants[key]; ok {
		return i
	}
	b.file.Grants = append(b.file.Grants, g)
	b.grants[key] = len(b.file.Grants) - 1
	return len(b.file.Grants) - 1
}

func (b *snapshotBuilder) check(c snapshotCheck) int {
	key := c.Op + "|" + c.Scope + "|" + c.Kind + "|" + c.ID + "|" + describeSelectorKey(c.Dimensions) + "|" + c.Project + "|" + c.Tool + "|" + c.Disposition
	if c.Probe {
		key += "|probe"
	}
	if i, ok := b.checks[key]; ok {
		return i
	}
	b.file.Checks = append(b.file.Checks, c)
	b.checks[key] = len(b.file.Checks) - 1
	return len(b.file.Checks) - 1
}

func (b *snapshotBuilder) group(name string, checks []int, sets [][]int) {
	group := snapshotGroup{Name: name, Checks: checks, Sets: make([]snapshotSet, 0, len(sets))}
	for _, set := range sets {
		grants := make([]Grant, len(set))
		for i, g := range set {
			grants[i] = b.file.Grants[g].build()
		}
		answers := make([]byte, len(checks))
		for i, c := range checks {
			answers[i] = snapshotAnswer(grants, b.file.Checks[c].build())
		}
		group.Sets = append(group.Sets, snapshotSet{Grants: set, Answers: string(answers)})
	}
	b.file.Groups = append(b.file.Groups, group)
}

func snapshotAnswer(grants []Grant, check Check) byte {
	allowed, err := GrantsAuthorize(grants, check)
	switch {
	case err != nil:
		return 'e'
	case allowed:
		return '1'
	default:
		return '0'
	}
}

func (g snapshotGrant) build() Grant {
	grant := NewGrantWithSelector(Scope(g.Scope), maps.Clone(g.Selector))
	grant.PrincipalUrn = g.Principal
	grant.Inferred = g.Inferred
	return grant
}

func (c snapshotCheck) build() Check {
	var check Check
	if c.Op == "" {
		check = snapshotRawCheck(Scope(c.Scope), c.Kind, c.ID, maps.Clone(c.Dimensions))
	} else {
		server := snapshotMCPServer(c.ID, c.Project)
		switch c.Op {
		case "read":
			check = server.Read()
		case "write":
			check = server.Write()
		case "connect":
			check = server.Connect()
		case "tool_call":
			check = server.ToolCall(c.Tool, c.Disposition)
		case "auth_client":
			check = MCPAuthClientCheck(c.ID, MCPAuthClientDimensions{ClientType: c.Kind, ProjectID: c.Project, ConnectionOwner: c.Dimensions[SelectorKeyConnectionOwner], UpstreamClient: c.Dimensions[SelectorKeyUpstreamClient]})
		default:
			panic("unknown snapshot check op " + c.Op)
		}
	}
	if c.Probe {
		check = check.Probe()
	}
	return check
}

func describeSelectorKey(m map[string]string) string {
	keys := slices.Sorted(maps.Keys(m))
	out := ""
	for _, k := range keys {
		out += k + "=" + m[k] + ";"
	}
	return out
}

func buildDecisionSnapshot() snapshotFile {
	b := newSnapshotBuilder()
	b.everyPermission()
	b.mcpSingleGrants()
	b.mcpServerGrantPairs()
	b.mcpAllowsWithLimitedBlocks()
	b.authClients()
	return b.file
}

func (b *snapshotBuilder) everyPermission() {
	scopes := slices.Collect(maps.Keys(scopeExpansions))
	slices.Sort(scopes)
	for _, scope := range scopes {
		// authClients covers mcp:auth, which needs client dimensions.
		if scope == ScopeRoot || scope == ScopeMCPAuth || IsBlocklistScope(scope) {
			continue
		}
		kind := ResourceKindForScope(scope)
		dimensions := map[string]string{}
		for _, key := range RequiredCheckDimensions(kind) {
			dimensions[key] = snapshotDimensionValue(key)
		}
		checks := []int{
			b.check(snapshotCheck{Scope: string(scope), Kind: kind, ID: "r1", Dimensions: dimensions}),
			b.check(snapshotCheck{Scope: string(scope), Kind: kind, ID: "r2", Dimensions: dimensions}),
		}
		allow := func(principal, s, resource string) int {
			return b.grant(snapshotGrant{Principal: principal, Scope: s, Selector: NewSelector(Scope(s), resource)})
		}
		on := func(principal, resource string) int { return allow(principal, string(scope), resource) }
		sets := [][]int{
			{},
			{on(snapshotUser, "r1")},
			{on(snapshotUser, WildcardResource)},
			{on(snapshotRole, WildcardResource)},
			{on(snapshotEveryone, WildcardResource)},
			{b.grant(snapshotGrant{Principal: snapshotUser, Scope: string(ScopeRoot), Selector: NewSelector(ScopeRoot, WildcardResource)})},
		}
		for _, implying := range scopeExpansions[scope] {
			sets = append(sets, []int{allow(snapshotUser, string(implying), WildcardResource)})
		}
		if blockScope, ok := ExclusionScopeFor(scope); ok {
			block := func(principal string) int { return allow(principal, string(blockScope), "r1") }
			inferred := b.grant(snapshotGrant{Principal: snapshotUser, Scope: string(scope), Selector: b.file.Grants[on(snapshotUser, "r1")].Selector, Inferred: true})
			sets = append(sets,
				[]int{on(snapshotUser, WildcardResource), block(snapshotRole)},
				[]int{on(snapshotUser, WildcardResource), block(snapshotUser)},
				[]int{on(snapshotUser, "r1"), block(snapshotRole)},
				[]int{on(snapshotUser, "r1"), block(snapshotEveryone)},
				[]int{on(snapshotUser, "r1"), block(snapshotUser)},
				[]int{inferred, block(snapshotRole)},
				[]int{on(snapshotRole, WildcardResource), block(snapshotEveryone)},
			)
		}
		b.group("permission "+string(scope), checks, sets)
	}
}

func snapshotDimensionValue(key string) string {
	if key == SelectorKeyProjectID {
		return snapshotProjectA
	}
	return key + "-1"
}

var (
	snapshotMCPServers   = [][2]string{{"s1", snapshotProjectA}, {"s2", snapshotProjectA}, {"s3", snapshotProjectB}}
	snapshotTools        = []string{"t1", "t2"}
	snapshotDispositions = []string{DispositionReadOnly, DispositionDestructive, DispositionIdempotent, DispositionOpenWorld, "unannotated"}
	snapshotMCPScopes    = []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite, ScopeMCPBlockedConnect}
)

func (b *snapshotBuilder) mcpChecks(withCalls bool) []int {
	var checks []int
	for _, server := range snapshotMCPServers {
		id, project := server[0], server[1]
		for _, op := range []string{"read", "write", "connect"} {
			checks = append(checks, b.check(snapshotCheck{Op: op, ID: id, Project: project}))
		}
		if withCalls {
			checks = append(checks, b.mcpCallChecks(id, project)...)
		}
	}
	return checks
}

func (b *snapshotBuilder) mcpCallChecks(id, project string) []int {
	var checks []int
	for _, tool := range append([]string{""}, snapshotTools...) {
		for _, disposition := range append([]string{""}, snapshotDispositions...) {
			checks = append(checks, b.check(snapshotCheck{Op: "tool_call", ID: id, Project: project, Tool: tool, Disposition: disposition}))
			if tool == "" || disposition == "" {
				checks = append(checks, b.check(snapshotCheck{Op: "tool_call", ID: id, Project: project, Tool: tool, Disposition: disposition, Probe: true}))
			}
		}
	}
	return checks
}

// A call that leaves out a blocked tool or disposition only shows up here.
func (b *snapshotBuilder) mcpAllowsWithLimitedBlocks() {
	var checks []int
	for _, server := range snapshotMCPServers {
		checks = append(checks, b.mcpCallChecks(server[0], server[1])...)
	}
	var allows, blocks []int
	for _, g := range b.mcpGrants(true) {
		grant := b.file.Grants[g]
		_, tool := grant.Selector[SelectorKeyTool]
		_, disposition := grant.Selector[SelectorKeyDisposition]
		switch {
		case grant.Scope == string(ScopeMCPConnect) && !tool && !disposition:
			allows = append(allows, g)
		case grant.Scope == string(ScopeMCPBlockedConnect) && (tool || disposition):
			blocks = append(blocks, g)
		}
	}
	var sets [][]int
	for _, allow := range allows {
		for _, block := range blocks {
			sets = append(sets, []int{allow, block})
		}
	}
	b.group("mcp allows with limited blocks", checks, sets)
}

// Grants ValidateSelector refuses are kept: rows stored before a rule existed
// are still answered.
func (b *snapshotBuilder) mcpGrants(withLimits bool) []int {
	var grants []int
	for _, principal := range []string{snapshotUser, snapshotRole, snapshotEveryone} {
		for _, scope := range snapshotMCPScopes {
			for _, resource := range []string{"s1", WildcardResource} {
				for _, project := range []string{"", snapshotProjectA} {
					tools, dispositions := []string{""}, []string{""}
					if withLimits {
						tools = []string{"", "t1"}
						dispositions = append([]string{""}, snapshotDispositions...)
					}
					for _, tool := range tools {
						for _, disposition := range dispositions {
							selector := NewSelector(scope, resource)
							if project != "" {
								selector[SelectorKeyProjectID] = project
							}
							if tool != "" {
								selector[SelectorKeyTool] = tool
							}
							if disposition != "" {
								selector[SelectorKeyDisposition] = disposition
							}
							grants = append(grants, b.grant(snapshotGrant{Principal: principal, Scope: string(scope), Selector: selector}))
						}
					}
				}
			}
		}
	}
	return grants
}

func (b *snapshotBuilder) mcpSingleGrants() {
	checks := b.mcpChecks(true)
	grants := b.mcpGrants(true)
	sets := [][]int{{}}
	for _, g := range grants {
		sets = append(sets, []int{g})
	}
	b.group("mcp single grants", checks, sets)
}

// Pairs are where precedence shows.
func (b *snapshotBuilder) mcpServerGrantPairs() {
	checks := b.mcpChecks(false)
	grants := b.mcpGrants(false)
	var sets [][]int
	for i := range grants {
		for j := i + 1; j < len(grants); j++ {
			sets = append(sets, []int{grants[i], grants[j]})
		}
	}
	b.group("mcp server grant pairs", checks, sets)
}

// No represented user is bound here, so personal client checks are refused.
func (b *snapshotBuilder) authClients() {
	var checks []int
	for _, project := range []string{snapshotProjectA, snapshotProjectB} {
		for _, client := range []string{"c1", "c2"} {
			// Literal names: older versions lack constants for some types.
			for _, clientType := range []string{"service_account", "static_credential", "pass_through", "ema"} {
				checks = append(checks, b.check(snapshotCheck{Op: "auth_client", ID: client, Kind: clientType, Project: project}))
			}
			checks = append(checks, b.check(snapshotCheck{Op: "auth_client", ID: client, Kind: "remote_session", Project: project, Dimensions: map[string]string{SelectorKeyConnectionOwner: "u1", SelectorKeyUpstreamClient: "up1"}}))
		}
	}
	grant := func(principal string, scope Scope, selector Selector) int {
		return b.grant(snapshotGrant{Principal: principal, Scope: string(scope), Selector: selector})
	}
	oneClient := grant(snapshotUser, ScopeMCPAuth, AuthClientSelector("c1", ""))
	inProject := grant(snapshotUser, ScopeMCPAuth, AuthClientSelector(WildcardResource, snapshotProjectA))
	personal := grant(snapshotEveryone, ScopeMCPAuth, PersonalAuthClientSelector())
	blockOne := grant(snapshotRole, ScopeMCPBlockedAuth, AuthClientSelector("c1", ""))
	root := grant(snapshotUser, ScopeRoot, NewSelector(ScopeRoot, WildcardResource))
	b.group("auth clients", checks, [][]int{{}, {oneClient}, {inProject}, {personal}, {root}, {oneClient, blockOne}, {inProject, blockOne}, {personal, blockOne}, {root, blockOne}})
}
