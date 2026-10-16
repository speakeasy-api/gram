package channels

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// AdminAuditChange is one boolean permission transition.
type AdminAuditChange struct {
	Before bool
	After  bool
}

// AdminAuditEvent describes an administrative Channels change. It carries
// identifiers and permission transitions only: message bodies, titles, notes
// and instance labels are never copied into an audit record.
type AdminAuditEvent struct {
	OrganizationID string
	ActorSubjectID string
	// TargetSubjectID is empty for an organization enablement change.
	TargetSubjectID string
	EnabledBefore   *bool
	EnabledAfter    *bool
	CanConnect      *AdminAuditChange
	CanReadAll      *AdminAuditChange
	// Removed reports that the target's explicit exception was withdrawn, so
	// the organization's native permissions apply again.
	Removed bool
}

// AdminAudit records an administrative change. It runs inside the same
// transaction as the change itself and before commit, so a committed
// permission change can never exist without its audit record. Returning an
// error aborts the change.
type AdminAudit func(ctx context.Context, tx pgx.Tx, event AdminAuditEvent) error

// WithAdminAudit installs the administrative audit hook. A handler without one
// behaves exactly as before.
func WithAdminAudit(record AdminAudit) Option {
	return func(h *Handler) { h.adminAudit = record }
}
