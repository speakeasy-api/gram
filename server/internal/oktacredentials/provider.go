// Package oktacredentials coordinates Okta token exchanges with connection writes.
package oktacredentials

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

// Provider is a comparable config value, so worker clients retain their cache.
// Tx is only for verification, which already holds the connection lock and may
// have an uncommitted replacement secret. Never open another transaction there.
type Provider struct {
	DB           *pgxpool.Pool
	Tx           pgx.Tx
	ConnectionID uuid.UUID
	// ObservedDPoP lets a verification caller retain binding evidence even if
	// its credential replacement transaction subsequently rolls back.
	ObservedDPoP *atomic.Bool
}

func (p Provider) Acquire(ctx context.Context, cfg okta.Config) (okta.CredentialLease, error) {
	if cfg.AuthMethod != remotesessions.TokenEndpointAuthMethodBasic {
		return nil, errors.New("okta credential leases require client_secret_basic")
	}
	tx := p.Tx
	owned := tx == nil
	if owned {
		var err error
		tx, err = p.DB.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin okta credential transaction: %w", err)
		}
	}
	l := &lease{tx: tx, owned: owned, connectionID: p.ConnectionID, org: cfg.OrganizationID, observed: p.ObservedDPoP, secret: "", required: false, clientID: cfg.ClientID}
	success := false
	defer func() {
		if !success {
			l.Close()
		}
	}()
	q := repo.New(tx)
	_, err := q.LockOktaIdentityProviderConnection(ctx, repo.LockOktaIdentityProviderConnectionParams{ID: p.ConnectionID, OrganizationID: cfg.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("lock okta connection: %w", err)
	}
	// The locking join only locks the parent. If it waited, its subtype may
	// still come from the pre-wait statement snapshot. Read again under the
	// parent lock so a previous exchange's committed pin is visible.
	rows, err := q.GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{ID: conv.ToNullUUID(p.ConnectionID), OrganizationID: cfg.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("read locked okta connection: %w", err)
	}
	if rows.OktaIdentityProviderConnection.RemoteSessionClientID != cfg.RemoteSessionClientID {
		return nil, errors.New("okta credential changed")
	}
	credential, err := q.LockOktaTokenCredential(ctx, repo.LockOktaTokenCredentialParams{ID: cfg.RemoteSessionClientID, OrganizationID: conv.ToPGText(cfg.OrganizationID), IdentityProviderConnectionID: conv.ToNullUUID(p.ConnectionID)})
	if err != nil {
		return nil, fmt.Errorf("lock okta credential: %w", err)
	}
	if credential.ClientID != cfg.ClientID {
		return nil, errors.New("okta client changed")
	}
	l.secret = credential.ClientSecretEncrypted.String
	if cfg.AuthMethod == remotesessions.TokenEndpointAuthMethodBasic && l.secret == "" {
		return nil, errors.New("okta credential revoked")
	}
	l.required = rows.OktaIdentityProviderConnection.DpopRequired
	success = true
	return l, nil
}

// RequireDPoP reads the committed pin, or the transaction's view of it for a
// verification provider, without locking the connection.
func (p Provider) RequireDPoP(ctx context.Context, cfg okta.Config) (bool, error) {
	var q *repo.Queries
	if p.Tx != nil {
		q = repo.New(p.Tx)
	} else {
		q = repo.New(p.DB)
	}
	rows, err := q.GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{ID: conv.ToNullUUID(p.ConnectionID), OrganizationID: cfg.OrganizationID})
	if err != nil {
		return false, fmt.Errorf("read okta connection pin: %w", err)
	}
	return rows.OktaIdentityProviderConnection.DpopRequired, nil
}

type lease struct {
	clientID     string
	observed     *atomic.Bool
	tx           pgx.Tx
	owned        bool
	connectionID uuid.UUID
	org          string
	secret       string
	required     bool
}

func (l *lease) EncryptedSecret() string { return l.secret }
func (l *lease) RequireDPoP() bool       { return l.required }
func (l *lease) Close() {
	if l.owned {
		_ = l.tx.Rollback(context.Background())
	}
}
func (l *lease) Observe(ctx context.Context, bound bool) error {
	if bound {
		if l.observed != nil {
			l.observed.Store(true)
		}
		if err := repo.New(l.tx).PinOktaDPoP(ctx, repo.PinOktaDPoPParams{ClientID: l.clientID, IdentityProviderConnectionID: l.connectionID, OrganizationID: l.org}); err != nil {
			return fmt.Errorf("pin okta binding: %w", err)
		}
	}
	if l.owned {
		if err := l.tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit okta credential transaction: %w", err)
		}
	}
	return nil
}
