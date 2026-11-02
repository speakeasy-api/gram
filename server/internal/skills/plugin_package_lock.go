package skills

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// lockPluginPackageContent is the last resource lock of a skill mutation.
// The shared lock serializes its commit with the exclusive check that turns
// auto sync off, whether or not a publisher is configured. Keep all skill,
// plugin-row and admission locks before this call.
func lockPluginPackageContent(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if err := pluginsrepo.New(tx).LockPluginPackageContentShared(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock plugin package content after skill change: %w", err)
	}
	return nil
}
