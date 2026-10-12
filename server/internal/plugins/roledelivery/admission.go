package roledelivery

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func checkAttachment(ctx context.Context, tx pgx.Tx, guard *admission.Guard, org string, projectID, pluginID, serverID uuid.UUID) error {
	if err := guard.CheckAttachment(ctx, tx, org, projectID, pluginID, serverID); err != nil {
		return fmt.Errorf("check role delivery attachment admission: %w", err)
	}
	return nil
}
