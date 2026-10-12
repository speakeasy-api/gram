package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// AssertionType is the JOSE typ of an ID-JAG.
const AssertionType = "oauth-id-jag+jwt"

// ErrInvalidClaims means a caller asked for an assertion Speakeasy won't sign.
var ErrInvalidClaims = errors.New("internalsessionissuers: invalid assertion claims")

// SigningRefusedError means the organization's issuer state doesn't allow
// signing: disabled, paused, not entitled or not rolled out.
type SigningRefusedError struct{ State State }

func (e *SigningRefusedError) Error() string {
	return "internalsessionissuers: signing refused, issuer is " + string(e.State)
}

// reservedClaims are set by Sign or by typed Claims fields, never by Extra.
var reservedClaims = map[string]bool{"iss": true, "iat": true, "nbf": true, "exp": true, "sub": true, "aud": true, "jti": true, "email": true, "client_id": true, "resource": true, "scope": true}

// Claims are the caller-owned claims of one ID-JAG. iss and iat are always
// set by Sign.
type Claims struct {
	Subject  string
	Email    string
	Audience string
	ClientID string
	Resource string
	Scope    string
	ID       string
	Expiry   time.Time
	// Extra carries vendor-specific claims; reserved names are refused.
	Extra map[string]any
}

func (c Claims) validate(now time.Time) error {
	switch {
	case strings.TrimSpace(c.Subject) == "":
		return fmt.Errorf("%w: sub is required", ErrInvalidClaims)
	case strings.TrimSpace(c.Audience) == "":
		return fmt.Errorf("%w: aud is required", ErrInvalidClaims)
	case strings.TrimSpace(c.ClientID) == "":
		return fmt.Errorf("%w: client_id is required", ErrInvalidClaims)
	case strings.TrimSpace(c.ID) == "":
		return fmt.Errorf("%w: jti is required", ErrInvalidClaims)
	case c.Expiry.IsZero() || !c.Expiry.After(now):
		return fmt.Errorf("%w: exp must be in the future", ErrInvalidClaims)
	case c.Expiry.After(now.Add(MaxAssertionLifetime)):
		return fmt.Errorf("%w: exp exceeds %s", ErrInvalidClaims, MaxAssertionLifetime)
	}
	for name := range c.Extra {
		if reservedClaims[name] {
			return fmt.Errorf("%w: %s cannot be set as an extra claim", ErrInvalidClaims, name)
		}
	}
	return nil
}

func (c Claims) body(issuer string, now time.Time) map[string]any {
	body := make(map[string]any, len(c.Extra)+9)
	maps.Copy(body, c.Extra)
	body["iss"] = issuer
	body["iat"] = jwt.NewNumericDate(now)
	body["exp"] = jwt.NewNumericDate(c.Expiry)
	body["sub"] = c.Subject
	body["aud"] = c.Audience
	body["jti"] = c.ID
	body["client_id"] = c.ClientID
	for name, value := range map[string]string{"email": c.Email, "resource": c.Resource, "scope": c.Scope} {
		if value != "" {
			body[name] = value
		}
	}
	return body
}

// Signer signs with an organization's issuer key. It is internal: no customer
// API or tool exposes it.
type Signer struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	metrics     *metrics
	db          *pgxpool.Pool
	gate        *Gate
	gcpIdentity *gcpauth.Identity
	kmsClients  gcpkms.SigningClientFactory
	// managedSigner pins the service account the key's credential may impersonate.
	managedSigner string
	now           func() time.Time
}

func NewSigner(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, gate *Gate, gcpIdentity *gcpauth.Identity, kmsClients gcpkms.SigningClientFactory, managedSigner string) *Signer {
	logger = logger.With(attr.SlogComponent("internalsessionissuers"))
	return &Signer{
		logger:        logger,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/internalsessionissuers"),
		metrics:       newMetrics(logger, meterProvider),
		db:            db,
		gate:          gate,
		gcpIdentity:   gcpIdentity,
		kmsClients:    kmsClients,
		managedSigner: strings.TrimSpace(managedSigner),
		now:           time.Now,
	}
}

// Sign serializes claims as an ID-JAG signed by the organization's active
// issuer key, with iss set to the issuer URL pinned when it was enabled.
func (s *Signer) Sign(ctx context.Context, organizationID string, claims Claims) (assertion string, err error) {
	ctx, span := s.tracer.Start(ctx, "internalsessionissuers.Sign")
	defer func() {
		s.metrics.record(ctx, opSign, err)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	now := s.now()
	if err := claims.validate(now); err != nil {
		return "", err
	}
	if s.gcpIdentity == nil || s.kmsClients == nil {
		return "", fmt.Errorf("%w: no signing credentials on this deployment", ErrMisconfigured)
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("read issuer status: %w", err)
	}
	if status.State != StateEnabled || status.Issuer == nil || status.ActiveKey == nil {
		return "", &SigningRefusedError{State: status.State}
	}
	issuer := *status.Issuer
	key := *status.ActiveKey
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(s.db)
	backing, err := q.GetExternalKeyForMint(ctx, repo.GetExternalKeyForMintParams{ID: key.ExternalKeyID, OrganizationID: conv.ToPGText(organizationID)})
	if err != nil {
		return "", fmt.Errorf("load issuer backing key: %w", err)
	}
	// Only a key that backs this organization's issuer may sign as it.
	if !backing.EmaIssuerManaged || !backing.ResourceName.Valid || backing.ResourceName.String == "" {
		return "", fmt.Errorf("%w: active key is not a managed issuer key", ErrMisconfigured)
	}
	// The key's credential must impersonate exactly the configured signer.
	if s.managedSigner == "" {
		return "", fmt.Errorf("%w: no platform signing service account is configured", ErrMisconfigured)
	}
	if got := strings.TrimSpace(backing.ImpersonateServiceAccount.String); !strings.EqualFold(got, s.managedSigner) {
		return "", fmt.Errorf("%w: key credential impersonates %q, configured signer is %s", ErrMisconfigured, got, s.managedSigner)
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
		return "", fmt.Errorf("%w: issuer credential is unusable: %s", ErrMisconfigured, detail)
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
	assertion, err = sign(ctx, kmsClient, backing.ResourceName.String, key.Kid, key.PublicJwk, claims.body(issuer.Issuer, now))
	if err != nil {
		return "", err
	}
	// A disable or revocation that committed while KMS was signing wins.
	current, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("recheck issuer after signing: %w", err)
	}
	if current.DisabledAt.Valid {
		return "", &SigningRefusedError{State: StateDisabled}
	}
	live, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: issuer.JsonWebKeySetID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return "", fmt.Errorf("recheck issuer key after signing: %w", err)
	}
	if !slices.ContainsFunc(live, func(k repo.JsonWebKey) bool { return k.ID == key.ID }) {
		return "", &SigningRefusedError{State: StateKeyless}
	}
	return assertion, nil
}

func sign(ctx context.Context, client gcpkms.SigningClient, resourceName, kid string, publicJWKDocument []byte, body map[string]any) (string, error) {
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
	assertion, err := jwt.Signed(signer).Claims(body).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign assertion: %w", err)
	}
	return assertion, nil
}
