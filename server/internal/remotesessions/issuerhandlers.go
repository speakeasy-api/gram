package remotesessions

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// DiscoverRemoteSessionIssuer is stubbed; ticket #9 implements it.
func (s *Service) DiscoverRemoteSessionIssuer(ctx context.Context, payload *gen.DiscoverRemoteSessionIssuerPayload) (*types.RemoteSessionIssuerDraft, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// CreateRemoteSessionIssuer is stubbed; ticket #9 implements it.
func (s *Service) CreateRemoteSessionIssuer(ctx context.Context, payload *gen.CreateRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// UpdateRemoteSessionIssuer is stubbed; ticket #9 implements it.
func (s *Service) UpdateRemoteSessionIssuer(ctx context.Context, payload *gen.UpdateRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// ListRemoteSessionIssuers is stubbed; ticket #9 implements it.
func (s *Service) ListRemoteSessionIssuers(ctx context.Context, payload *gen.ListRemoteSessionIssuersPayload) (*gen.ListRemoteSessionIssuersResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// GetRemoteSessionIssuer is stubbed; ticket #9 implements it.
func (s *Service) GetRemoteSessionIssuer(ctx context.Context, payload *gen.GetRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// DeleteRemoteSessionIssuer is stubbed; ticket #9 implements it.
func (s *Service) DeleteRemoteSessionIssuer(ctx context.Context, payload *gen.DeleteRemoteSessionIssuerPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
