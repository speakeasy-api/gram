// Package hints records transactional role-provisioning maintenance requests.
// Hints carry identifiers, never desired state or proof of completed maintenance.
package hints

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// Hint identifies an organization and optionally narrows maintenance to a role
// or plugin. Empty RoleURN and uuid.Nil PluginID mean organization-wide work.
// Identifiers can refer to deleted resources; consumers must reload current state.
type Hint struct {
	OrganizationID string
	RoleURN        string
	PluginID       uuid.UUID
}

// Emit enqueues a hint in the caller's required transaction. It neither commits
// nor rolls back; success is provisional until the caller commits. It does not
// consult provisioning/publication enablement or require a GitHub connection.
func Emit(ctx context.Context, tx pgx.Tx, hint Hint) error {
	if tx == nil {
		return fmt.Errorf("role provisioning hint requires a transaction")
	}
	if hint.OrganizationID == "" {
		return fmt.Errorf("role provisioning hint requires an organization")
	}
	event := pluginsv1.RoleProvisioningRequested_builder{OrgId: new(hint.OrganizationID)}.Build()
	if hint.RoleURN != "" {
		event.SetRoleUrn(hint.RoleURN)
	}
	if hint.PluginID != uuid.Nil {
		event.SetPluginId(hint.PluginID.String())
	}
	if _, err := outbox.Publish(ctx, tx, hint.OrganizationID, outbox.Message{Proto: event}); err != nil {
		return fmt.Errorf("enqueue role provisioning hint: %w", err)
	}
	return nil
}
