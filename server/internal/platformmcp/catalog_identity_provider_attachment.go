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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	oauthregistration "github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
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

	if attachedClient, attached, err := s.matchingAttachment(ctx, s.db, principal.OrganizationID, project, registration.UserSessionIssuerID.UUID, metadata.Issuer); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	} else if attached {
		if err := s.associateExistingAttachment(ctx, principal, project, registration, metadata.Issuer, attachedClient); err != nil {
			return CatalogIdentityProviderAttachmentResult{}, err
		}
		return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: metadata.Issuer}, nil
	}

	// Checked before registering, which may create an upstream client with no
	// delete path, and again under the server lock before binding it.
	current, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("load registered mcp server: %w", err)
	}
	if current.RemoteMcpServerID != registration.RemoteMcpServerID || current.UserSessionIssuerID != registration.UserSessionIssuerID {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentConflict
	}

	// Resolved before registering so a provider this flow cannot serve costs no
	// upstream client: a dynamic registration may have no portable delete API,
	// so one made and then abandoned is a client left behind at the provider.
	existing, reuse, err := s.reusableIssuer(ctx, principal, project, metadata.Issuer)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}

	// The registration names what the first login will request: the row's pin
	// and challenge scopes, else the list this attach just read.
	registrationResource, _, err := remotesessions.CachedResourceScopes(ctx, s.db, project.ID, remote.Url, true)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("read protected resource for attachment: %w", err)
	}
	registrationResource.ScopesSupported = append([]string(nil), resourceMetadata.ScopesSupported...)
	registrationResource.Live = true
	commit := s.identity.Prepare(attachmentIdentityPlan(principal, project, registrationID, registration.UserSessionIssuerID.UUID, registration.McpServerID.UUID, metadata, existing, reuse, remote.Url, resourceMetadata, registrationResource))
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
	// The server row is locked ahead of the issuer, the order MCP server
	// updates take, because the commit associates the server with its client.
	locked, err := mcpserversrepo.New(tx.DB()).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("lock registered mcp server: %w", err)
	}
	// Discovery and registration ran unlocked, so the server must still
	// front the same source through the same issuer they were decided for.
	if locked.RemoteMcpServerID != registration.RemoteMcpServerID || locked.UserSessionIssuerID != registration.UserSessionIssuerID {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentConflict
	}
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
		if err != nil || strings.TrimSpace(metadata.Issuer) == "" || !sameIssuerURL(metadata.Issuer, authorizationServer) || strings.TrimSpace(metadata.AuthorizationEndpoint) == "" || strings.TrimSpace(metadata.TokenEndpoint) == "" || !supportsAutomaticClientRegistration(metadata.RegistrationEndpoint, metadata.ClientIDMetadataDocumentSupported, metadata.TokenEndpointAuthMethodsSupported) {
			continue
		}
		return metadata, nil
	}
	return remotesessions.DiscoveredIssuerMetadata{}, ErrIdentityProviderAttachmentUnsupported
}

// associateExistingAttachment records the server's association with the
// client its issuer already binds, for an attachment made before associations
// were recorded or a retry after a partial one. The binding was read unlocked,
// so under the server lock the server must still front the registered source
// through the registered issuer, and that issuer must still bind the same
// client, before the association is written in the same transaction. The
// client's remote session issuer is locked after the server, the order
// binding writers take, so no binding change commits between the reread and
// the write.
func (s *CatalogIdentityProviderAttachmentService) associateExistingAttachment(ctx context.Context, principal Principal, project ResolvedProject, registration platformrepo.PlatformMcpCatalogRegistration, issuerURL string, client remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin identity-provider association: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRegistrationInvalid
	}
	if err != nil {
		return fmt.Errorf("lock registered mcp server: %w", err)
	}
	if locked.RemoteMcpServerID != registration.RemoteMcpServerID || locked.UserSessionIssuerID != registration.UserSessionIssuerID {
		return ErrIdentityProviderAttachmentConflict
	}
	if err := remotesessionsrepo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, client.RemoteSessionIssuerID); err != nil {
		return fmt.Errorf("lock identity-provider client binding: %w", err)
	}
	current, attached, err := s.matchingAttachment(ctx, tx, principal.OrganizationID, project, registration.UserSessionIssuerID.UUID, issuerURL)
	if err != nil {
		return err
	}
	if !attached || current.ClientID != client.ClientID {
		return ErrIdentityProviderAttachmentConflict
	}
	associated, err := upstreamclientsrepo.New(tx).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        principal.OrganizationID,
		RemoteSessionClientID: client.ClientID,
		McpServerID:           registration.McpServerID.UUID,
		ProjectID:             project.ID,
	})
	if err != nil {
		return fmt.Errorf("associate registered mcp server with its client: %w", err)
	}
	if associated == 0 {
		return ErrRegistrationInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit identity-provider association: %w", err)
	}
	return nil
}

// matchingAttachment returns the client the user session issuer already binds
// for issuerURL, reporting false when it binds none. Any other binding is a
// conflict this flow does not resolve.
func (s *CatalogIdentityProviderAttachmentService) matchingAttachment(ctx context.Context, db remotesessionsrepo.DBTX, organizationID string, project ResolvedProject, userSessionIssuerID uuid.UUID, issuerURL string) (remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow, bool, error) {
	var none remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow
	clients, err := remotesessionsrepo.New(db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(project.ID),
		OrganizationID:      conv.ToPGText(organizationID),
		OrganizationWide:    false,
	})
	if err != nil {
		return none, false, fmt.Errorf("list registered identity providers: %w", err)
	}
	if len(clients) == 0 {
		return none, false, nil
	}
	if len(clients) != 1 || !sameIssuerURL(clients[0].IssuerUrl, issuerURL) {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	return clients[0], true, nil
}

// reusableIssuer resolves the project-owned issuer this attachment should reuse
// for issuerURL, reporting false when none exists and the caller must create one.
//
// A stored issuer bound to a tunneled MCP server is refused rather than reused.
// Everything this flow does reaches the provider over Speakeasy's direct egress —
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
	if !issuer.ProjectID.Valid || issuer.ProjectID.UUID != project.ID || !issuer.OrganizationID.Valid || issuer.OrganizationID.String != principal.OrganizationID || !issuer.AuthorizationEndpoint.Valid || issuer.AuthorizationEndpoint.String == "" || !issuer.TokenEndpoint.Valid || issuer.TokenEndpoint.String == "" || !supportsAutomaticClientRegistration(issuer.RegistrationEndpoint.String, issuer.ClientIDMetadataDocumentSupported, issuer.TokenEndpointAuthMethodsSupported) {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	if issuer.TunneledMcpServerID.Valid {
		return none, false, ErrIdentityProviderAttachmentConflict
	}
	return issuer, true, nil
}

// attachmentIdentityPlan describes the attachment's identity write: the
// discovered provider (or the reusable stored one) and a client this flow
// registers itself. Dynamic registration stays the path for a provider where
// this flow can use it, so existing attachments are unchanged; otherwise a
// provider is set up through a Client ID Metadata Document when it supports
// one, the same path the dashboard's automatic setup takes. A provider offering
// neither leaves the registration needing manual setup.
func attachmentIdentityPlan(principal Principal, project ResolvedProject, registrationID, userSessionIssuerID, mcpServerID uuid.UUID, metadata remotesessions.DiscoveredIssuerMetadata, existing remotesessionsrepo.RemoteSessionIssuer, reuse bool, resourceURL string, resourceMetadata wellknown.OAuthProtectedResourceMetadata, registrationResource remotesessions.ResourceScopes) remotesessions.IdentityPlan {
	var provider remotesessions.ProviderChoice
	var allowCIMD bool
	if reuse {
		provider = remotesessions.UseProvider(existing.ID)
		allowCIMD = !attachmentCanUseDynamicRegistration(existing.RegistrationEndpoint.String, existing.TokenEndpointAuthMethodsSupported)
	} else {
		provider = remotesessions.CreateProvider(discoveredIssuerParams(principal, project, registrationID, metadata))
		allowCIMD = !attachmentCanUseDynamicRegistration(metadata.RegistrationEndpoint, metadata.TokenEndpointAuthMethodsSupported)
	}
	return remotesessions.IdentityPlan{
		Scope: remotesessions.IdentityScope{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        project.ID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
		},
		UserSessionIssuerID: userSessionIssuerID,
		MCPServerID:         mcpServerID,
		Provider:            provider,
		Client: remotesessions.RegisterClient(remotesessions.RegistrationPolicy{
			// No copied scope: login discovers what to request live, and the
			// registration names what that login would request.
			Scope:                   nil,
			RegistrationResource:    &registrationResource,
			Audience:                nil,
			TokenEndpointAuthMethod: optionalString(browserCatalogDCRAuthMethod),
			RequireClientSecret:     true,
			AllowCIMD:               allowCIMD,
		}),
		Bound:           remotesessions.ReuseBound,
		ResourceDisplay: &remotesessions.ResourceDisplay{ResourceURL: resourceURL, Metadata: resourceMetadata},
	}
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
		ProjectID:                           conv.ToNullUUID(project.ID),
		OrganizationID:                      conv.ToPGText(principal.OrganizationID),
		Slug:                                attachmentIssuerSlug(registrationID),
		Issuer:                              metadata.Issuer,
		Name:                                conv.ToPGText("Remote identity provider"),
		LogoAssetID:                         uuid.NullUUID{},
		ClientSetupDocumentationUrl:         pgtype.Text{},
		AuthorizationEndpoint:               conv.ToPGText(metadata.AuthorizationEndpoint),
		TokenEndpoint:                       conv.ToPGText(metadata.TokenEndpoint),
		RegistrationEndpoint:                conv.ToPGTextEmpty(metadata.RegistrationEndpoint),
		JwksUri:                             pgtype.Text{},
		ServiceDocumentation:                pgtype.Text{},
		OpPolicyUri:                         pgtype.Text{},
		OpTosUri:                            pgtype.Text{},
		ScopesSupported:                     append([]string(nil), metadata.ScopesSupported...),
		GrantTypesSupported:                 append([]string(nil), metadata.GrantTypesSupported...),
		AuthorizationGrantProfilesSupported: slices.Clone(metadata.AuthorizationGrantProfilesSupported),
		ResponseTypesSupported:              append([]string(nil), metadata.ResponseTypesSupported...),
		TokenEndpointAuthMethodsSupported:   append([]string(nil), metadata.TokenEndpointAuthMethodsSupported...),
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
		OmitScopeFallback:                          pgtype.Bool{Bool: false, Valid: false},
		ResourceIndicatorSupported:                 pgtype.Bool{Bool: false, Valid: false},
		Metadata:                                   metadata.Metadata,
		MetadataFetchedAt:                          pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		MetadataLastError:                          metadata.UnreadableMessage,
		MetadataLastErrorUrl:                       metadata.UnreadableURL,
	}
}

// identityProviderRegistrationError preserves the important distinction
// between a provider rejecting Speakeasy's fixed registration contract (which cannot
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

// supportsAutomaticClientRegistration reports whether this flow can obtain a
// client from a provider without manual setup: through dynamic client
// registration it can use, or through a Client ID Metadata Document under the
// predicate the dashboard's automatic setup uses. An issuer offering neither is
// skipped before any upstream client is registered.
func supportsAutomaticClientRegistration(registrationEndpoint string, clientIDMetadataDocumentSupported bool, tokenEndpointAuthMethodsSupported []string) bool {
	return attachmentCanUseDynamicRegistration(registrationEndpoint, tokenEndpointAuthMethodsSupported) || remotesessions.SupportsClientIDMetadataDocument(clientIDMetadataDocumentSupported, tokenEndpointAuthMethodsSupported)
}

// attachmentCanUseDynamicRegistration reports whether dynamic registration can
// give this flow the client it requires: a valid endpoint, and a token
// endpoint that accepts client_secret_basic (an unlisted method set defaults
// to it under RFC 8414). A provider that excludes it would only hand back a
// client this flow refuses.
func attachmentCanUseDynamicRegistration(registrationEndpoint string, tokenEndpointAuthMethodsSupported []string) bool {
	return validDynamicClientRegistrationEndpoint(registrationEndpoint) && (len(tokenEndpointAuthMethodsSupported) == 0 || slices.Contains(tokenEndpointAuthMethodsSupported, browserCatalogDCRAuthMethod))
}

func validDynamicClientRegistrationEndpoint(raw string) bool {
	return urls.IsAbsoluteHTTPS(raw)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
