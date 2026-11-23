package authz

import (
	"context"
	"errors"
	"sync"
)

// Denial names the permission a request lacked. It is what a client can be
// told about a refusal: the scope and the kind of resource it was checked
// against. The resource id and the loaded grants stay on the server.
type Denial struct {
	Scope        Scope
	ResourceKind string
}

type denialRecorderKey struct{}

// denialRecorder keeps the most recent denial of a request. A handler may
// probe a permission, swallow the refusal and continue, so the last denial
// recorded is the one closest to the response that is finally written.
type denialRecorder struct {
	mu     sync.Mutex
	denial *Denial
}

// WithDenialRecorder returns a context that remembers the denial a later
// Require reports. HTTP middleware installs it per request and reads it back
// with RecordedDenial when the response is a 403.
func WithDenialRecorder(ctx context.Context) context.Context {
	return context.WithValue(ctx, denialRecorderKey{}, &denialRecorder{mu: sync.Mutex{}, denial: nil})
}

// RecordedDenial returns the last denial recorded on ctx, if any.
func RecordedDenial(ctx context.Context) (Denial, bool) {
	recorder, ok := ctx.Value(denialRecorderKey{}).(*denialRecorder)
	if !ok {
		return Denial{Scope: "", ResourceKind: ""}, false
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.denial == nil {
		return Denial{Scope: "", ResourceKind: ""}, false
	}
	return *recorder.denial, true
}

// recordDenial notes a refusal on the request's recorder. It is a no-op when
// no recorder was installed, so the engine behaves the same outside HTTP, and
// when err is not a denial. A refusal an explicit block produced is not
// recorded either: the caller may hold the scope already, so naming it would
// send them after a grant that cannot help.
func recordDenial(ctx context.Context, err error) {
	denied, ok := errors.AsType[*DeniedError](err)
	if !ok || denied.Blocked {
		return
	}
	recorder, ok := ctx.Value(denialRecorderKey{}).(*denialRecorder)
	if !ok {
		return
	}
	kind := denied.Selector[SelectorKeyResourceKind]
	if kind == "" {
		kind = ResourceKindForScope(denied.Scope)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.denial = &Denial{Scope: denied.Scope, ResourceKind: kind}
}

// Reason says why the caller is not authorized for a check.
type Reason string

const (
	// ReasonNone marks a check the caller is authorized for.
	ReasonNone Reason = ""

	// ReasonBlocked marks a check an exclusion (a *:blocked_* grant)
	// withdraws.
	ReasonBlocked Reason = "blocked"

	// ReasonReadOnly marks a check no grant allows only because of its
	// disposition dimension, where the caller's access covers read_only: a
	// grant would allow the same check if it named the read_only disposition.
	ReasonReadOnly Reason = "read_only"

	// ReasonDisposition marks a check no grant allows only because of
	// its disposition dimension, where the caller's access is limited to other
	// dispositions than read_only (for example idempotent or open_world): a
	// grant would allow the same check if it named one of those dispositions
	// instead.
	ReasonDisposition Reason = "disposition"

	// ReasonNotGranted marks a check no grant allows.
	ReasonNotGranted Reason = "not_granted"
)

// DenialReasons returns why each check is refused, aligned with the input
// order, without recording a challenge. It is the Reason of each of
// Decide's decisions, kept until its caller moves to Decide.
func (e *Engine) DenialReasons(ctx context.Context, checks []Check) ([]Reason, error) {
	d, err := e.decideAll(ctx, checks, false)
	if err != nil {
		return nil, err
	}
	reasons := make([]Reason, len(checks))
	for i, decision := range d.Each {
		reasons[i] = decision.Reason
	}
	return reasons, nil
}

// RecordedCheck rebuilds a check that was stored earlier, such as the denied
// request an access challenge keeps, from its scope, resource and dimensions.
// A connect check that names a tool or a disposition was a tool call, so it is
// rebuilt as one; anything else is rebuilt as an ordinary check. A recorded
// check the evaluator would refuse today, such as a view check that names a
// tool, stays invalid.
func RecordedCheck(scope Scope, resourceKind, resourceID string, dimensions map[string]string) Check {
	check := Check{Scope: scope, ResourceKind: resourceKind, ResourceID: resourceID, Dimensions: dimensions, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, exact: false}
	_, hasTool := dimensions[SelectorKeyTool]
	_, hasDisposition := dimensions[SelectorKeyDisposition]
	if (hasTool || hasDisposition) && isConnectScope(scope) {
		check.operation = operationToolCall
	}
	return check
}
