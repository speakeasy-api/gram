package usersessions

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ListUserSessionClients is stubbed; ticket #6 implements it.
func (s *Service) ListUserSessionClients(ctx context.Context, payload *gen.ListUserSessionClientsPayload) (*gen.ListUserSessionClientsResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// GetUserSessionClient is stubbed; ticket #6 implements it.
func (s *Service) GetUserSessionClient(ctx context.Context, payload *gen.GetUserSessionClientPayload) (*types.UserSessionClient, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// RevokeUserSessionClient is stubbed; ticket #6 implements it.
func (s *Service) RevokeUserSessionClient(ctx context.Context, payload *gen.RevokeUserSessionClientPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
