package chatanalysis

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// TriggerOrganization wakes the chat analysis coordinator for every live
// project in an organization. It continues through individual signal failures
// so one project cannot prevent the others from being woken.
func TriggerOrganization(
	ctx context.Context,
	db *pgxpool.Pool,
	signaler analysis.Signaler,
	organizationID string,
) (int, error) {
	projectIDs, err := repo.New(db).ListChatAnalysisProjectsForOrganization(ctx, organizationID)
	if err != nil {
		return 0, fmt.Errorf("list organization projects: %w", err)
	}

	var signalErrs []error
	for _, projectID := range projectIDs {
		if err := signaler.Signal(ctx, projectID); err != nil {
			signalErrs = append(signalErrs, err)
		}
	}
	if len(signalErrs) > 0 {
		return 0, fmt.Errorf(
			"signal chat analysis coordinator for %d of %d projects: %w",
			len(signalErrs), len(projectIDs), errors.Join(signalErrs...),
		)
	}

	return len(projectIDs), nil
}
