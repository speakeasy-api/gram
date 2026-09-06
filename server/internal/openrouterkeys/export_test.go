package openrouterkeys

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func WithAdminMutationCommitForTest(commit func(context.Context, pgx.Tx) error) ServiceOption {
	return func(service *Service) {
		service.commitAdminMutation = commit
	}
}
