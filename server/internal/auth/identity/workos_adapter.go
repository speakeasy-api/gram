package identity

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// WorkOSAdapter wraps the WorkOS usermanagement.Client to implement both
// IDPClient (for identity) and sessions.SessionRevoker (for sessions),
// translating between Gram's slim interfaces and the SDK's richer types.
type WorkOSAdapter struct {
	client *usermanagement.Client
}

// NewWorkOSAdapter creates an adapter from a WorkOS SDK client.
// Returns nil when client is nil (e.g. OSS / test environments).
func NewWorkOSAdapter(client *usermanagement.Client) *WorkOSAdapter {
	if client == nil {
		return nil
	}
	return &WorkOSAdapter{client: client}
}

func (a *WorkOSAdapter) AuthenticateWithCode(ctx context.Context, clientID, code string) (*AuthenticateResult, error) {
	resp, err := a.client.AuthenticateWithCode(ctx, usermanagement.AuthenticateWithCodeOpts{
		ClientID:     clientID,
		Code:         code,
		CodeVerifier: "",
		IPAddress:    "",
		UserAgent:    "",
	})
	if err != nil {
		return nil, fmt.Errorf("workos authenticate: %w", err)
	}

	return &AuthenticateResult{
		AccessToken:    resp.AccessToken,
		OrganizationID: resp.OrganizationID,
		User: AuthenticatedUser{
			ID:                resp.User.ID,
			FirstName:         resp.User.FirstName,
			LastName:          resp.User.LastName,
			Email:             resp.User.Email,
			ProfilePictureURL: resp.User.ProfilePictureURL,
			ExternalID:        resp.User.ExternalID,
		},
	}, nil
}

func (a *WorkOSAdapter) RevokeSession(ctx context.Context, sessionID string) error {
	if err := a.client.RevokeSession(ctx, usermanagement.RevokeSessionOpts{
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("workos revoke session: %w", err)
	}
	return nil
}
