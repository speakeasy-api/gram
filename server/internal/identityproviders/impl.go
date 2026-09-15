package identityproviders

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/identity_providers/server"
	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	setupStepConnect                       = "connect"
	setupValueClientID                     = "client_id"
	identityProviderJSONWebKeySetMaxAgeSec = 3600
	identityProviderSigningKeyBits         = 2048
	oktaAPIScopes                          = "okta.apps.read okta.groups.read okta.users.read okta.apps.manage"
	oktaAdministratorRoles                 = "Read-only Administrator, Application Administrator"
)

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       *auth.Auth
	authz      *authz.Engine
	audit      *audit.Logger
	encryption *encryption.Client
	serverURL  *url.URL
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	encryptionClient *encryption.Client,
	serverURL *url.URL,
) *Service {
	logger = logger.With(attr.SlogComponent("identity_providers"))
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/identityproviders"),
		logger:     logger,
		db:         db,
		auth:       auth.New(logger, db, sessionManager, authzEngine),
		authz:      authzEngine,
		audit:      auditLogger,
		encryption: encryptionClient,
		serverURL:  serverURL,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
	o11y.AttachHandler(
		mux,
		http.MethodGet,
		"/.well-known/identity-provider/{connection_id}/jwks.json",
		oops.ErrHandle(service.logger, service.HandleJSONWebKeySet).ServeHTTP,
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.IdentityProviderConnection, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if payload.Kind != "okta" {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported identity provider kind").LogError(ctx, logger)
	}

	tenantIdentifier, err := normalizeTenantURL(payload.TenantURL)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "tenant_url must be an HTTPS tenant origin").LogError(ctx, logger)
	}
	if _, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID); err == nil {
		return nil, oops.E(oops.CodeConflict, nil, "an identity provider connection already exists for this organization").LogError(ctx, logger)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "error checking for an existing identity provider connection").LogError(ctx, logger)
	}

	minted, err := s.generateSigningKey()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating identity provider signing key").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating identity provider connection").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	connection, err := queries.CreateIdentityProviderConnection(ctx, repo.CreateIdentityProviderConnectionParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Kind:             payload.Kind,
		TenantIdentifier: tenantIdentifier,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "an identity provider connection already exists for this organization").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error creating identity provider connection").LogError(ctx, logger)
	}

	signingKey, err := queries.CreateIdentityProviderSigningKey(ctx, repo.CreateIdentityProviderSigningKeyParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: connection.ID,
		Kid:                          minted.kid,
		Algorithm:                    string(jose.RS256),
		PublicJwk:                    minted.publicJWK,
		PrivateKeyEncrypted:          minted.encryptedPrivateKey,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider signing key").LogError(ctx, logger)
	}

	if _, err := queries.CreateOktaIdentityProviderConnection(ctx, repo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connection.ID,
		OktaDomain:                   tenantIdentifier,
		SigningKeyID:                 uuid.NullUUID{UUID: signingKey.ID, Valid: true},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving Okta identity provider connection").LogError(ctx, logger)
	}

	if err := s.audit.LogIdentityProviderConnectionCreated(ctx, dbtx, audit.LogIdentityProviderConnectionCreatedEvent{
		OrganizationID:                authCtx.ActiveOrganizationID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:              authCtx.Email,
		ActorSlug:                     nil,
		IdentityProviderConnectionURN: urn.NewIdentityProviderConnectionID(connection.ID),
		TenantIdentifier:              connection.TenantIdentifier,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording identity provider connection creation").LogError(ctx, logger)
	}

	row, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading created identity provider connection").LogError(ctx, logger)
	}
	view, err := mv.BuildIdentityProviderConnectionView(row, IdentityProviderJSONWebKeySetURL(s.serverURL, row.ID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider connection response").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider connection").LogError(ctx, logger)
	}
	return view, nil
}

func (s *Service) Get(ctx context.Context, _ *gen.GetPayload) (*gen.GetIdentityProviderResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	row, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &gen.GetIdentityProviderResult{Connection: nil}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	view, err := mv.BuildIdentityProviderConnectionView(row, IdentityProviderJSONWebKeySetURL(s.serverURL, row.ID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider connection response").LogError(ctx, logger)
	}
	return &gen.GetIdentityProviderResult{Connection: view}, nil
}

func (s *Service) DescribeSetup(ctx context.Context, _ *gen.DescribeSetupPayload) (*gen.IdentityProviderSetup, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	row, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider setup").LogError(ctx, logger)
	}
	return &gen.IdentityProviderSetup{
		ConnectionID: row.ID.String(),
		Steps:        []*gen.IdentityProviderSetupStep{buildConnectSetupStep(row.TenantIdentifier, IdentityProviderJSONWebKeySetURL(s.serverURL, row.ID), row.Status)},
	}, nil
}

func (s *Service) SubmitSetupStep(ctx context.Context, payload *gen.SubmitSetupStepPayload) (*gen.SubmitSetupStepResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if payload.StepKey != setupStepConnect {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown identity provider setup step").LogError(ctx, logger)
	}
	if len(payload.Values) != 1 || payload.Values[0] == nil || payload.Values[0].Key != setupValueClientID {
		return nil, oops.E(oops.CodeBadRequest, nil, "connect requires exactly one client_id value").LogError(ctx, logger)
	}
	clientID := strings.TrimSpace(payload.Values[0].Value)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id must not be empty").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating identity provider connection").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	if err := queries.UpdateOktaIdentityProviderClientID(ctx, repo.UpdateOktaIdentityProviderClientIDParams{
		ClientID:                     conv.ToPGText(clientID),
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving Okta client id").LogError(ctx, logger)
	}
	if err := queries.MarkIdentityProviderAwaitingVerification(ctx, repo.MarkIdentityProviderAwaitingVerificationParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating identity provider connection status").LogError(ctx, logger)
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading updated identity provider connection").LogError(ctx, logger)
	}

	if err := s.audit.LogIdentityProviderConnectionUpdated(ctx, dbtx, audit.LogIdentityProviderConnectionUpdatedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording identity provider connection update").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider connection update").LogError(ctx, logger)
	}

	return &gen.SubmitSetupStepResult{
		Step: buildConnectSetupStep(after.TenantIdentifier, IdentityProviderJSONWebKeySetURL(s.serverURL, after.ID), after.Status),
		FieldOutcomes: []*gen.IdentityProviderFieldOutcome{{
			Key:     setupValueClientID,
			Outcome: "accepted",
			Detail:  "Client ID saved.",
		}},
		NextStepKey: nil,
	}, nil
}

func (s *Service) VerifySetupStep(ctx context.Context, _ *gen.VerifySetupStepPayload) (*gen.IdentityProviderVerifyResult, error) {
	if _, _, err := s.requireAccess(ctx, authz.ScopeOrgAdmin); err != nil {
		return nil, err
	}

	// Slice B replaces this placeholder with the Okta capability probe.
	return nil, oops.E(oops.CodeUnavailable, nil, "verification arrives in slice B")
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return err
	}
	connectionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid identity provider connection id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting identity provider connection").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	row, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && row.ID != connectionID:
		return oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}

	if err := queries.SoftDeleteIdentityProviderSigningKeys(ctx, repo.SoftDeleteIdentityProviderSigningKeysParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: row.ID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting identity provider signing keys").LogError(ctx, logger)
	}
	if err := queries.SoftDeleteIdentityProviderConnection(ctx, repo.SoftDeleteIdentityProviderConnectionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             row.ID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting identity provider connection").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionDeleted(ctx, dbtx, audit.LogIdentityProviderConnectionDeletedEvent{
		OrganizationID:                authCtx.ActiveOrganizationID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:              authCtx.Email,
		ActorSlug:                     nil,
		IdentityProviderConnectionURN: urn.NewIdentityProviderConnectionID(row.ID),
		TenantIdentifier:              row.TenantIdentifier,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error recording identity provider connection deletion").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving identity provider connection deletion").LogError(ctx, logger)
	}
	return nil
}

// HandleJSONWebKeySet serves every live public signing key for a connection.
func (s *Service) HandleJSONWebKeySet(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "identity provider JSON Web Key Set not found")
	}
	connectionID, err := uuid.Parse(chi.URLParam(r, "connection_id"))
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "identity provider JSON Web Key Set not found")
	}
	body, err := repo.New(s.db).GetIdentityProviderJSONWebKeySet(ctx, connectionID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "identity provider JSON Web Key Set not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load identity provider JSON Web Key Set").LogError(ctx, s.logger)
	}
	return httpcache.WriteCacheableJSON(ctx, w, r, s.logger, "application/jwk-set+json", identityProviderJSONWebKeySetMaxAgeSec, body)
}

func IdentityProviderJSONWebKeySetURL(serverURL *url.URL, connectionID uuid.UUID) string {
	return strings.TrimRight(serverURL.String(), "/") + "/.well-known/identity-provider/" + connectionID.String() + "/jwks.json"
}

func (s *Service) requireAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}
	return authCtx, logger, nil
}

type generatedSigningKey struct {
	kid                 string
	publicJWK           []byte
	encryptedPrivateKey string
}

func (s *Service) generateSigningKey() (*generatedSigningKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, identityProviderSigningKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA private key: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal PKCS#8 private key: %w", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: nil, Bytes: privateKeyDER})
	encryptedPrivateKey, err := s.encryption.Encrypt(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	jwk := jose.JSONWebKey{
		Key:                         &privateKey.PublicKey,
		KeyID:                       "",
		Algorithm:                   string(jose.RS256),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	}
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("derive public key thumbprint: %w", err)
	}
	jwk.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)
	publicJWK, err := jwk.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal public JWK: %w", err)
	}
	return &generatedSigningKey{kid: jwk.KeyID, publicJWK: publicJWK, encryptedPrivateKey: encryptedPrivateKey}, nil
}

func normalizeTenantURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse tenant URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("tenant URL must contain only an HTTPS origin")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(host) != nil || !validDNSName(host) {
		return "", errors.New("tenant URL must contain a valid DNS hostname")
	}
	return host, nil
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func buildConnectSetupStep(tenantIdentifier, jwksURL, status string) *gen.IdentityProviderSetupStep {
	return &gen.IdentityProviderSetupStep{
		Key:   setupStepConnect,
		Title: "Connect Okta",
		Where: "their_console",
		Instructions: []string{
			"Create an API Services app integration in the Okta Admin Console.",
			"Use the JWKS URL for Public key / Private key client authentication.",
			"Grant the API scopes and administrator roles shown below, then enter the app's Client ID in Speakeasy.",
		},
		DeepLink: oktaAdminAppsURL(tenantIdentifier),
		PrintedValues: []*gen.IdentityProviderPrintedValue{
			{Label: "JWKS URL", Value: jwksURL, Copyable: true},
			{Label: "API scopes", Value: oktaAPIScopes, Copyable: true},
			{Label: "Administrator roles", Value: oktaAdministratorRoles, Copyable: false},
		},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{{Key: setupValueClientID, Label: "Client ID", Secret: false}},
		State:          setupStepState(status),
		LastOutcome:    nil,
	}
}

func setupStepState(status string) string {
	switch status {
	case "pending":
		return "awaiting_values"
	case "awaiting_verification":
		return "awaiting_verification"
	case "active":
		return "passed"
	case "failed":
		return "failed"
	default:
		return "not_started"
	}
}

func oktaAdminAppsURL(tenantIdentifier string) *string {
	for _, family := range []string{"okta.com", "oktapreview.com", "okta-emea.com"} {
		suffix := "." + family
		if subdomain, ok := strings.CutSuffix(tenantIdentifier, suffix); ok && subdomain != "" && !strings.Contains(subdomain, ".") {
			return conv.PtrEmpty("https://" + subdomain + "-admin." + family + "/admin/apps/active")
		}
	}
	return nil
}

func identityProviderConnectionSnapshot(row repo.GetIdentityProviderConnectionByOrganizationRow) *audit.IdentityProviderConnectionSnapshot {
	return &audit.IdentityProviderConnectionSnapshot{
		Kind:             row.Kind,
		TenantIdentifier: row.TenantIdentifier,
		Status:           row.Status,
		Capabilities:     row.Capabilities,
		GrantedScopes:    row.GrantedScopes,
	}
}
