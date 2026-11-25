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
// signing: disabled, paused, no verified domain or not rolled out.
type SigningRefusedError struct {
	State State
	// Reason is set when the pause switch refused.
	Reason PauseReason
}

func (e *SigningRefusedError) Error() string {
	return "internalsessionissuers: signing refused, issuer is " + string(e.State)
}

// reservedClaims are set by Sign or by typed Claims fields, or carry identity
// or delegation meaning a vendor may trust, so Extra can never set them.
var reservedClaims = map[string]bool{
	"iss": true, "iat": true, "nbf": true, "exp": true, "sub": true, "aud": true, "jti": true, "email": true, "client_id": true, "resource": true, "scope": true,
	"email_verified": true, "act": true, "may_act": true, "azp": true, "auth_time": true, "amr": true, "acr": true, "cnf": true, "nonce": true,
	"tenant": true, "tid": true, "org_id": true, "sub_id": true, "upn": true, "preferred_username": true, "name": true, "groups": true, "roles": true,
}

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
	case strings.TrimSpace(c.Email) == "":
		return fmt.Errorf("%w: email is required", ErrInvalidClaims)
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

// emailOnVerifiedDomain reports whether email's domain is exactly one of the
// organization's verified domains. Vendors match users on email alone.
func emailOnVerifiedDomain(email string, verifiedDomains []string) bool {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") || domain != strings.TrimSpace(domain) || !isASCII(domain) {
		return false
	}
	// ASCII-only folding: Unicode case folding would match lookalikes such as
	// the Kelvin sign against k.
	domain = strings.ToLower(domain)
	return slices.ContainsFunc(verifiedDomains, func(d string) bool { return strings.ToLower(strings.TrimSpace(d)) == domain })
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
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
	deployment    Deployment
	now           func() time.Time
}

// SignerConfig identifies this deployment's authority and signing identity.
type SignerConfig struct {
	// Deployment pins the permitted issuer origin and key ring.
	Deployment Deployment

	// ManagedSigner is the only service account a key credential may impersonate.
	ManagedSigner string
}

func NewSigner(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, gate *Gate, gcpIdentity *gcpauth.Identity, kmsClients gcpkms.SigningClientFactory, cfg SignerConfig) *Signer {
	logger = logger.With(attr.SlogComponent("internalsessionissuers"))
	return &Signer{
		logger:        logger,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/internalsessionissuers"),
		metrics:       newMetrics(logger, meterProvider),
		db:            db,
		gate:          gate,
		gcpIdentity:   gcpIdentity,
		kmsClients:    kmsClients,
		managedSigner: strings.TrimSpace(cfg.ManagedSigner),
		deployment:    cfg.Deployment,
		now:           time.Now,
	}
}

// Available reports whether this deployment has the configuration required to
// sign. The per-organization lifecycle gate and stored credential are checked
// again for each assertion; configuration alone never authorizes a signature.
func (s *Signer) Available() bool {
	if s == nil || s.gate == nil || s.db == nil || s.gcpIdentity == nil || s.kmsClients == nil || s.managedSigner == "" {
		return false
	}
	if _, err := URL(s.deployment.ServerURL, "configuration-check", s.deployment.AllowLoopbackIssuer); err != nil {
		return false
	}
	return gcpkms.ValidateKeyRingName(s.deployment.KeyRing) == nil
}

// CheckEnableAuthority checks configuration and retained issuer ownership without
// requiring an active key: disabled and new issuers may not have one yet.
func (s *Signer) CheckEnableAuthority(organizationID string, status Status) error {
	if !s.Available() {
		return fmt.Errorf("%w: signing configuration unavailable", ErrMisconfigured)
	}
	if status.Issuer != nil {
		return s.deployment.CheckIssuer(organizationID, status.Issuer.Issuer)
	}
	return nil
}

// CheckAuthority requires the selected issuer and active key to belong to this
// deployment. Cache readers use the same check as fresh signing, so copying a
// database cannot grant authority to release credentials from another issuer.
func (s *Signer) CheckAuthority(ctx context.Context, organizationID string, status Status) error {
	_, err := s.authority(ctx, organizationID, status)
	return err
}

// CheckActivation approves activating a ready pending key without an admin:
// the issuer must be enabled (rolled out, on a verified domain, not disabled,
// not paused), and both the current and the pending key must pass the checks
// a signing key passes.
func (s *Signer) CheckActivation(ctx context.Context, organizationID string, pending repo.JsonWebKey) error {
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return err
	}
	if _, err := s.authority(ctx, organizationID, status); err != nil {
		return err
	}
	if pending.State != "pending" {
		return &SigningRefusedError{State: status.State, Reason: status.PauseReason}
	}
	status.ActiveKey = &pending
	_, err = s.authority(ctx, organizationID, status)
	return err
}

func (s *Signer) authority(ctx context.Context, organizationID string, status Status) (repo.GetExternalKeyForMintRow, error) {
	var none repo.GetExternalKeyForMintRow
	if !s.Available() {
		return none, fmt.Errorf("%w: signing configuration unavailable", ErrMisconfigured)
	}
	if status.State != StateEnabled || status.Issuer == nil || status.ActiveKey == nil {
		return none, &SigningRefusedError{State: status.State, Reason: status.PauseReason}
	}
	if status.Issuer.OrganizationID != organizationID || status.ActiveKey.OrganizationID != organizationID || status.ActiveKey.JsonWebKeySetID != status.Issuer.JsonWebKeySetID {
		return none, fmt.Errorf("%w: issuer organization mismatch", ErrMisconfigured)
	}
	if err := s.deployment.CheckIssuer(organizationID, status.Issuer.Issuer); err != nil {
		return none, err
	}
	backing, err := repo.New(s.db).GetExternalKeyForMint(ctx, repo.GetExternalKeyForMintParams{ID: status.ActiveKey.ExternalKeyID, OrganizationID: conv.ToPGText(organizationID)})
	if err != nil {
		return none, fmt.Errorf("load issuer backing key: %w", err)
	}
	if !backing.EmaIssuerManaged || !backing.ResourceName.Valid || backing.ResourceName.String == "" {
		return none, fmt.Errorf("%w: active key is not a managed issuer key", ErrMisconfigured)
	}
	if err := s.deployment.CheckKey(backing.ResourceName.String); err != nil {
		return none, err
	}
	if !strings.EqualFold(strings.TrimSpace(backing.ImpersonateServiceAccount.String), s.managedSigner) {
		return none, fmt.Errorf("%w: issuer credential signing identity mismatch", ErrMisconfigured)
	}
	if !backing.CredentialID.Valid {
		return none, fmt.Errorf("%w: issuer credential unavailable", ErrMisconfigured)
	}
	return backing, nil
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
	if !s.Available() {
		return "", fmt.Errorf("%w: signing configuration unavailable", ErrMisconfigured)
	}
	status, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("read issuer status: %w", err)
	}
	backing, err := s.authority(ctx, organizationID, status)
	if err != nil {
		return "", err
	}
	if !emailOnVerifiedDomain(claims.Email, status.VerifiedDomains) {
		return "", fmt.Errorf("%w: email is not on a verified domain of the organization", ErrInvalidClaims)
	}
	issuer, key := *status.Issuer, *status.ActiveKey
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	q := repo.New(s.db)
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
	// Re-read every signing gate after KMS, including kill switches and verified domains.
	currentStatus, err := s.gate.Status(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("recheck signing gate: %w", err)
	}
	if currentStatus.State != StateEnabled {
		return "", &SigningRefusedError{State: currentStatus.State, Reason: currentStatus.PauseReason}
	}
	if !emailOnVerifiedDomain(claims.Email, currentStatus.VerifiedDomains) {
		return "", fmt.Errorf("%w: email is not on a verified domain of the organization", ErrInvalidClaims)
	}
	// A disable or revocation that committed while KMS was signing wins.
	current, err := q.GetOrganizationInternalSessionIssuer(ctx, organizationID)
	if err != nil {
		return "", fmt.Errorf("recheck issuer after signing: %w", err)
	}
	if current.DisabledAt.Valid {
		return "", &SigningRefusedError{State: StateDisabled, Reason: ""}
	}
	live, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: issuer.JsonWebKeySetID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return "", fmt.Errorf("recheck issuer key after signing: %w", err)
	}
	if !slices.ContainsFunc(live, func(k repo.JsonWebKey) bool { return k.ID == key.ID }) {
		return "", &SigningRefusedError{State: StateKeyless, Reason: ""}
	}
	logger.InfoContext(ctx, "issued internal session issuer assertion",
		attr.SlogIssuedAssertionID(claims.ID), attr.SlogIssuedAssertionAudience(claims.Audience), attr.SlogOAuthClientID(claims.ClientID), attr.SlogIssuedAssertionKeyID(key.Kid))
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
	parsed, err := jose.ParseSigned(assertion, []jose.SignatureAlgorithm{alg})
	if err != nil {
		return "", fmt.Errorf("parse signed assertion: %w", err)
	}
	if _, err := parsed.Verify(publicJWK.Key); err != nil {
		return "", fmt.Errorf("verify assertion against published key: %w", err)
	}
	return assertion, nil
}
