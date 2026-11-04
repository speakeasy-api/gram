package authz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrPlanUnsupported reports a permission an access plan cannot express. The
// whole plan is refused; no part of it is translated.
var ErrPlanUnsupported = errors.New("access plan cannot express the permission")

// ErrPlanStale reports a plan reused for a request it was not compiled for.
var ErrPlanStale = errors.New("access plan does not match the current request")

// ErrPlanTenant reports a plan asked for without an organization and project.
// Collections are listed inside one project, and a plan is bound to it.
var ErrPlanTenant = errors.New("access plan needs an organization and a project")

// PlanTenant is the organization and project a plan lists rows of.
type PlanTenant struct {
	OrganizationID string
	ProjectID      string
}

// ErrPlanNotEnforced reports a plan asked for where authorization is not
// enforced. Callers decide what an unenforced collection shows themselves.
var ErrPlanNotEnforced = errors.New("access plan requested where authorization is not enforced")

// PlanColumn is the SQL that reads one fact of a row.
type PlanColumn struct {
	// SQL is a trusted column expression, never caller input.
	SQL string

	// Nullable reports whether the column can be NULL. NULL reads as a known
	// absent fact.
	Nullable bool
}

// PlanRelation is the SQL that reaches the resource a relationship fact
// names. A row whose join finds no target has the relationship absent.
type PlanRelation struct {
	// From is the trusted FROM clause of the related table, with its alias.
	From string

	// Join is the trusted condition tying the related row to the outer row.
	Join string

	// Target maps the related type's facts.
	Target PlanTable
}

// PlanTable maps one resource type onto SQL.
type PlanTable struct {
	// ID is a trusted expression yielding the resource ID as text.
	ID string

	// Facts maps attribute facts to columns.
	Facts map[string]PlanColumn

	// Relations maps relationship facts to joins.
	Relations map[string]PlanRelation
}

// PlanAdapter is a query adapter's capability: the facts it can read and the
// relationship it can join. A plan that needs anything else is refused.
type PlanAdapter struct {
	// Name identifies the adapter and its version. Plans are bound to it.
	Name string

	// Table maps the planned type.
	Table PlanTable
}

// PlanBinding is everything a plan was compiled for. A plan answers only the
// request whose binding is identical.
type PlanBinding struct {
	OrganizationID string
	ProjectID      string
	Actor          urn.Principal
	Permission     schema.PermissionRef
	// Registry is the compiled registry's fingerprint.
	Registry string
	// Policies fingerprints the live and captured policies, the actor and the
	// represented user.
	Policies string
	Adapter  string
	// Snapshot names the database snapshot the plan runs in, once bound.
	Snapshot string
}

// AccessPlan is a permission compiled for a collection: a predicate over
// rows that admits exactly the resources a point decision would allow. It is
// opaque; an adapter renders it with Predicate.
type AccessPlan struct {
	binding PlanBinding
	root    planNode
}

// Binding returns what the plan was compiled for.
func (p AccessPlan) Binding() PlanBinding { return p.binding }

// Reusable reports whether the plan answers a request with the current
// binding. Any change of tenant, actor, policies, permission, registry,
// adapter or snapshot makes it stale.
func (p AccessPlan) Reusable(current PlanBinding) error {
	if p.root == nil {
		return fmt.Errorf("%w: plan was never compiled", ErrPlanStale)
	}
	if current != p.binding {
		return ErrPlanStale
	}
	return nil
}

// BindSnapshot ties the plan to one database snapshot. A plan is bound once.
func (p AccessPlan) BindSnapshot(token string) (AccessPlan, error) {
	switch {
	case p.root == nil:
		return AccessPlan{}, fmt.Errorf("%w: plan was never compiled", ErrPlanStale)
	case token == "":
		return AccessPlan{}, errors.New("access plan snapshot token is empty")
	case p.binding.Snapshot != "":
		return AccessPlan{}, fmt.Errorf("%w: plan is already bound to a snapshot", ErrPlanStale)
	}
	p.binding.Snapshot = token
	return p, nil
}

// Predicate renders the plan as a SQL boolean expression that is never NULL,
// numbering its parameters from firstParam. It refuses a plan not yet bound
// to a snapshot.
func (p AccessPlan) Predicate(firstParam int) (string, []any, error) {
	if p.root == nil {
		return "", nil, fmt.Errorf("%w: plan was never compiled", ErrPlanStale)
	}
	if p.binding.Snapshot == "" {
		return "", nil, errors.New("access plan is not bound to a snapshot")
	}
	if firstParam < 1 {
		return "", nil, fmt.Errorf("access plan parameters start at $1, not $%d", firstParam)
	}
	r := &planRenderer{next: firstParam, args: nil}
	return p.root.render(r), r.args, nil
}

// Plan compiles a permission over the prepared policies of the request, for
// the request's organization and project. It refuses where authorization is
// not enforced, so callers keep deciding what an unenforced collection shows.
func (c *Client) Plan(ctx context.Context, ref schema.PermissionRef, adapter PlanAdapter) (AccessPlan, error) {
	enforce, err := c.engine.ShouldEnforce(ctx)
	if err != nil {
		return AccessPlan{}, fmt.Errorf("plan %s.%s: %w", ref.Type, ref.Permission, err)
	}
	if !enforce {
		return AccessPlan{}, ErrPlanNotEnforced
	}
	binding, err := c.PlanBinding(ctx, ref, adapter)
	if err != nil {
		return AccessPlan{}, err
	}
	set, ok := c.policySet(ctx)
	if !ok {
		return AccessPlan{}, c.engine.mapError(ctx, ErrMissingGrants)
	}
	plan, err := c.evaluator.Plan(set, ref, adapter, PlanTenant{OrganizationID: binding.OrganizationID, ProjectID: binding.ProjectID})
	if err != nil {
		return AccessPlan{}, err
	}
	plan.binding.Registry = binding.Registry
	return plan, nil
}

// PlanBinding is the binding a plan compiled for this request would carry,
// without a snapshot. Compare it with Reusable before reusing a plan.
func (c *Client) PlanBinding(ctx context.Context, ref schema.PermissionRef, adapter PlanAdapter) (PlanBinding, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return PlanBinding{}, fmt.Errorf("plan %s.%s: %w", ref.Type, ref.Permission, ErrPlanTenant)
	}
	set, ok := c.policySet(ctx)
	if !ok {
		return PlanBinding{}, c.engine.mapError(ctx, ErrMissingGrants)
	}
	policies, err := set.fingerprint()
	if err != nil {
		return PlanBinding{}, err
	}
	return PlanBinding{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		Actor:          set.Actor,
		Permission:     ref,
		Registry:       c.registry.Fingerprint(),
		Policies:       policies,
		Adapter:        adapter.Name,
		Snapshot:       "",
	}, nil
}

// fingerprint identifies the live and captured policies, the actor and the
// represented user, so a plan is never reused after any of them changes.
func (set PolicySet) fingerprint() (string, error) {
	hash := sha256.New()
	write := func(parts ...string) {
		for _, part := range parts {
			// Length-prefixed, so no two different sets encode the same.
			_, _ = fmt.Fprintf(hash, "%d:%s", len(part), part)
		}
	}
	write("actor", set.Actor.String(), "represented_user", set.RepresentedUser)
	for _, policies := range [][]Policy{set.Policies, set.Captured} {
		write(strconv.Itoa(len(policies)))
		for _, policy := range policies {
			write(policy.Principal, strconv.Itoa(len(policy.Grants)))
			for _, grant := range policy.Grants {
				keys := slices.Sorted(maps.Keys(grant.Selector))
				write(grant.PrincipalUrn, string(grant.Scope), strconv.FormatBool(grant.Inferred), strconv.Itoa(len(keys)))
				for _, key := range keys {
					write(key, grant.Selector[key])
				}
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Plan compiles ref over set for adapter, inside tenant. The plan's binding
// carries everything but the registry fingerprint, which Client.Plan adds.
//
// A plan admits a row exactly when Decide would allow the point request for
// that row with facts read from the same row: every live and captured policy
// must allow. Supported nodes are grant relations, authorship, guards over
// adapter columns, one relationship hop, unions, raw differences and reused
// permissions. Block precedence, dimensions, auth-client types and anything
// the adapter cannot read refuse the whole plan.
func (e *RegistryEvaluator) Plan(set PolicySet, ref schema.PermissionRef, adapter PlanAdapter, tenant PlanTenant) (AccessPlan, error) {
	if err := set.validate(); err != nil {
		return AccessPlan{}, err
	}
	if tenant.OrganizationID == "" || tenant.ProjectID == "" {
		return AccessPlan{}, ErrPlanTenant
	}
	if adapter.Name == "" {
		return AccessPlan{}, errors.New("access plan adapter has no name")
	}
	if e.kinds[ref.Type] == ResourceKindMCPAuthClient {
		return AccessPlan{}, fmt.Errorf("%w: %s.%s is limited by represented policies", ErrPlanUnsupported, ref.Type, ref.Permission)
	}
	p, ok := e.permissions[ref]
	if !ok {
		return AccessPlan{}, fmt.Errorf("%w: %s.%s", ErrUnknownPermission, ref.Type, ref.Permission)
	}
	if p.unsupported != nil {
		return AccessPlan{}, fmt.Errorf("plan %s.%s: %w", ref.Type, ref.Permission, p.unsupported)
	}
	req := set.bind(PermissionRequest{
		Permission:      ref,
		ResourceID:      planProbeID,
		Dimensions:      nil,
		StrictMatch:     false,
		RepresentedUser: "",
		Actor:           urn.Principal{Type: "", ID: ""},
		Facts:           nil,
		RequiredFacts:   nil,
	})
	policies := slices.Concat(set.Policies, set.Captured)
	nodes := make([]planNode, 0, len(policies))
	for _, policy := range policies {
		c := planCompiler{e: e, req: req, policy: policy.Grants}
		run := e.evaluation(policy.Grants, req, ref.Type, resourceInstance{id: planProbeID, dimensions: nil, facts: nil})
		node, err := c.permission(ref.Type, p, adapter.Table, run.allowMode(), false)
		if err != nil {
			return AccessPlan{}, fmt.Errorf("plan %s.%s, policy %s: %w", ref.Type, ref.Permission, policy.Principal, err)
		}
		nodes = append(nodes, node)
	}
	policiesFingerprint, err := set.fingerprint()
	if err != nil {
		return AccessPlan{}, err
	}
	return AccessPlan{
		binding: PlanBinding{
			OrganizationID: tenant.OrganizationID,
			ProjectID:      tenant.ProjectID,
			Actor:          set.Actor,
			Permission:     ref,
			Registry:       "",
			Policies:       policiesFingerprint,
			Adapter:        adapter.Name,
			Snapshot:       "",
		},
		root: allOf(nodes),
	}, nil
}

// planProbeID is a resource ID no stored row can have. A relation that allows
// it allows every instance, since grant matching reads only the ID.
const planProbeID = "\x00access-plan-probe"

type planCompiler struct {
	e      *RegistryEvaluator
	req    PermissionRequest
	policy []Grant
}

func (c planCompiler) permission(typeName string, p evaluatorPermission, table PlanTable, mode leafMode, related bool) (planNode, error) {
	if len(p.def.ExcludedBy) > 0 {
		return nil, fmt.Errorf("%w: %s.%s applies block precedence", ErrPlanUnsupported, typeName, p.def.Name)
	}
	if len(p.dimensions) > 0 {
		return nil, fmt.Errorf("%w: %s.%s matches dimensions", ErrPlanUnsupported, typeName, p.def.Name)
	}
	return c.expr(typeName, p.def.Expr, table, mode, related)
}

func (c planCompiler) expr(typeName string, expr schema.Expr, table PlanTable, mode leafMode, related bool) (planNode, error) {
	switch expr.Op() {
	case schema.OpRel:
		return c.relation(typeName, expr, table, mode)
	case schema.OpUnion:
		operands := expr.Operands()
		nodes := make([]planNode, 0, len(operands))
		for _, operand := range operands {
			node, err := c.expr(typeName, operand, table, mode, related)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
		return anyOf(nodes), nil
	case schema.OpMinus:
		operands := expr.Operands()
		base, err := c.expr(typeName, operands[0], table, mode, related)
		if err != nil {
			return nil, err
		}
		run := c.e.evaluation(c.policy, c.req, typeName, resourceInstance{id: planProbeID, dimensions: nil, facts: nil})
		subtracted, err := c.expr(typeName, operands[1], table, run.subtractMode(), related)
		if err != nil {
			return nil, err
		}
		return allOf([]planNode{base, not(subtracted)}), nil
	case schema.OpPerm:
		p := c.e.permissions[schema.PermissionRef{Type: typeName, Permission: expr.Name()}]
		return c.permission(typeName, p, table, mode, related)
	case schema.OpIs:
		if mode.grantsOnly {
			return planConst(false), nil
		}
		return c.is(typeName, expr.Name(), table)
	case schema.OpVia:
		if mode.grantsOnly {
			return planConst(false), nil
		}
		return c.via(typeName, expr, table, mode, related)
	case schema.OpWhen:
		guard, err := c.cond(typeName, c.e.guards[typeName][expr.Guard()], table)
		if err != nil {
			return nil, fmt.Errorf("guard %s: %w", expr.Guard(), err)
		}
		operand, err := c.expr(typeName, expr.Operands()[0], table, mode, related)
		if err != nil {
			return nil, err
		}
		return allOf([]planNode{guard, operand}), nil
	default:
		return nil, fmt.Errorf("%w: node %q", ErrPlanUnsupported, expr.Op())
	}
}

// relation decides a grant relation for every row at once. Grant matching
// reads only the resource ID, so a relation that allows the probe ID allows
// every row; otherwise it allows exactly the IDs the policy's grants name
// that the evaluator itself accepts.
func (c planCompiler) relation(typeName string, expr schema.Expr, table PlanTable, mode leafMode) (planNode, error) {
	if table.ID == "" {
		return nil, fmt.Errorf("%w: adapter has no ID column for %s", ErrPlanUnsupported, typeName)
	}
	probe := c.e.evaluation(c.policy, c.req, typeName, resourceInstance{id: planProbeID, dimensions: nil, facts: nil})
	out := probe.expr(expr, mode)
	if out.err != nil {
		return nil, out.err
	}
	if out.allowed {
		return planConst(true), nil
	}
	ids := []string{}
	for _, grant := range c.policy {
		id := grant.Selector[SelectorKeyResourceID]
		if id == "" || id == WildcardResource || slices.Contains(ids, id) {
			continue
		}
		run := c.e.evaluation(c.policy, c.req, typeName, resourceInstance{id: id, dimensions: nil, facts: nil})
		matched := run.expr(expr, mode)
		if matched.err != nil {
			return nil, matched.err
		}
		if matched.allowed {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return planConst(false), nil
	}
	slices.Sort(ids)
	return planIDIn{column: table.ID, ids: ids}, nil
}

// is matches rows whose user fact names the actor. NULL and "" name nobody,
// and an actor that is not a user is never the author.
func (c planCompiler) is(typeName, fact string, table PlanTable) (planNode, error) {
	declared, ok := c.e.facts[typeName][fact]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s is not declared", ErrPlanUnsupported, typeName, fact)
	}
	source := fact
	if declared.DerivedFrom != "" {
		source = declared.DerivedFrom
	}
	column, ok := table.Facts[source]
	if !ok {
		return nil, fmt.Errorf("%w: adapter cannot read %s.%s", ErrPlanUnsupported, typeName, source)
	}
	actor := c.req.Actor
	if actor.Type != urn.PrincipalTypeUser || actor.ID == "" {
		return planConst(false), nil
	}
	return planEquals{column: column.SQL, value: actor.ID}, nil
}

// via evaluates a permission on the related row, in the same policy and for
// the same actor. Traversal is one hop.
func (c planCompiler) via(typeName string, expr schema.Expr, table PlanTable, mode leafMode, related bool) (planNode, error) {
	if related {
		return nil, fmt.Errorf("%w: traversal from a related %s is more than one hop", ErrPlanUnsupported, typeName)
	}
	relation, ok := table.Relations[expr.Name()]
	if !ok {
		return nil, fmt.Errorf("%w: adapter cannot join %s.%s", ErrPlanUnsupported, typeName, expr.Name())
	}
	target := schema.PermissionRef{Type: expr.TargetType(), Permission: expr.Permission()}
	p, ok := c.e.permissions[target]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrUnknownPermission, target.Type, target.Permission)
	}
	inner, err := c.permission(target.Type, p, relation.Target, mode, true)
	if err != nil {
		return nil, fmt.Errorf("relationship %s: %w", expr.Name(), err)
	}
	if constant, ok := inner.(planConst); ok && !bool(constant) {
		return planConst(false), nil
	}
	return planExists{from: relation.From, join: relation.Join, where: inner}, nil
}

// cond translates a guard. Every condition reads a column the adapter maps,
// and a condition on a nullable boolean is refused, because the evaluator
// cannot decide it for an absent value.
func (c planCompiler) cond(typeName string, cond schema.Cond, table PlanTable) (planNode, error) {
	if cond.Op() == schema.CondAll {
		operands := cond.Operands()
		nodes := make([]planNode, 0, len(operands))
		for _, operand := range operands {
			node, err := c.cond(typeName, operand, table)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
		return allOf(nodes), nil
	}
	name := cond.Fact()
	if relation, ok := table.Relations[name]; ok {
		exists := planExists{from: relation.From, join: relation.Join, where: planConst(true)}
		switch cond.Op() {
		case schema.CondPresent:
			return exists, nil
		case schema.CondIsNull:
			return not(exists), nil
		default:
			return nil, fmt.Errorf("%w: condition %s on relationship %s", ErrPlanUnsupported, cond.Op(), name)
		}
	}
	column, ok := table.Facts[name]
	if !ok {
		return nil, fmt.Errorf("%w: adapter cannot read %s.%s", ErrPlanUnsupported, typeName, name)
	}
	switch cond.Op() {
	case schema.CondIsNull:
		return planSQL{sql: "(" + column.SQL + " IS NULL)"}, nil
	case schema.CondPresent:
		return planSQL{sql: "(" + column.SQL + " IS NOT NULL)"}, nil
	case schema.CondNullOrEmpty:
		return planSQL{sql: "(" + column.SQL + " IS NULL OR " + column.SQL + " = '')"}, nil
	case schema.CondIsFalse:
		if column.Nullable {
			return nil, fmt.Errorf("%w: %s.%s is a nullable boolean", ErrPlanUnsupported, typeName, name)
		}
		return planSQL{sql: "(" + column.SQL + " IS FALSE)"}, nil
	case schema.CondAll:
		return nil, errors.New("unreachable condition")
	default:
		return nil, fmt.Errorf("%w: condition %q", ErrPlanUnsupported, cond.Op())
	}
}

// planNode renders as a SQL boolean that is never NULL, so negation and
// conjunction never meet SQL's unknown truth.
type planNode interface {
	render(r *planRenderer) string
}

type planRenderer struct {
	next int
	args []any
}

func (r *planRenderer) param(value any) string {
	placeholder := fmt.Sprintf("$%d", r.next)
	r.next++
	r.args = append(r.args, value)
	return placeholder
}

type planConst bool

func (n planConst) render(*planRenderer) string {
	if n {
		return "TRUE"
	}
	return "FALSE"
}

type planIDIn struct {
	column string
	ids    []string
}

func (n planIDIn) render(r *planRenderer) string {
	return "COALESCE(" + n.column + " = ANY(" + r.param(n.ids) + "::text[]), FALSE)"
}

type planEquals struct {
	column string
	value  string
}

func (n planEquals) render(r *planRenderer) string {
	return "COALESCE(" + n.column + " = " + r.param(n.value) + "::text, FALSE)"
}

type planSQL struct {
	sql string
}

func (n planSQL) render(*planRenderer) string { return n.sql }

type planExists struct {
	from  string
	join  string
	where planNode
}

func (n planExists) render(r *planRenderer) string {
	return "EXISTS (SELECT 1 FROM " + n.from + " WHERE " + n.join + " AND " + n.where.render(r) + ")"
}

type planNot struct {
	operand planNode
}

func (n planNot) render(r *planRenderer) string {
	return "(NOT " + n.operand.render(r) + ")"
}

type planAll []planNode

func (n planAll) render(r *planRenderer) string {
	return join(n, " AND ", r)
}

type planAny []planNode

func (n planAny) render(r *planRenderer) string {
	return join(n, " OR ", r)
}

func join(nodes []planNode, separator string, r *planRenderer) string {
	parts := make([]string, len(nodes))
	for i, node := range nodes {
		parts[i] = node.render(r)
	}
	return "(" + strings.Join(parts, separator) + ")"
}

// not negates a node, folding constants.
func not(node planNode) planNode {
	if constant, ok := node.(planConst); ok {
		return !constant
	}
	return planNot{operand: node}
}

// allOf folds constants and nested conjunctions, so a plan that grants
// everything renders as TRUE.
func allOf(nodes []planNode) planNode {
	kept := make([]planNode, 0, len(nodes))
	for _, node := range nodes {
		switch n := node.(type) {
		case planConst:
			if !n {
				return planConst(false)
			}
		case planAll:
			kept = append(kept, n...)
		default:
			kept = append(kept, node)
		}
	}
	switch len(kept) {
	case 0:
		return planConst(true)
	case 1:
		return kept[0]
	default:
		return planAll(kept)
	}
}

// anyOf folds constants and nested disjunctions, and merges ID lists over the
// same column into one.
func anyOf(nodes []planNode) planNode {
	kept := make([]planNode, 0, len(nodes))
	merged := map[string]int{}
	add := func(node planNode) {
		in, ok := node.(planIDIn)
		if !ok {
			kept = append(kept, node)
			return
		}
		if i, seen := merged[in.column]; seen {
			previous, _ := kept[i].(planIDIn)
			ids := slices.Concat(previous.ids, in.ids)
			slices.Sort(ids)
			kept[i] = planIDIn{column: in.column, ids: slices.Compact(ids)}
			return
		}
		merged[in.column] = len(kept)
		kept = append(kept, in)
	}
	for _, node := range nodes {
		switch n := node.(type) {
		case planConst:
			if n {
				return planConst(true)
			}
		case planAny:
			for _, operand := range n {
				add(operand)
			}
		default:
			add(node)
		}
	}
	switch len(kept) {
	case 0:
		return planConst(false)
	case 1:
		return kept[0]
	default:
		return planAny(kept)
	}
}
