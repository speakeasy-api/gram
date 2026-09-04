// A live grant made before the issuer advertised openid carries no identity.
// The consent page tells the subject a reconnect would add one, and only then:
// a grant that already has openid, or an issuer that will not request it, gets
// no such prompt.

package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const consentIdentityCopy = "Reconnect to enable identity"

// grantScoped writes a live session for the fixture subject on clientID that
// carries the given scopes.
func grantScoped(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID, scopes []string) {
	t.Helper()
	encrypted, err := fx.ti.enc.Encrypt([]byte("token-" + clientID.String()))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             fx.subject,
		UserSessionIssuerID:    fx.shared,
		RemoteSessionClientID:  clientID,
		AccessTokenEncrypted:   encrypted,
		AccessExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshTokenEncrypted:  pgtype.Text{String: "", Valid: false},
		AuthorizationExpiresAt: pgtype.Timestamptz{Valid: false},
		RefreshExpiresAt:       pgtype.Timestamptz{Valid: false},
		Scopes:                 scopes,
		Resource:               conv.ToPGTextEmpty(""),
		AutoRefresh:            true,
	})
	require.NoError(t, err)
}

// advertise sets the issuer's scopes_supported.
func advertise(t *testing.T, ctx context.Context, fx consentActionFixture, issuerID uuid.UUID, scopes []string) {
	t.Helper()
	_, err := remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionIssuer(ctx, remotesessions_repo.UpdateRemoteSessionIssuerParams{
		ScopesSupported: scopes,
		ID:              issuerID,
		ProjectID:       conv.ToNullUUID(fx.projectID),
	})
	require.NoError(t, err)
}

func expectIdentityHint(t *testing.T, fx consentActionFixture, shown bool) {
	t.Helper()
	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 1 connected", "the grant stays connected either way")
	if shown {
		require.Contains(t, html, consentIdentityCopy)
		require.Contains(t, html, "> Reconnect </button>")
	} else {
		require.NotContains(t, html, consentIdentityCopy)
		require.NotContains(t, html, "> Reconnect </button>")
	}
}

func TestServeConsent_OffersIdentityReconnectOnlyWhenOpenIDWouldBeRequested(t *testing.T) {
	t.Parallel()
	ctx, fx, _, clientID, issuerID := metaConsent(t, "identity-hint")

	// The issuer advertises nothing yet, so a reconnect would not add openid.
	grantScoped(t, ctx, fx, clientID, []string{"read"})
	expectIdentityHint(t, fx, false)

	// Discovery now shows openid: the same grant is worth reconnecting.
	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	expectIdentityHint(t, fx, true)

	// A grant that already carries openid has nothing to gain.
	grantScoped(t, ctx, fx, clientID, []string{"read", "openid"})
	expectIdentityHint(t, fx, false)
}

func TestServeConsent_NoIdentityReconnectWhenOverrideOmitsOpenID(t *testing.T) {
	t.Parallel()
	ctx, fx, _, clientID, issuerID := metaConsent(t, "identity-override")

	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	_, err := remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionIssuer(ctx, remotesessions_repo.UpdateRemoteSessionIssuerParams{
		ScopeOverride: []string{"read"},
		ID:            issuerID,
		ProjectID:     conv.ToNullUUID(fx.projectID),
	})
	require.NoError(t, err)

	grantScoped(t, ctx, fx, clientID, []string{"read"})
	expectIdentityHint(t, fx, false)
}
