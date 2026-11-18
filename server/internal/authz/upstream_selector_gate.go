package authz

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ErrUpstreamSelectorWritesDisabled refuses a write of a selector naming
// upstream_client for an organization the write gate has not enabled.
var ErrUpstreamSelectorWritesDisabled = errors.New("upstream_client selectors are not enabled for this organization")

// UpstreamSelectorWriteGate reports whether an organization may write grants
// whose selector names upstream_client. Replicas that predate the dimension
// drop such restrictions and their policy edits do not protect connection
// owner blocks, so writes stay off until every replica reads them.
type UpstreamSelectorWriteGate func(ctx context.Context, organizationID string) (bool, error)

var upstreamSelectorWriteGate atomic.Pointer[UpstreamSelectorWriteGate]

type upstreamSelectorWriteGateKey struct{}

// SetUpstreamSelectorWriteGate installs the process-wide gate. Without one,
// every write naming upstream_client is refused.
func SetUpstreamSelectorWriteGate(gate UpstreamSelectorWriteGate) {
	if gate == nil {
		upstreamSelectorWriteGate.Store(nil)
		return
	}
	upstreamSelectorWriteGate.Store(&gate)
}

// WithUpstreamSelectorWriteGate overrides the process-wide gate for writes
// made with ctx.
func WithUpstreamSelectorWriteGate(ctx context.Context, gate UpstreamSelectorWriteGate) context.Context {
	return context.WithValue(ctx, upstreamSelectorWriteGateKey{}, gate)
}

// UpstreamSelectorWritesAllowed reports whether organizationID may write
// selectors naming upstream_client. A missing gate, a missing organization,
// an evaluation error or a disabled flag all refuse.
func UpstreamSelectorWritesAllowed(ctx context.Context, organizationID string) bool {
	if organizationID == "" {
		return false
	}
	gate, ok := ctx.Value(upstreamSelectorWriteGateKey{}).(UpstreamSelectorWriteGate)
	if !ok {
		if stored := upstreamSelectorWriteGate.Load(); stored != nil {
			gate = *stored
		}
	}
	if gate == nil {
		return false
	}
	allowed, err := gate(ctx, organizationID)
	return err == nil && allowed
}

// NamesUpstreamClient reports whether a selector carries the upstream_client
// dimension.
func NamesUpstreamClient(selector Selector) bool {
	_, ok := selector[SelectorKeyUpstreamClient]
	return ok
}

// RequireUpstreamSelectorWrites refuses, before anything is written, a write
// of any selector naming upstream_client unless the organization's gate
// allows it. Selectors without the dimension are never affected, and
// removals are not writes.
func RequireUpstreamSelectorWrites(ctx context.Context, organizationID string, selectors ...Selector) error {
	for _, selector := range selectors {
		if !NamesUpstreamClient(selector) {
			continue
		}
		if UpstreamSelectorWritesAllowed(ctx, organizationID) {
			return nil
		}
		return oops.E(oops.CodeForbidden, ErrUpstreamSelectorWritesDisabled, "Grants naming upstream_client are not enabled for this organization yet.")
	}
	return nil
}

// RequireUpstreamSelectorWritesRaw is RequireUpstreamSelectorWrites for a
// stored selector.
func RequireUpstreamSelectorWritesRaw(ctx context.Context, organizationID string, raw []byte) error {
	selector, err := SelectorFromRow(raw)
	if err != nil {
		return fmt.Errorf("decode selector: %w", err)
	}
	return RequireUpstreamSelectorWrites(ctx, organizationID, selector)
}

// RequireUpstreamSelectorWritesForGrants checks every selector of grants.
func RequireUpstreamSelectorWritesForGrants(ctx context.Context, organizationID string, grants []*RoleGrant) error {
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		if err := RequireUpstreamSelectorWrites(ctx, organizationID, grant.Selectors...); err != nil {
			return err
		}
	}
	return nil
}
