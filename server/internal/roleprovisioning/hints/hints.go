// Package hints records transactional role-provisioning maintenance requests.
// Hints carry identifiers, never desired state or proof of completed maintenance.
package hints

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// Hint identifies an organization and optionally narrows maintenance to a role
// or plugin. Empty RoleURN and uuid.Nil PluginID mean organization-wide work.
// GlobalRoleURN or GlobalSweep requests bounded global fanout with no organization.
// Identifiers can refer to deleted resources; consumers must reload current state.
type Hint struct {
	OrganizationID      string
	RoleURN             string
	PluginID            uuid.UUID
	GlobalRoleURN       string
	GlobalSweep         bool
	AfterOrganizationID string
	AfterRoleURN        string
}

// Emit enqueues a hint in the caller's required transaction. It neither commits
// nor rolls back; success is provisional until the caller commits. It does not
// consult provisioning/publication enablement or require a GitHub connection.
func Emit(ctx context.Context, tx pgx.Tx, hint Hint) error {
	if tx == nil {
		return fmt.Errorf("role provisioning hint requires a transaction")
	}
	if hint.OrganizationID == "" && hint.GlobalRoleURN == "" && !hint.GlobalSweep {
		return fmt.Errorf("role provisioning hint requires an organization")
	}
	global := hint.GlobalRoleURN != "" || hint.GlobalSweep
	if global && (hint.OrganizationID != "" || hint.RoleURN != "" || hint.PluginID != uuid.Nil || hint.AfterRoleURN != "") {
		return fmt.Errorf("global hint cannot have organization scope")
	}
	if !global && hint.AfterOrganizationID != "" {
		return fmt.Errorf("organization hint cannot have global cursor")
	}
	if hint.GlobalRoleURN != "" {
		id, err := uuid.Parse(strings.TrimPrefix(hint.GlobalRoleURN, "role:global:"))
		if err != nil || id == uuid.Nil || hint.GlobalRoleURN != "role:global:"+id.String() {
			return fmt.Errorf("invalid global role hint")
		}
	}
	event := pluginsv1.RoleProvisioningRequested_builder{OrganizationId: new(hint.OrganizationID)}.Build()
	if hint.GlobalRoleURN != "" {
		event.SetGlobalRoleUrn(hint.GlobalRoleURN)
	}
	if hint.GlobalSweep {
		event.SetGlobalSweep(true)
	}
	if hint.AfterOrganizationID != "" {
		event.SetAfterOrganizationId(hint.AfterOrganizationID)
	}
	if hint.AfterRoleURN != "" {
		event.SetAfterRoleUrn(hint.AfterRoleURN)
	}
	if hint.RoleURN != "" {
		event.SetRoleUrn(hint.RoleURN)
	}
	if hint.PluginID != uuid.Nil {
		event.SetPluginId(hint.PluginID.String())
	}
	if _, err := outbox.Publish(ctx, tx, hint.OrganizationID, outbox.Message{PublicID: uuid.Nil, Attributes: nil, Proto: event}); err != nil {
		return fmt.Errorf("enqueue role provisioning hint: %w", err)
	}
	return nil
}
