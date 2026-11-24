package authz

import (
	"context"
	"errors"
	"maps"
	"slices"
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

// DenialReason says why the caller is not authorized for a check.
type DenialReason string

const (
	// DenialReasonNone marks a check the caller is authorized for.
	DenialReasonNone DenialReason = ""

	// DenialReasonBlocked marks a check an exclusion (a *:blocked_* grant)
	// withdraws.
	DenialReasonBlocked DenialReason = "blocked"

	// DenialReasonReadOnly marks a check no grant allows only because of its
	// disposition dimension, where the caller's access covers read_only: a
	// grant would allow the same check if it named the read_only disposition.
	DenialReasonReadOnly DenialReason = "read_only"

	// DenialReasonDisposition marks a check no grant allows only because of
	// its disposition dimension, where the caller's access is limited to other
	// dispositions than read_only (for example idempotent or open_world): a
	// grant would allow the same check if it named one of those dispositions
	// instead.
	DenialReasonDisposition DenialReason = "disposition"

	// DenialReasonNotGranted marks a check no grant allows.
	DenialReasonNotGranted DenialReason = "not_granted"
)

// DenialReasons evaluates each check against the prepared grants and returns
// why each is refused, aligned with the input order. It records no authz
// challenge, because it only labels decisions that [Engine.FindMatched] or
// [Engine.Require] already make, and it reads only the grants already on the
// context.
//
// When RBAC is not enforced every check is authorized except auth-client
// checks, which are not granted, and checks are not validated, matching
// [Engine.FindMatched] and [Engine.Require].
func (e *Engine) DenialReasons(ctx context.Context, checks []Check) ([]DenialReason, error) {
	reasons := make([]DenialReason, len(checks))
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		for i, c := range checks {
			if !unenforcedAllows(c) {
				reasons[i] = DenialReasonNotGranted
			}
		}
		return reasons, nil
	}

	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}

	for i, c := range checks {
		if err := validateInput(c); err != nil {
			return nil, e.mapError(ctx, err)
		}
		evaluation, err := authorization.evaluate(c)
		if err != nil {
			return nil, e.mapError(ctx, err)
		}
		switch {
		case evaluation.Grant != nil:
			reasons[i] = DenialReasonNone
		case evaluation.Denied:
			reasons[i] = DenialReasonBlocked
		default:
			reasons[i] = DenialReasonNotGranted
		}
		if reasons[i] != DenialReasonNotGranted {
			continue
		}

		disposition, hasDisposition := c.Dimensions[SelectorKeyDisposition]
		if !hasDisposition {
			continue
		}

		if disposition != DispositionReadOnly {
			asReadOnly := c
			asReadOnly.Dimensions = maps.Clone(c.Dimensions)
			asReadOnly.Dimensions[SelectorKeyDisposition] = DispositionReadOnly
			evaluation, err = authorization.evaluate(asReadOnly)
			if err != nil {
				return nil, e.mapError(ctx, err)
			}
			if evaluation.Grant != nil {
				reasons[i] = DenialReasonReadOnly
				continue
			}
		}

		// Refused only for its disposition when some other disposition, with
		// every other value unchanged, would be allowed.
		for _, other := range slices.Sorted(maps.Keys(validDispositions)) {
			if other == disposition || other == DispositionReadOnly {
				continue
			}
			asOther := c
			asOther.Dimensions = maps.Clone(c.Dimensions)
			asOther.Dimensions[SelectorKeyDisposition] = other
			evaluation, err = authorization.evaluate(asOther)
			if err != nil {
				return nil, e.mapError(ctx, err)
			}
			if evaluation.Grant != nil {
				reasons[i] = DenialReasonDisposition
				break
			}
		}
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
