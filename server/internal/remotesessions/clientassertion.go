package remotesessions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	jsonwebkeysets_repo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

const (
	clientAssertionLifetime = 60 * time.Second
	clientAssertionType     = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
)

type ClientAssertionRequest struct {
	RemoteSessionClientID uuid.UUID
	OrganizationID        string
	JSONWebKeySetID       uuid.UUID
	ClientID              string
	Audience              string
}

// TokenEndpointAssertionSigner creates one fresh RFC 7523 client assertion for
// one outbound request. Implementations must not cache the serialized result:
// every request, including a retry, needs a new jti and time window.
type TokenEndpointAssertionSigner interface {
	SignClientAssertion(ctx context.Context, request ClientAssertionRequest) (string, error)
}

type unavailableTokenEndpointAssertionSigner struct{}

func (unavailableTokenEndpointAssertionSigner) SignClientAssertion(context.Context, ClientAssertionRequest) (string, error) {
	return "", fmt.Errorf("private_key_jwt signing is unavailable")
}

type KMSClientAssertionSigner struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	gcpIdentity *gcpauth.Identity
	kmsClients  gcpkms.SigningClientFactory
	now         func() time.Time
}

func NewKMSClientAssertionSigner(logger *slog.Logger, db *pgxpool.Pool, gcpIdentity *gcpauth.Identity, kmsClients gcpkms.SigningClientFactory) *KMSClientAssertionSigner {
	return &KMSClientAssertionSigner{
		logger:      logger.With(attr.SlogComponent("remotesessions_client_assertion")),
		db:          db,
		gcpIdentity: gcpIdentity,
		kmsClients:  kmsClients,
		now:         time.Now,
	}
}

func (s *KMSClientAssertionSigner) SignClientAssertion(ctx context.Context, request ClientAssertionRequest) (string, error) {
	logger := s.logger.With(attr.SlogRemoteSessionClientID(request.RemoteSessionClientID.String()))
	if request.OrganizationID == "" || request.JSONWebKeySetID == uuid.Nil {
		return "", fmt.Errorf("private_key_jwt requires an organization-owned JSON Web Key Set")
	}
	if request.ClientID == "" || request.Audience == "" {
		return "", fmt.Errorf("private_key_jwt requires non-empty client and audience identifiers")
	}

	q := jsonwebkeysets_repo.New(s.db)
	key, err := q.GetActiveJsonWebKey(ctx, jsonwebkeysets_repo.GetActiveJsonWebKeyParams{
		JsonWebKeySetID: request.JSONWebKeySetID,
		OrganizationID:  request.OrganizationID,
	})
	if err != nil {
		return "", fmt.Errorf("load active client assertion key: %w", err)
	}

	backing, err := q.GetExternalKeyForMint(ctx, jsonwebkeysets_repo.GetExternalKeyForMintParams{
		ID:             key.ExternalKeyID,
		OrganizationID: conv.ToPGText(key.OrganizationID),
	})
	if err != nil {
		return "", fmt.Errorf("load client assertion backing key: %w", err)
	}
	if !backing.ResourceName.Valid || backing.ResourceName.String == "" {
		return "", fmt.Errorf("active client assertion key is not backed by GCP KMS")
	}

	credential, problem, detail, err := s.gcpIdentity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   backing.CredentialID.Valid,
		ImpersonateServiceAccount: backing.ImpersonateServiceAccount.String,
		HasWifConfig:              backing.WifPoolID.Valid || backing.WifProviderID.Valid || backing.WifProjectNumber.Valid,
		SkipProjectVerification:   backing.SkipProjectVerification.Bool,
	})
	if err != nil {
		return "", fmt.Errorf("screen client assertion credential: %w", err)
	}
	if problem != "" {
		return "", fmt.Errorf("client assertion credential is unusable: %s", detail)
	}

	tokenSource, err := s.gcpIdentity.TokenSource(ctx, credential)
	if err != nil {
		return "", fmt.Errorf("build client assertion credential: %w", err)
	}
	kmsClient, err := s.kmsClients(ctx, tokenSource)
	if err != nil {
		return "", fmt.Errorf("build client assertion KMS client: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, "failed to close client assertion KMS client", func() error { return kmsClient.Close() })

	return serializeClientAssertion(ctx, kmsClient, backing.ResourceName.String, key.Kid, key.PublicJwk, request.ClientID, request.Audience, s.now().UTC())
}

func serializeClientAssertion(ctx context.Context, kmsClient gcpkms.SigningClient, resourceName, kid string, publicJWKDocument []byte, clientID, audience string, now time.Time) (string, error) {
	var publicJWK jose.JSONWebKey
	if err := publicJWK.UnmarshalJSON(publicJWKDocument); err != nil {
		return "", fmt.Errorf("decode active client assertion public JWK: %w", err)
	}
	alg, err := gcpkms.ParseSignatureAlgorithm(publicJWK.Algorithm)
	if err != nil {
		return "", fmt.Errorf("resolve active client assertion algorithm: %w", err)
	}
	opaque, err := gcpkms.NewSigner(ctx, kmsClient, resourceName, kid, gcpkms.PublicKey{
		Algorithm: alg,
		Key:       publicJWK.Key,
	})
	if err != nil {
		return "", fmt.Errorf("build client assertion signer: %w", err)
	}
	joseSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: opaque},
		new(jose.SignerOptions).WithType("client-authentication+jwt"),
	)
	if err != nil {
		return "", fmt.Errorf("configure client assertion signer: %w", err)
	}

	assertion, err := jwt.Signed(joseSigner).Claims(jwt.Claims{
		Issuer:    clientID,
		Subject:   clientID,
		Audience:  jwt.Audience{audience},
		Expiry:    jwt.NewNumericDate(now.Add(clientAssertionLifetime)),
		NotBefore: nil,
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        uuid.NewString(),
	}).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign client assertion: %w", err)
	}
	return assertion, nil
}
