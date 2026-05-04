package usersessions

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// CreateUserSessionIssuer is stubbed; ticket #4 implements it.
func (s *Service) CreateUserSessionIssuer(ctx context.Context, payload *gen.CreateUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// UpdateUserSessionIssuer is stubbed; ticket #4 implements it.
func (s *Service) UpdateUserSessionIssuer(ctx context.Context, payload *gen.UpdateUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// ListUserSessionIssuers is stubbed; ticket #4 implements it.
func (s *Service) ListUserSessionIssuers(ctx context.Context, payload *gen.ListUserSessionIssuersPayload) (*gen.ListUserSessionIssuersResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// GetUserSessionIssuer is stubbed; ticket #4 implements it.
func (s *Service) GetUserSessionIssuer(ctx context.Context, payload *gen.GetUserSessionIssuerPayload) (*types.UserSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// DeleteUserSessionIssuer is stubbed; ticket #4 implements it.
func (s *Service) DeleteUserSessionIssuer(ctx context.Context, payload *gen.DeleteUserSessionIssuerPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
