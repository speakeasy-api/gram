package gramotel

import (
	"context"
	"errors"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type resultKey struct{}

type recordIDKey struct{}

type tenantKey struct{}

// Result is where Emit leaves its publish error, since the logs API's Emit
// returns nothing. It is safe to share across goroutines; when several
// records are emitted under one Result, their errors are joined.
type Result struct {
	mu  sync.Mutex
	err error
}

// WithResult returns a context carrying a fresh Result. Pass the context to
// Emit and read Err afterwards to learn whether the record reached the topic.
func WithResult(ctx context.Context) (context.Context, *Result) {
	result := &Result{mu: sync.Mutex{}, err: nil}
	return context.WithValue(ctx, resultKey{}, result), result
}

// Err is the publish error of every Emit made under this result, or nil when
// each of them reached the topic.
func (r *Result) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Result) add(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = errors.Join(r.err, err)
}

func resultFrom(ctx context.Context) *Result {
	result, _ := ctx.Value(resultKey{}).(*Result)
	return result
}

// WithRecordID sets the record id the next Emit under this context is
// published with. Without it, the id is derived from the record's content,
// so emitting the same record twice yields the same id.
func WithRecordID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, recordIDKey{}, id)
}

func recordIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(recordIDKey{}).(string)
	return id
}

// tenant is the organization and project a record belongs to.
type tenant struct {
	organizationID string
	projectID      string
}

// WithTenant sets the organization and project for code that runs without an
// authenticated request, such as a background job. Tenancy always comes from
// Gram's own code, never from a record's attributes.
func WithTenant(ctx context.Context, organizationID, projectID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant{organizationID: organizationID, projectID: projectID})
}

// tenantFrom resolves tenancy the way the ingest edge does: from
// WithTenant when set, otherwise from the authenticated request.
func tenantFrom(ctx context.Context) (tenant, bool) {
	if t, ok := ctx.Value(tenantKey{}).(tenant); ok && t.organizationID != "" && t.projectID != "" {
		return t, true
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ProjectID == nil {
		return tenant{organizationID: "", projectID: ""}, false
	}
	return tenant{organizationID: authCtx.ActiveOrganizationID, projectID: authCtx.ProjectID.String()}, true
}
