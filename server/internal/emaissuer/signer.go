package emaissuer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// AssertionType is the JOSE typ of an ID-JAG.
const AssertionType = "oauth-id-jag+jwt"

// ErrSigningDisabled is returned while the platform kill switch is on.
var ErrSigningDisabled = errors.New("emaissuer: signing is disabled")

// Signer signs with an organization's issuer key. It is internal: no customer
// API or tool exposes it.
type Signer struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	gcpIdentity *gcpauth.Identity
	kmsClients  gcpkms.SigningClientFactory
	// disabled is the platform-wide kill switch. Discovery keeps serving.
	disabled bool
	// managedSigner, when set, is the only service account the key's
	// credential may impersonate.
	managedSigner string
}

// SignerConfig configures a Signer.
type SignerConfig struct {
	// Disabled refuses every signature, for incident response.
	Disabled bool
	// ManagedSigner pins the service account the key credential may use.
	ManagedSigner string
}

func NewSigner(logger *slog.Logger, db *pgxpool.Pool, gcpIdentity *gcpauth.Identity, kmsClients gcpkms.SigningClientFactory, cfg SignerConfig) *Signer {
	return &Signer{
		logger:        logger.With(attr.SlogComponent("emaissuer")),
		db:            db,
		gcpIdentity:   gcpIdentity,
		kmsClients:    kmsClients,
		disabled:      cfg.Disabled,
		managedSigner: strings.TrimSpace(cfg.ManagedSigner),
	}
}

// Enabled reports whether signing is permitted at all on this deployment.
func (s *Signer) Enabled() bool {
	return s != nil && !s.disabled && s.gcpIdentity != nil && s.kmsClients != nil
}

// Sign serializes claims as an ID-JAG signed by the organization's active
// issuer key, setting iss to the issuer URL pinned when it was enabled. Callers own every
// other claim and its validation.
func (s *Signer) Sign(ctx context.Context, organizationID string, claims map[string]any) (string, error) {
	if !s.Enabled() {
		return "", ErrSigningDisabled
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(s.db)
	row, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNotFound
	case err != nil:
		return "", fmt.Errorf("load issuer: %w", err)
	}
	if row.DisabledAt.Valid {
		return "", ErrDisabled
	}
	key, err := q.GetActiveJsonWebKey(ctx, repo.GetActiveJsonWebKeyParams{JsonWebKeySetID: row.JsonWebKeySetID, OrganizationID: organizationID})
	if err != nil {
		return "", fmt.Errorf("load active issuer key: %w", err)
	}
	backing, err := q.GetExternalKeyForMint(ctx, repo.GetExternalKeyForMintParams{ID: key.ExternalKeyID, OrganizationID: conv.ToPGText(organizationID)})
	if err != nil {
		return "", fmt.Errorf("load issuer backing key: %w", err)
	}
	// Only a key that backs this organization's issuer may sign as it.
	if !backing.EmaIssuerManaged || !backing.ResourceName.Valid || backing.ResourceName.String == "" {
		return "", errors.New("emaissuer: active key is not a managed issuer key")
	}
	if s.managedSigner != "" {
		if got := strings.TrimSpace(backing.ImpersonateServiceAccount.String); got != "" && !strings.EqualFold(got, s.managedSigner) {
			return "", fmt.Errorf("emaissuer: key credential impersonates %s, configured signer is %s", got, s.managedSigner)
		}
	}
	credential, problem, detail, err := s.gcpIdentity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   backing.CredentialID.Valid,
		ImpersonateServiceAccount: backing.ImpersonateServiceAccount.String,
		HasWifConfig:              backing.WifPoolID.Valid || backing.WifProviderID.Valid || backing.WifProjectNumber.Valid,
		SkipProjectVerification:   backing.SkipProjectVerification.Bool,
	})
	if err != nil {
		return "", fmt.Errorf("screen issuer credential: %w", err)
	}
	if problem != "" {
		return "", fmt.Errorf("emaissuer: issuer credential is unusable: %s", detail)
	}
	tokenSource, err := s.gcpIdentity.TokenSource(ctx, credential)
	if err != nil {
		return "", fmt.Errorf("build issuer credential: %w", err)
	}
	kmsClient, err := s.kmsClients(ctx, tokenSource)
	if err != nil {
		return "", fmt.Errorf("build issuer KMS client: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, "failed to close issuer KMS client", func() error { return kmsClient.Close() })
	return sign(ctx, kmsClient, backing.ResourceName.String, key.Kid, key.PublicJwk, row.Issuer, claims)
}

func sign(ctx context.Context, client gcpkms.SigningClient, resourceName, kid string, publicJWKDocument []byte, issuer string, claims map[string]any) (string, error) {
	var publicJWK jose.JSONWebKey
	if err := publicJWK.UnmarshalJSON(publicJWKDocument); err != nil {
		return "", fmt.Errorf("decode issuer public key: %w", err)
	}
	alg, err := gcpkms.ParseSignatureAlgorithm(publicJWK.Algorithm)
	if err != nil {
		return "", fmt.Errorf("resolve issuer algorithm: %w", err)
	}
	opaque, err := gcpkms.NewSigner(ctx, client, resourceName, kid, gcpkms.PublicKey{Algorithm: alg, Key: publicJWK.Key})
	if err != nil {
		return "", fmt.Errorf("build issuer signer: %w", err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: opaque}, new(jose.SignerOptions).WithType(AssertionType))
	if err != nil {
		return "", fmt.Errorf("configure issuer signer: %w", err)
	}
	body := make(map[string]any, len(claims)+1)
	maps.Copy(body, claims)
	body["iss"] = issuer
	assertion, err := jwt.Signed(signer).Claims(body).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign assertion: %w", err)
	}
	return assertion, nil
}
