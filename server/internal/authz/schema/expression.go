package schema

import "slices"

// Op names the kind of a permission expression node.
type Op string

const (
	// OpRel matches grants of a relation declared on the same type.
	OpRel Op = "rel"
	// OpUnion matches when any operand matches.
	OpUnion Op = "union"
	// OpMinus matches the base operand minus the subtracted operand. It is raw
	// set difference: it applies no block precedence and restores nothing.
	OpMinus Op = "minus"
	// OpIs matches when the actor is the principal held in a fact.
	OpIs Op = "is"
	// OpVia evaluates a permission on the resource a fact refers to.
	OpVia Op = "via"
	// OpPerm reuses another permission of the same type.
	OpPerm Op = "perm"
	// OpWhen evaluates its operand only when a named guard holds.
	OpWhen Op = "when"
)

// Expr is an immutable permission expression node. Declarations build it with
// Rel, Union, Minus, Is, Via, Perm and When; the compiler returns a resolved
// copy whose Rel nodes also carry the stored scope and whose Via nodes carry
// the target type.
type Expr struct {
	op         Op
	name       string
	permission string
	guard      string
	scope      string
	target     string
	operands   []Expr
}

func newExpr(op Op, name, permission, guard string, operands []Expr) Expr {
	return Expr{op: op, name: name, permission: permission, guard: guard, scope: "", target: "", operands: operands}
}

// Op returns the node kind.
func (e Expr) Op() Op { return e.op }

// Name returns the relation of a Rel node, the fact of an Is or Via node, or
// the permission of a Perm node. Via nodes name their target permission with
// Permission.
func (e Expr) Name() string { return e.name }

// Scope returns the stored scope of a compiled Rel node, and "" elsewhere.
func (e Expr) Scope() string { return e.scope }

// TargetType returns the type a compiled Via node traverses to.
func (e Expr) TargetType() string { return e.target }

// Permission returns the permission a Via node evaluates on its target.
func (e Expr) Permission() string { return e.permission }

// Guard returns the guard a When node depends on.
func (e Expr) Guard() string { return e.guard }

// Operands returns a copy of the node's children: every branch of a Union,
// the base and subtracted operands of a Minus, and the guarded operand of a
// When.
func (e Expr) Operands() []Expr { return slices.Clone(e.operands) }

// Rel matches grants of the relation declared with Grant on the same type.
func Rel(relation string) Expr {
	return newExpr(OpRel, relation, "", "", nil)
}

// Union matches when any branch matches.
func Union(branches ...Expr) Expr {
	return newExpr(OpUnion, "", "", "", slices.Clone(branches))
}

// Minus matches base unless subtracted also matches the same operation
// instance. Unlike ExcludedBy it applies no block precedence.
func Minus(base, subtracted Expr) Expr {
	return newExpr(OpMinus, "", "", "", []Expr{base, subtracted})
}

// Is matches when the actor is the user principal held in fact, an Attr or
// Derived of type "user".
func Is(fact string) Expr {
	return newExpr(OpIs, fact, "", "", nil)
}

// Via evaluates permission on the resource that fact refers to. The fact must
// be an Attr whose type is a declared resource type, which may be the
// declaring type itself.
func Via(fact, permission string) Expr {
	return newExpr(OpVia, fact, permission, "", nil)
}

// Perm reuses a permission declared on the same type.
func Perm(permission string) Expr {
	return newExpr(OpPerm, permission, "", "", nil)
}

// When evaluates expr only when the named Guard holds. A false guard skips
// expr; it never grants anything on its own.
func When(guard string, expr Expr) Expr {
	return newExpr(OpWhen, "", "", guard, []Expr{expr})
}

// CondOp names the kind of a guard condition node.
type CondOp string

const (
	// CondAll holds when every operand holds.
	CondAll CondOp = "all"
	// CondIsNull holds when a nullable fact is NULL.
	CondIsNull CondOp = "is_null"
	// CondIsFalse holds when a bool fact is false.
	CondIsFalse CondOp = "is_false"
	// CondNullOrEmpty holds when a string fact is NULL or "".
	CondNullOrEmpty CondOp = "null_or_empty"
	// CondPresent holds when a fact has a value.
	CondPresent CondOp = "present"
)

// Cond is an immutable guard condition over a type's facts. Guards are pure:
// they read loaded facts and perform no I/O.
type Cond struct {
	op       CondOp
	fact     string
	operands []Cond
}

// Op returns the node kind.
func (c Cond) Op() CondOp { return c.op }

// Fact returns the fact a leaf condition reads, and "" for All.
func (c Cond) Fact() string { return c.fact }

// Operands returns a copy of an All node's conditions.
func (c Cond) Operands() []Cond { return slices.Clone(c.operands) }

// All holds when every condition holds.
func All(conditions ...Cond) Cond {
	return Cond{op: CondAll, fact: "", operands: slices.Clone(conditions)}
}

// IsNull holds when a nullable fact is NULL. An empty string is not NULL.
func IsNull(fact string) Cond { return Cond{op: CondIsNull, fact: fact, operands: nil} }

// IsFalse holds when a bool fact is false.
func IsFalse(fact string) Cond { return Cond{op: CondIsFalse, fact: fact, operands: nil} }

// NullOrEmpty holds when a string fact is NULL or the empty string.
func NullOrEmpty(fact string) Cond { return Cond{op: CondNullOrEmpty, fact: fact, operands: nil} }

// Present holds when a fact has a value.
func Present(fact string) Cond { return Cond{op: CondPresent, fact: fact, operands: nil} }

// Projection derives a fact from other facts without I/O.
type Projection struct {
	principalType string
	fact          string
}

// PrincipalType returns the principal type the projection produces.
func (p Projection) PrincipalType() string { return p.principalType }

// Fact returns the fact the projection reads.
func (p Projection) Fact() string { return p.fact }

// NonEmptyPrincipal projects a string fact to a principal of principalType.
// NULL and "" both produce no principal.
func NonEmptyPrincipal(principalType, fact string) Projection {
	return Projection{principalType: principalType, fact: fact}
}
