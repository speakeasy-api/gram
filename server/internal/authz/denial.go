package authz

import (
	"context"
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
// no recorder was installed, so the engine behaves the same outside HTTP.
func recordDenial(ctx context.Context, denied *DeniedError) {
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
