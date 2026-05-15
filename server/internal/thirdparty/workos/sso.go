package workos

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/sso"
)

// InviteLinkProfile contains the authenticated user's identity from a
// passwordless magic-link code exchange.
type InviteLinkProfile struct {
	Email     string
	FirstName string
	LastName  string
}

// AuthenticateWithInviteLink exchanges a WorkOS passwordless authorization
// code for the authenticated user's profile.
//
// We use the SSO client (sso.GetProfileAndToken) rather than
// usermanagement.AuthenticateWithCode because the User Management endpoint
// rejects passwordless connection types with "invalid_connection". The SSO
// /sso/token endpoint handles passwordless codes correctly — this matches
// the speakeasy-registry's authInviteCallback implementation.
func (wc *Client) AuthenticateWithInviteLink(ctx context.Context, code string) (*InviteLinkProfile, error) {
	ssoClient := &sso.Client{
		APIKey:     wc.apiKey,
		ClientID:   wc.clientID,
		HTTPClient: wc.httpClient,
		Endpoint:   wc.endpoint,
		JSONEncode: nil,
	}

	resp, err := ssoClient.GetProfileAndToken(ctx, sso.GetProfileAndTokenOpts{
		Code: code,
	})
	if err != nil {
		return nil, fmt.Errorf("authenticate with invite link: %w", err)
	}

	return &InviteLinkProfile{
		Email:     resp.Profile.Email,
		FirstName: resp.Profile.FirstName,
		LastName:  resp.Profile.LastName,
	}, nil
}
