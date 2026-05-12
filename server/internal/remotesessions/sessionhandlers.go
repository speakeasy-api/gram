package remotesessions

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ListRemoteSessions is stubbed; ticket #11 implements it.
func (s *Service) ListRemoteSessions(ctx context.Context, payload *gen.ListRemoteSessionsPayload) (*gen.ListRemoteSessionsResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// RevokeRemoteSession is stubbed; ticket #11 implements it.
func (s *Service) RevokeRemoteSession(ctx context.Context, payload *gen.RevokeRemoteSessionPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
