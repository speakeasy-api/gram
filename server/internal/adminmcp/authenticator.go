package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The staff issuer, signing key, and resource audience must be configured
// independently of customer Platform MCP before this authenticator is mounted.
type StaffAuthenticator struct {
	signer   *sessiontokens.Signer
	store    staffAccessStore
	cipher   *encryption.Client
	verifier adminSessionVerifier
	issuer   string
	audience string
}

type adminSessionVerifier interface {
	Authorize(context.Context, string, *security.APIKeyScheme) (context.Context, error)
}

type staffAccessStore interface {
	ActiveSession(context.Context, string) (staffAccessSession, error)
}

type staffAccessSession struct {
	Subject          string
	ClientID         string
	ConnectionID     string
	Generation       string
	ActiveGeneration string
	ResourceURI      string
	Scopes           []string
	AdminSessionEnc  string
	ExpiresAt        time.Time
}

func NewStaffAuthenticator(signer *sessiontokens.Signer, db *pgxpool.Pool, cipher *encryption.Client, verifier adminSessionVerifier, issuer, audience string) *StaffAuthenticator {
	var store staffAccessStore
	if db != nil {
		store = postgresStaffAccessStore{db: db}
	}
	return &StaffAuthenticator{signer: signer, store: store, cipher: cipher, verifier: verifier, issuer: issuer, audience: audience}
}

func (a *StaffAuthenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if a == nil || a.signer == nil || a.store == nil || a.cipher == nil || a.verifier == nil || a.issuer == "" || a.audience == "" {
		return Principal{}, ErrAuthUnavailable
	}
	claims, err := a.signer.ValidateExactAudience(token, a.audience)
	if err != nil || claims.Issuer != a.issuer || claims.ID == "" || claims.ClientID == "" {
		return Principal{}, errors.New("invalid staff token")
	}
	subject, err := urn.ParseSessionSubject(claims.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return Principal{}, errors.New("invalid staff subject")
	}

	session, err := a.store.ActiveSession(ctx, claims.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, errors.New("staff token is not active")
	}
	if err != nil {
		return Principal{}, ErrAuthUnavailable
	}
	if session.Subject != claims.Subject || session.ClientID != claims.ClientID || session.ConnectionID == "" || session.Generation == "" || session.Generation != session.ActiveGeneration || session.ResourceURI != a.audience || session.AdminSessionEnc == "" || !time.Now().Before(session.ExpiresAt) || !hasReadScope(session.Scopes) {
		return Principal{}, errors.New("staff connection is not active")
	}
	browserSessionID, err := a.cipher.Decrypt(session.AdminSessionEnc)
	if err != nil || browserSessionID == "" {
		return Principal{}, errors.New("staff session reference is invalid")
	}
	// Always supply the explicit, internally decrypted key. Authorize's cookie
	// fallback is only reachable for empty keys, which are rejected above.
	verifiedCtx, err := a.verifier.Authorize(ctx, browserSessionID, &security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}) //nolint:exhaustruct // Only the scheme name is checked by the admin verifier.
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
			return Principal{}, errors.New("staff session is no longer authorized")
		}
		return Principal{}, ErrAuthUnavailable
	}
	staff, ok := contextvalues.GetAdminAuthContext(verifiedCtx)
	if !ok || staff == nil || staff.SessionID != browserSessionID || staff.OIDCSubject != subject.ID || staff.Email == "" {
		return Principal{}, errors.New("staff identity does not match connection")
	}
	return Principal{Subject: claims.Subject, Email: staff.Email, ClientID: claims.ClientID, ConnectionID: session.ConnectionID, Scopes: session.Scopes}, nil
}

type postgresStaffAccessStore struct{ db *pgxpool.Pool }

func (s postgresStaffAccessStore) ActiveSession(ctx context.Context, jti string) (staffAccessSession, error) {
	var result staffAccessSession
	var connectionID, generation, activeGeneration string
	err := s.db.QueryRow(ctx, `
SELECT connection.subject_urn, client.client_id, connection.id::text,
       session.connection_generation::text, connection.active_generation::text,
       connection.resource_uri, connection.scopes, connection.admin_session_id_enc, session.expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = connection.oauth_client_id
WHERE session.jti = $1
  AND session.revoked_at IS NULL AND session.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL AND connection.reauthorization_required_at IS NULL
  AND connection.authorization_expires_at > clock_timestamp()
  AND client.revoked_at IS NULL
`, jti).Scan(&result.Subject, &result.ClientID, &connectionID, &generation, &activeGeneration, &result.ResourceURI, &result.Scopes, &result.AdminSessionEnc, &result.ExpiresAt)
	result.ConnectionID = connectionID
	result.Generation = generation
	result.ActiveGeneration = activeGeneration
	if err != nil {
		return staffAccessSession{}, fmt.Errorf("lookup active staff MCP session: %w", err)
	}
	return result, nil
}
