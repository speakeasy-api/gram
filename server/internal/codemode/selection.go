package codemode

import "context"

// SelectionGate reports whether an organization may select Code Mode: the
// runtime is available on this deployment and the organization has the Code
// Mode entitlement. It is independent of settings authorization.
type SelectionGate func(ctx context.Context, organizationID string) bool

// Allows fails closed when the new-selection gate has not been configured.
func (g SelectionGate) Allows(ctx context.Context, organizationID string) bool {
	return g != nil && organizationID != "" && g(ctx, organizationID)
}
