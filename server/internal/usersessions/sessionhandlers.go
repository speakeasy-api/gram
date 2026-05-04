package usersessions

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ListUserSessions is stubbed; ticket #5 implements it.
func (s *Service) ListUserSessions(ctx context.Context, payload *gen.ListUserSessionsPayload) (*gen.ListUserSessionsResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// RevokeUserSession is stubbed; ticket #5 implements it.
func (s *Service) RevokeUserSession(ctx context.Context, payload *gen.RevokeUserSessionPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
