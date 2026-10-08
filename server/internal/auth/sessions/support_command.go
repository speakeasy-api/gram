package sessions

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// AuthenticateSupportCommand authenticates an existing support session for one
// exact organization without refreshing it or resolving external identity data.
// Only the session cache and user repository are needed by this path.
func (s *Manager) AuthenticateSupportCommand(ctx context.Context, token, organizationID string, now time.Time) (context.Context, error) {
	if token == "" || organizationID == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	session, err := s.sessionCache.Get(ctx, SessionCacheKey(token))
	if err != nil {
		return ctx, fmt.Errorf("read support command session: %w", err)
	}
	user, err := s.userRepo.GetUser(ctx, session.UserID)
	if err != nil {
		return ctx, fmt.Errorf("read support command operator: %w", err)
	}
	if !validSupportSession(session, isCurrentPlatformAdmin(user.Admin, user.DeletedAt.Valid), now) || session.ActiveOrganizationID != organizationID {
		return ctx, oops.C(oops.CodeForbidden)
	}
	return contextvalues.WithValidatedSupportSession(ctx, &contextvalues.AuthContext{
		SessionID: &session.SessionID, ActiveOrganizationID: organizationID,
		UserID: user.ID, Email: &user.Email, IsAdmin: true,
		SupportOrganizationID: organizationID,
		ExternalUserID:        "", APIKeyID: "", APIKeyName: "", OrgWidePluginHooksKey: false,
		ProjectID: nil, OrganizationSlug: "", AccountType: "", HasActiveSubscription: false,
		Whitelisted: false, ProjectSlug: nil, APIKeyScopes: nil,
	}), nil
}
