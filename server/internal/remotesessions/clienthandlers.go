package remotesessions

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) CreateRemoteSessionClient(ctx context.Context, payload *gen.CreateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

func (s *Service) UpdateRemoteSessionClient(ctx context.Context, payload *gen.UpdateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

func (s *Service) ListRemoteSessionClients(ctx context.Context, payload *gen.ListRemoteSessionClientsPayload) (*gen.ListRemoteSessionClientsResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

func (s *Service) GetRemoteSessionClient(ctx context.Context, payload *gen.GetRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

func (s *Service) DeleteRemoteSessionClient(ctx context.Context, payload *gen.DeleteRemoteSessionClientPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
