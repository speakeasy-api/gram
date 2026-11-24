// Package ingresscredentials seals, opens and stores the provider credentials
// of network ingresses: the Tailscale OAuth client an organization connects
// its private network with.
//
// Credentials have two stored copies: network_ingresses.credentials_encrypted
// under the platform key and network_ingresses.credentials_org_encrypted under
// the owning organization's key. Which copies are written and read follows the
// organization's transition state for the tailscale-oauth-clients use case
// (package orgencryption). Every reader and writer goes through Credentials,
// and every statement that writes either copy lives in credential_queries.sql
// in package networkingress.
//
// Every network ingress row carries its organization, so nothing is ever
// unattributed.
package ingresscredentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// UseCase is the organization encryption use case the credentials move to.
const UseCase = orgencryption.UseCaseTailscaleOAuthClients

// Columns names the credentials' two copies. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var Columns = orgencryption.CopyColumns{
	UseCase:        UseCase,
	PlatformColumn: "network_ingresses.credentials_encrypted",
	OrgColumn:      "network_ingresses.credentials_org_encrypted",
}

// maxWriteAttempts bounds how often a write decides, seals and writes again
// after the transition moved under it. Each retry follows a committed
// transition, and an organization changes state a handful of times in its
// life, so a third consecutive move means something is wrong.
const maxWriteAttempts = 3

// ErrIngressNotFound means the ingress to write does not exist, is deleted, or
// belongs to another organization.
var ErrIngressNotFound = errors.New("ingresscredentials: network ingress not found")

// Stored returns the copies a network ingress row stores.
func Stored(row repo.NetworkIngress) orgencryption.Copies {
	return orgencryption.Copies{Platform: row.CredentialsEncrypted, Org: row.CredentialsOrgEncrypted}
}

// Has reports whether a row stores credentials in either copy.
func Has(stored orgencryption.Copies) bool {
	return (stored.Platform.Valid && stored.Platform.String != "") || (stored.Org.Valid && stored.Org.String != "")
}

// Credentials is the one seam for network ingress credentials.
type Credentials struct {
	logger *slog.Logger
	cipher *orgencryption.Cipher
}

// New builds Credentials over the process's cipher.
func New(logger *slog.Logger, cipher *orgencryption.Cipher) *Credentials {
	return &Credentials{
		logger: logger.With(attr.SlogComponent("ingresscredentials")),
		cipher: cipher,
	}
}

// WithoutOrganizationEncryption builds Credentials for a process without
// organization encryption, over its own transition state: organizations in
// platform_key work, and any other transition state fails closed.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Credentials {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions))
}

// Open decrypts the copy the organization's transition state reads. db is
// whatever the caller already holds (a connection or a transaction); a cache
// miss reads transition state through it. A nil db reads through the
// process's pool.
func (c *Credentials) Open(ctx context.Context, db repo.DBTX, organizationID string, stored orgencryption.Copies) ([]byte, error) {
	decision, err := c.decide(ctx, db, organizationID)
	if err != nil {
		return nil, err
	}

	plaintext, err := c.cipher.Open(ctx, decision, Columns, stored)
	if err != nil {
		return nil, fmt.Errorf("open network ingress credentials: %w", err)
	}

	return plaintext, nil
}

func (c *Credentials) decide(ctx context.Context, db repo.DBTX, organizationID string) (orgencryption.Decision, error) {
	var decision orgencryption.Decision
	var err error
	if db == nil {
		decision, err = c.cipher.Decide(ctx, organizationID, UseCase)
	} else {
		decision, err = c.cipher.DecideIn(ctx, db, organizationID, UseCase)
	}
	if err != nil {
		return orgencryption.Decision{}, fmt.Errorf("decide network ingress credential state: %w", err)
	}

	return decision, nil
}

// Set stores plaintext as the ingress's new or replaced credentials and
// returns the updated row, which is pending reconciliation again. Run it in
// the same transaction as the write that created or changed the ingress.
//
// When the transition moves between the decision and the write, Set decides
// again and seals the same plaintext again.
func (c *Credentials) Set(ctx context.Context, dbtx repo.DBTX, ingressID uuid.UUID, organizationID string, plaintext []byte) (repo.NetworkIngress, error) {
	q := repo.New(dbtx)

	decision, err := c.cipher.DecideIn(ctx, dbtx, organizationID, UseCase)
	if err != nil {
		return repo.NetworkIngress{}, fmt.Errorf("decide network ingress credential state: %w", err)
	}

	for range maxWriteAttempts {
		copies, err := c.cipher.Seal(ctx, decision, Columns, plaintext)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			decision, err = c.cipher.RedecideIn(ctx, dbtx, decision)
			if err != nil {
				return repo.NetworkIngress{}, fmt.Errorf("decide network ingress credential state again: %w", err)
			}
			continue
		}
		if err != nil {
			return repo.NetworkIngress{}, fmt.Errorf("seal network ingress credentials: %w", err)
		}

		row, err := q.SetNetworkIngressCredentials(ctx, repo.SetNetworkIngressCredentialsParams{
			PlatformCopy:      copies.Platform,
			OrgCopy:           copies.Org,
			ID:                ingressID,
			OrganizationID:    organizationID,
			DecidedGeneration: decision.Generation,
		})
		switch {
		case err == nil:
			return row, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return repo.NetworkIngress{}, fmt.Errorf("store network ingress credentials: %w", err)
		}

		fresh, err := c.cipher.RedecideIn(ctx, dbtx, decision)
		if err != nil {
			return repo.NetworkIngress{}, fmt.Errorf("decide network ingress credential state again: %w", err)
		}
		if fresh.Generation == decision.Generation {
			return repo.NetworkIngress{}, ErrIngressNotFound
		}

		c.logger.InfoContext(ctx, "network ingress credential write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID), attr.SlogOrgEncryptionUseCase(UseCase))
		decision = fresh
	}

	return repo.NetworkIngress{}, fmt.Errorf("store network ingress credentials: %w", orgencryption.ErrFenced)
}
