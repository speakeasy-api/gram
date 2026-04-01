package workos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

type WorkOS struct {
	logger *slog.Logger
	um     *usermanagement.Client
}

func New(logger *slog.Logger, apiKey string) *WorkOS {
	logger = logger.With(attr.SlogComponent("workos"))

	if apiKey == "" || apiKey == "unset" {
		return nil
	}

	return &WorkOS{
		logger: logger,
		um:     usermanagement.NewClient(apiKey),
	}
}

func (w *WorkOS) GetUserByEmail(ctx context.Context, email string) (*usermanagement.User, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	user, err := w.um.ListUsers(ctx, usermanagement.ListUsersOpts{
		Email:          email,
		Limit:          1,
		OrganizationID: "",
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list users from workos: %w", err)
	}

	if len(user.Data) == 0 {
		return nil, nil
	}

	return &user.Data[0], nil
}

func (w *WorkOS) GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*usermanagement.OrganizationMembership, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	membership, err := w.um.ListOrganizationMemberships(
		ctx,
		usermanagement.ListOrganizationMembershipsOpts{
			UserID:         workOSUserID,
			OrganizationID: workOSOrgID,
			Limit:          1,
			Statuses:       nil,
			Order:          "",
			Before:         "",
			After:          "",
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to list organization memberships from workos: %w", err)
	}

	if len(membership.Data) == 0 {
		return nil, nil
	}

	return &membership.Data[0], nil
}
