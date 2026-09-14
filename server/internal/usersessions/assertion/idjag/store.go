package idjag

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// PostgresStore reads the configured trust link and resolves active directory
// identities to active users in the same organization.
type PostgresStore struct {
	userSessions   *usersessionsrepo.Queries
	remoteSessions *remotesessionsrepo.Queries
	directory      *directoryrepo.Queries
}

// NewPostgresStore builds the database-backed ID-JAG validation store.
func NewPostgresStore(db *pgxpool.Pool) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("idjag: database is required")
	}
	return &PostgresStore{
		userSessions:   usersessionsrepo.New(db),
		remoteSessions: remotesessionsrepo.New(db),
		directory:      directoryrepo.New(db),
	}, nil
}

var _ Store = (*PostgresStore)(nil)

// TrustedIssuer follows an organization-level user session issuer's explicit
// link to an active organization or global remote issuer.
func (s *PostgresStore) TrustedIssuer(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID) (TrustedIssuer, error) {
	issuer, err := s.userSessions.GetOrganizationUserSessionIssuerByID(ctx, usersessionsrepo.GetOrganizationUserSessionIssuerByIDParams{
		ID: userSessionIssuerID, OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustedIssuer{}, ErrNoTrustedIssuer
	}
	if err != nil {
		return TrustedIssuer{}, fmt.Errorf("load user session issuer: %w", err)
	}
	if !issuer.TrustedRemoteSessionIssuerID.Valid {
		return TrustedIssuer{}, ErrNoTrustedIssuer
	}
	trusted, err := s.remoteSessions.GetTrustedRemoteSessionIssuerForOrganization(ctx, remotesessionsrepo.GetTrustedRemoteSessionIssuerForOrganizationParams{
		ID: issuer.TrustedRemoteSessionIssuerID.UUID, OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustedIssuer{}, ErrNoTrustedIssuer
	}
	if err != nil {
		return TrustedIssuer{}, fmt.Errorf("load linked remote issuer: %w", err)
	}
	if !trusted.JwksUri.Valid || trusted.JwksUri.String == "" {
		return TrustedIssuer{}, errors.New("linked remote issuer has no jwks_uri")
	}
	return TrustedIssuer{ID: trusted.ID, Issuer: trusted.Issuer, JWKSURI: trusted.JwksUri.String}, nil
}

// ResolveUser refuses an absent or ambiguous active directory mapping.
func (s *PostgresStore) ResolveUser(ctx context.Context, organizationID, email string) (string, error) {
	users, err := s.directory.ResolveIDJAGUsersByEmail(ctx, directoryrepo.ResolveIDJAGUsersByEmailParams{
		OrganizationID: organizationID, Email: email,
	})
	if err != nil {
		return "", fmt.Errorf("resolve directory identity: %w", err)
	}
	if len(users) != 1 {
		return "", ErrNotProvisioned
	}
	return users[0], nil
}
