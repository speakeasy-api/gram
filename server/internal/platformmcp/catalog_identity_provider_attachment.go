//nolint:exhaustruct // OAuth persistence values intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	oauthregistration "github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	ErrIdentityProviderAttachmentUnavailable = errors.New("platform mcp identity provider attachment unavailable")
	ErrIdentityProviderAttachmentUnsupported = errors.New("platform mcp identity provider attachment unsupported")
	ErrIdentityProviderAttachmentConflict    = errors.New("platform mcp identity provider attachment conflict")
)

const browserCatalogDCRAuthMethod = string(remotesessions.TokenEndpointAuthMethodBasic)

// CatalogIdentityProviderAttachmentResult contains only non-secret provider
// context for the agent. Provider URLs are safe to return; client secrets,
// tokens, passwords, and OAuth codes are never represented here.
type CatalogIdentityProviderAttachmentResult struct {
	Attached    bool
	ProviderURL string
}

// CatalogIdentityProviderAttachment attaches the one OAuth provider advertised
// by a persisted reviewed Remote MCP source. It is a server-owned boundary:
// neither the tool caller nor the browser supplies a client id, client secret,
// OAuth code, token, or other credential.
type CatalogIdentityProviderAttachment interface {
	Attach(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogIdentityProviderAttachmentResult, error)
}

type CatalogIdentityProviderAttachmentService struct {
	db                    *pgxpool.Pool
	identity              *remotesessions.IdentityCommitter
	policy                *guardian.Policy
	serverURL             *url.URL
	registrationTelemetry oauthregistration.Recorder
}

func NewCatalogIdentityProviderAttachmentService(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, identity *remotesessions.IdentityCommitter, policy *guardian.Policy, serverURL *url.URL) *CatalogIdentityProviderAttachmentService {
	if serverURL == nil {
		return &CatalogIdentityProviderAttachmentService{}
	}
	serverURLCopy := *serverURL
	return &CatalogIdentityProviderAttachmentService{db: db, identity: identity, policy: policy, serverURL: &serverURLCopy, registrationTelemetry: oauthregistration.NewMetrics(logger, meterProvider)}
}

// Attach discovers the exact provider advertised by the lifecycle-owned Remote
// MCP source, creates a project-owned remote-session issuer/client when needed,
// and binds it to the registration's existing user-session issuer. It is safe
// to retry after a successful call: the existing matching binding is reused.
func (s *CatalogIdentityProviderAttachmentService) Attach(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	if s == nil || s.db == nil || s.identity == nil || s.policy == nil || s.serverURL == nil || principal.UserID == "" || principal.OrganizationID == "" || project.ID == uuid.Nil || registrationID == uuid.Nil {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnavailable
	}

	// A dynamic registration may not have a portable delete API. Serialize the
	// exact user/project/registration operation across server processes so two
	// simultaneous confirmations cannot register duplicate upstream clients.
	lockTx, err := s.db.Begin(ctx)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("begin identity-provider attachment lock: %w", err)
	}
	defer func() { _ = lockTx.Rollback(ctx) }()
	lockQ := platformrepo.New(lockTx)
	if err := lockQ.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID.String(),
		Operation:      "attach_identity_provider",
		IdempotencyKey: registrationID.String(),
	}); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("lock identity-provider attachment: %w", err)
	}

	result, err := s.attachLocked(ctx, lockQ, principal, project, registrationID)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	if err := lockTx.Commit(ctx); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("commit identity-provider attachment lock: %w", err)
	}
	return result, nil
}

func (s *CatalogIdentityProviderAttachmentService) attachLocked(ctx context.Context, lockQ *platformrepo.Queries, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("load platform mcp identity-provider registration: %w", err)
	}
	if (!isBrowserCatalogProviderKey(registration.CatalogProvider) && registration.CatalogProvider != directRemoteProviderKey) || registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnsupported
	}

	remote, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: registration.RemoteMcpServerID.UUID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("load registered Remote MCP source: %w", err)
	}
	if remote.TransportType != "streamable-http" || remote.Url == "" {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnsupported
	}

	resourceMetadata, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, s.policy, remote.Url)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("discover registered MCP identity provider: %w: %w", ErrIdentityProviderAttachmentUnsupported, err)
	}
	metadata, err := s.discoverSupportedIssuerMetadata(ctx, resourceMetadata.AuthorizationServers)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	if err := lockQ.LockPlatformMCPRemoteIssuerAttachment(ctx, platformrepo.LockPlatformMCPRemoteIssuerAttachmentParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
		Issuer:         metadata.Issuer,
	}); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("lock identity-provider issuer attachment: %w", err)
	}

	if attached, err := s.matchingAttachment(ctx, principal.OrganizationID, project, registration.UserSessionIssuerID.UUID, metadata.Issuer); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	} else if attached {
		return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: metadata.Issuer}, nil
	}

	// Resolved before registering so a provider this flow cannot serve costs no
	// upstream client: a dynamic registration may have no portable delete API,
	// so one made and then abandoned is a client left behind at the provider.
	existing, reuse, err := s.reusableIssuer(ctx, principal, project, metadata.Issuer)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}

	provider := remotesessions.CreateProvider(discoveredIssuerParams(principal, project, registrationID, metadata))
	if reuse {
		provider = remotesessions.UseProvider(existing.ID)
	}
	commit := s.identity.Prepare(remotesessions.IdentityPlan{
		Scope: remotesessions.IdentityScope{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        project.ID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
		},
		UserSessionIssuerID: registration.UserSessionIssuerID.UUID,
		Provider:            provider,
		Client: remotesessions.RegisterClient(remotesessions.RegistrationPolicy{
			Scope:                   append([]string(nil), resourceMetadata.ScopesSupported...),
			Audience:                nil,
			TokenEndpointAuthMethod: optionalString(browserCatalogDCRAuthMethod),
			RequireClientSecret:     true,
			AllowCIMD:               false,
		}),
		Bound:           remotesessions.ReuseBound,
		ResourceDisplay: &remotesessions.ResourceDisplay{ResourceURL: remote.Url, Metadata: resourceMetadata},
	})
	if err := commit.Preflight(ctx); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, attachmentCommitError("check identity-provider attachment", err)
	}
	reg, err := commit.Register(ctx)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, attachmentCommitError("register identity-provider client", err)
	}
	if !reg.Ready() {
		return CatalogIdentityProviderAttachmentResult{}, identityProviderRegistrationError(reg)
	}

	tx, err := commit.Begin(ctx)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("begin identity-provider attachment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := commit.Lock(ctx, tx); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, attachmentCommitError("lock identity-provider attachment", err)
	}
	if err := commit.Bind(ctx, tx, reg); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, attachmentCommitError("bind identity-provider client", err)
	}
	if _, err := commit.Commit(ctx, tx); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, attachmentCommitError("commit identity-provider attachment", err)
	}
	return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: metadata.Issuer}, nil
}

func (s *CatalogIdentityProviderAttachmentService) discoverSupportedIssuerMetadata(ctx context.Context, authorizationServers []string) (remotesessions.DiscoveredIssuerMetadata, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Resource metadata contains issuer identifiers, not operator input.
	// Do not normalize advertised authorization_servers before discovery.
	for _, authorizationServer := range authorizationServers {
		if strings.TrimSpace(authorizationServer) == "" {
			continue
		}
		metadata, err := remotesessions.DiscoverIssuerMetadata(probeCtx, s.policy, authorizationServer)
		if err != nil || strings.TrimSpace(metadata.Issuer) == "" || !sameIssuerURL(metadata.Issuer, authorizationServer) || strings.TrimSpace(metadata.AuthorizationEndpoint) == "" || strings.TrimSpace(metadata.TokenEndpoint) == "" || !validDynamicClientRegistrationEndpoint(metadata.RegistrationEndpoint) {
			continue
		}
		return metadata, nil
	}
	return remotesessions.DiscoveredIssuerMetadata{}, ErrIdentityProviderAttachmentUnsupported
}

func (s *CatalogIdentityProviderAttachmentService) matchingAttachment(ctx context.Context, organizationID string, project ResolvedProject, userSessionIssuerID uuid.UUID, issuerURL string) (bool, error) {
	clients, err := remotesessionsrepo.New(s.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(project.ID),
		OrganizationID:      conv.ToPGText(organizationID),
	})
	if err != nil {
		return false, fmt.Errorf("list registered identity providers: %w", err)
	}
	if len(clients) == 0 {
		return false, nil
	}
	if len(clients) != 1 || !sameIssuerURL(clients[0].IssuerUrl, issuerURL) {
		return false, ErrIdentityProviderAttachmentConflict
	}
	return true, nil
}

// reusableIssuer resolves the project-owned issuer this attachment should reuse
// for issuerURL, reporting false when none exists and the caller must create one.
//
// A stored issuer bound to a tunneled MCP server is refused rather than reused.
// Everything this flow does reaches the provider over Gram's direct egress —
// the metadata discovery above and the dynamic client registration below — but
// once the client hangs off a tunnel-bound issuer, its refreshes and
// revocations go out over the tunnel instead. An issuer is bound precisely
// because it is unreachable from the cloud, so that client would be registered
// against an authorization server the rest of its life cannot reach. A private
// provider is set up from the issuer settings page, where the registration
// takes the same route the binding names.
func (s *CatalogIdentityProviderAttachmentService) reusableIssuer(ctx context.Context, principal Principal, project ResolvedProject, issuerURL string) (remotesessionsrepo.RemoteSessionIssuer, bool, error) {
	var none remotesessionsrepo.RemoteSessionIssuer
	issuers, err := remotesessionsrepo.New(s.db).ListRemoteSessionIssuersByIssuerURL(ctx, remotesessionsrepo.ListRemoteSessionIssuersByIssuerURLParams{
		Issuers:               []string{issuerURL},
		ProjectID:             conv.ToNullUUID(project.ID),
		IncludeOrganizational: false,
		OrganizationID:        conv.ToPGText(principal.OrganizationID),
		IncludeGlobal:         false,
	})
	if err != nil {
		return none, false, fmt.Errorf("find registered identity provider: %w", err)
	}
	if len(issuers) > 1 {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	if len(issuers) == 0 {
		return none, false, nil
	}
	issuer := issuers[0]
	if !issuer.ProjectID.Valid || issuer.ProjectID.UUID != project.ID || !issuer.OrganizationID.Valid || issuer.OrganizationID.String != principal.OrganizationID || !issuer.AuthorizationEndpoint.Valid || issuer.AuthorizationEndpoint.String == "" || !issuer.TokenEndpoint.Valid || issuer.TokenEndpoint.String == "" || !issuer.RegistrationEndpoint.Valid || issuer.RegistrationEndpoint.String == "" {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	if issuer.TunneledMcpServerID.Valid {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	return issuer, true, nil
}

// attachmentCommitError reports a refused identity write as an attachment
// conflict and wraps anything else with what failed.
func attachmentCommitError(what string, err error) error {
	if errors.Is(err, remotesessions.ErrIdentityConflict) || errors.Is(err, remotesessions.ErrIdentityInvariant) || errors.Is(err, remotesessions.ErrIdentityOrgWideBinding) {
		return fmt.Errorf("%s: %w: %w", what, ErrIdentityProviderAttachmentConflict, err)
	}
	if errors.Is(err, remotesessions.ErrIdentityInvalid) {
		return fmt.Errorf("%s: %w: %w", what, ErrIdentityProviderAttachmentUnsupported, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// discoveredIssuerParams describes a new project-owned issuer for a discovered
// authorization server. It carries authorization-server data only; the
// resource's display members live on the client.
func discoveredIssuerParams(principal Principal, project ResolvedProject, registrationID uuid.UUID, metadata remotesessions.DiscoveredIssuerMetadata) remotesessionsrepo.CreateRemoteSessionIssuerParams {
	return remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(project.ID),
		OrganizationID:                    conv.ToPGText(principal.OrganizationID),
		Slug:                              attachmentIssuerSlug(registrationID),
		Issuer:                            metadata.Issuer,
		Name:                              conv.ToPGText("Remote identity provider"),
		LogoAssetID:                       uuid.NullUUID{},
		ClientSetupDocumentationUrl:       pgtype.Text{},
		AuthorizationEndpoint:             conv.ToPGText(metadata.AuthorizationEndpoint),
		TokenEndpoint:                     conv.ToPGText(metadata.TokenEndpoint),
		RegistrationEndpoint:              conv.ToPGText(metadata.RegistrationEndpoint),
		JwksUri:                           pgtype.Text{},
		ServiceDocumentation:              pgtype.Text{},
		OpPolicyUri:                       pgtype.Text{},
		OpTosUri:                          pgtype.Text{},
		ScopesSupported:                   append([]string(nil), metadata.ScopesSupported...),
		GrantTypesSupported:               append([]string(nil), metadata.GrantTypesSupported...),
		AuthorizationGrantProfilesSupported: slices.Clone(metadata.AuthorizationGrantProfilesSupported),
		ResponseTypesSupported:            append([]string(nil), metadata.ResponseTypesSupported...),
		TokenEndpointAuthMethodsSupported: append([]string(nil), metadata.TokenEndpointAuthMethodsSupported...),
		// An empty advertised list must survive as empty here: discovery ran,
		// so the nullable column should record "advertises no methods" ({})
		// rather than "not captured" (NULL). The plain append copy used by the
		// sibling fields collapses an empty slice to nil, so this field uses
		// slices.Clone, which preserves emptiness — and
		// DiscoveredIssuerMetadata guarantees the field non-nil.
		CodeChallengeMethodsSupported:     slices.Clone(metadata.CodeChallengeMethodsSupported),
		ClientIDMetadataDocumentSupported: metadata.ClientIDMetadataDocumentSupported,
		Oidc:                              false,
		Passthrough:                       false,
		TunneledMcpServerID:               uuid.NullUUID{},
		// Discovery ran, so the enrichment capabilities are captured here
		// rather than left NULL for a later refresh.
		UserinfoEndpoint:                           conv.ToPGTextEmpty(metadata.UserinfoEndpoint),
		IntrospectionEndpoint:                      conv.ToPGTextEmpty(metadata.IntrospectionEndpoint),
		IntrospectionEndpointAuthMethodsSupported:  metadata.IntrospectionEndpointAuthMethodsSupported,
		IDTokenSigningAlgValuesSupported:           metadata.IDTokenSigningAlgValuesSupported,
		ClaimsSupported:                            metadata.ClaimsSupported,
		BackchannelLogoutSupported:                 pgtype.Bool{Bool: metadata.BackchannelLogoutSupported, Valid: true},
		AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: metadata.AuthorizationResponseIssParameterSupported, Valid: true},
		ScopeOverride:                              nil,
		ResourceIndicatorSupported:                 pgtype.Bool{Bool: false, Valid: false},
		Metadata:                                   metadata.Metadata,
		MetadataFetchedAt:                          pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		MetadataLastError:                          metadata.UnreadableMessage,
		MetadataLastErrorUrl:                       metadata.UnreadableURL,
	}
}

// identityProviderRegistrationError preserves the important distinction
// between a provider rejecting Gram's fixed registration contract (which cannot
// succeed unchanged) and a temporary upstream/transport failure (which can be
// retried). It intentionally does not carry an upstream response detail into
// the MCP tool result or logs.
func identityProviderRegistrationError(reg remotesessions.Registration) error {
	if reg.Failure == nil || reg.Failure.Outcome == oauthregistration.OutcomeRefused {
		return fmt.Errorf("register identity-provider client: %w", ErrIdentityProviderAttachmentUnsupported)
	}
	return fmt.Errorf("register identity-provider client: %w", ErrIdentityProviderAttachmentUnavailable)
}

func attachmentIssuerSlug(registrationID uuid.UUID) string {
	sum := sha256.Sum256([]byte(registrationID.String()))
	return "platform-mcp-auto-" + hex.EncodeToString(sum[:8])
}

func sameIssuerURL(a, b string) bool {
	return a == b
}

func validDynamicClientRegistrationEndpoint(raw string) bool {
	endpoint, err := url.Parse(raw)
	return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
