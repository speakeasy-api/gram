package glintnolint

//nolint:glint // noanonymousdefer: whole function suppression // want `move //nolint:glint off top-level func "wholeFunc" onto the specific offending line\(s\); directly above a top-level declaration it suppresses every glint rule for the whole declaration`
func wholeFunc() {}

// docThenDirective has a doc comment in the same group as the directive.
//
//nolint:glint // noanonymousdefer: doc comment group suppression // want `move //nolint:glint off top-level func "docThenDirective"`
func docThenDirective() {}

//nolint:glint // noanonymousdefer: method suppression // want `move //nolint:glint off top-level func "method"`
func (r receiver) method() {}

type receiver struct{}

//nolint:glint // noanonymousdefer: const suppression // want `move //nolint:glint off top-level const "wholeConst"`
const wholeConst = 1

//nolint:glint // noanonymousdefer: var block suppression // want `move //nolint:glint off top-level var "wholeVarA"`
var (
	wholeVarA = 1
	wholeVarB = 2
)

//nolint:glint // noanonymousdefer: type suppression // want `move //nolint:glint off top-level type "wholeType"`
type wholeType struct {
	//nolint:glint // noanonymousdefer: field-scoped directive is allowed
	field int
}

var (
	//nolint:glint // noanonymousdefer: spec-scoped directive is allowed
	scopedVar = 1
)

// nolint:glint // noanonymousdefer: leading space is still a directive // want `move //nolint:glint off top-level func "spacedDirective"`
func spacedDirective() {}

//nolint:glint // noanonymousdefer: detached by a blank line so golangci-lint does not widen it

func detached() {}

func statements() {
	//nolint:glint // noanonymousdefer: own-line directive scoped to the next statement
	defer func() {
	}()

	defer func() {}() //nolint:glint // noanonymousdefer: trailing directive

	defer func() {}() //nolint:glint // noanonymousdefer,notestingrawsql: multiple analyzers

	defer func() {}() //nolint:glint // noanonymousdefer ,  notestingrawsql : spaced list

	defer func() {}() //nolint:glint,errcheck // noanonymousdefer: glint listed with another linter

	defer func() {}() //nolint:errcheck // prose is fine when glint is not listed

	defer func() {}() //nolint:glintnolint // prose is out of scope for glintnolint-only directives

	defer func() {}() //nolint:all // prose is out of scope for nolint:all

	defer func() {}() //nolint:GLINT // noanonymousdefer: linter names are case-insensitive

	defer func() {}() //nolint:glint // this defer is fine because reasons // want `start the //nolint:glint explanation with the suppressed glint analyzer name\(s\) and a colon, e.g. "//nolint:glint // notestingrawsql: <reason>"; got "this defer is fine because reasons`

	defer func() {}() //nolint:glint // transaction note: contains only SQLc queries // want `start the //nolint:glint explanation`

	defer func() {}() //nolint:glint // notarealanalyzer: unknown analyzer // want `"notarealanalyzer" in the //nolint:glint explanation is not a glint analyzer name; use one or more of: .*noanonymousdefer`

	defer func() {}() //nolint:glint // noanonymousdefer,bogus: one unknown in the list // want `"bogus" in the //nolint:glint explanation is not a glint analyzer name`

	defer func() {}() //nolint:glint // no-anonymous-defer: rule key instead of analyzer name // want `start the //nolint:glint explanation`
}

func helper(string) {}
