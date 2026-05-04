package usersessions

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ListUserSessionConsents is stubbed; ticket #7 implements it.
func (s *Service) ListUserSessionConsents(ctx context.Context, payload *gen.ListUserSessionConsentsPayload) (*gen.ListUserSessionConsentsResult, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}

// RevokeUserSessionConsent is stubbed; ticket #7 implements it.
func (s *Service) RevokeUserSessionConsent(ctx context.Context, payload *gen.RevokeUserSessionConsentPayload) error {
	return oops.E(oops.CodeNotImplemented, nil, "not implemented").Log(ctx, s.logger)
}
