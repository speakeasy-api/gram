package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// WorkOSAdapter wraps the WorkOS usermanagement.Client to implement both
// IDPClient (for identity) and sessions.SessionRevoker (for sessions),
// translating between Speakeasy's slim interfaces and the SDK's richer types.
type WorkOSAdapter struct {
	client            *usermanagement.Client
	googleUserInfoURL string
	evidence          evidenceCollector
}

// NewWorkOSAdapter creates an adapter from a WorkOS SDK client.
// Returns nil when client is nil (e.g. OSS / test environments).
func NewWorkOSAdapter(client *usermanagement.Client) *WorkOSAdapter {
	if client == nil {
		return nil
	}
	return &WorkOSAdapter{client: client, googleUserInfoURL: DefaultGoogleUserInfoURL, evidence: newEvidenceCollector(EvidenceConfig{Logger: nil, MeterProvider: nil, Enabled: nil})}
}

// WithEvidence enables sign-in evidence collection. Without it only the
// verified email is recorded and no sign-in qualifies for vouching.
func (a *WorkOSAdapter) WithEvidence(cfg EvidenceConfig) *WorkOSAdapter {
	if a != nil {
		a.evidence = newEvidenceCollector(cfg)
	}
	return a
}

// WithGoogleUserInfoURL points Google hosted-domain lookups elsewhere, e.g. at
// the dev-idp locally. Empty keeps Google's endpoint.
func (a *WorkOSAdapter) WithGoogleUserInfoURL(url string) *WorkOSAdapter {
	if a != nil && url != "" {
		a.googleUserInfoURL = url
	}
	return a
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

	impersonatorEmail := ""
	if resp.Impersonator != nil {
		impersonatorEmail = resp.Impersonator.Email
	}

	return &AuthenticateResult{
		AccessToken:          resp.AccessToken,
		OrganizationID:       resp.OrganizationID,
		AuthenticationMethod: string(resp.AuthenticationMethod),
		Evidence:             a.signInEvidence(ctx, resp),
		User: AuthenticatedUser{
			ID:                resp.User.ID,
			FirstName:         resp.User.FirstName,
			LastName:          resp.User.LastName,
			Email:             resp.User.Email,
			EmailVerified:     resp.User.EmailVerified,
			ProfilePictureURL: resp.User.ProfilePictureURL,
			ExternalID:        resp.User.ExternalID,
		},
		impersonatorEmail: impersonatorEmail,
	}, nil
}

func (a *WorkOSAdapter) CreateMagicAuth(ctx context.Context, email string) (*MagicAuthChallenge, error) {
	challenge, err := a.client.CreateMagicAuth(ctx, usermanagement.CreateMagicAuthOpts{
		Email:           email,
		InvitationToken: "",
	})
	if err != nil {
		return nil, fmt.Errorf("workos create magic auth: %w", err)
	}

	return &MagicAuthChallenge{
		Email: challenge.Email,
		Code:  challenge.Code,
	}, nil
}

func (a *WorkOSAdapter) AuthenticateWithMagicAuth(ctx context.Context, clientID, email, code string) (*AuthenticateResult, error) {
	resp, err := a.client.AuthenticateWithMagicAuth(ctx, usermanagement.AuthenticateWithMagicAuthOpts{
		ClientID:              clientID,
		Email:                 email,
		Code:                  code,
		LinkAuthorizationCode: "",
		IPAddress:             "",
		UserAgent:             "",
	})
	if err != nil {
		return nil, fmt.Errorf("workos authenticate with magic auth: %w", err)
	}

	return &AuthenticateResult{
		AccessToken:          resp.AccessToken,
		OrganizationID:       resp.OrganizationID,
		AuthenticationMethod: string(resp.AuthenticationMethod),
		// The server redeems this code itself, so nothing here was proven by
		// the user and the sign-in never qualifies.
		Evidence: SignInEvidence{HostedDomain: "", TenantID: "", VerifiedEmail: "", AuthenticatedAt: time.Time{}},
		User: AuthenticatedUser{
			ID:                resp.User.ID,
			FirstName:         resp.User.FirstName,
			LastName:          resp.User.LastName,
			Email:             resp.User.Email,
			EmailVerified:     resp.User.EmailVerified,
			ProfilePictureURL: resp.User.ProfilePictureURL,
			ExternalID:        resp.User.ExternalID,
		},
		impersonatorEmail: "",
	}, nil
}

func (a *WorkOSAdapter) SetEmailVerified(ctx context.Context, userID string) error {
	var opts usermanagement.UpdateUserOpts
	opts.User = userID
	opts.EmailVerified = true

	if _, err := a.client.UpdateUser(ctx, opts); err != nil {
		return fmt.Errorf("workos set email verified: %w", err)
	}
	return nil
}

func (a *WorkOSAdapter) RevokeSession(ctx context.Context, sessionID string) error {
	if err := a.client.RevokeSession(ctx, usermanagement.RevokeSessionOpts{
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("workos revoke session: %w", err)
	}
	return nil
}

// maxSessionPages caps the WorkOS session listing for one user.
const maxSessionPages = 50

// ListSessionIDs lists every WorkOS session the user holds, following
// pagination.
func (a *WorkOSAdapter) ListSessionIDs(ctx context.Context, workosUserID string) ([]string, error) {
	if a == nil {
		return nil, nil
	}
	var ids []string
	after := ""
	for range maxSessionPages {
		resp, err := a.client.ListSessions(ctx, workosUserID, usermanagement.ListSessionsOpts{
			Limit:  100,
			Before: "",
			After:  after,
			Order:  usermanagement.Desc,
		})
		if err != nil {
			return nil, fmt.Errorf("workos list sessions: %w", err)
		}
		for _, session := range resp.Data {
			ids = append(ids, session.ID)
		}
		if resp.ListMetadata.After == "" || len(resp.Data) == 0 {
			return ids, nil
		}
		after = resp.ListMetadata.After
	}
	return nil, fmt.Errorf("workos list sessions: more than %d pages", maxSessionPages)
}
