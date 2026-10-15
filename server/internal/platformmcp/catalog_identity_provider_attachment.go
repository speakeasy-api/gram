//nolint:exhaustruct // OAuth persistence values intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
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

	// ErrSignInRemovalStale refuses a confirmation that does not name the
	// server state it was agreed against: the sign-in was reconfigured since
	// the preview, or the same confirmation is being replayed after it was
	// already carried out.
	ErrSignInRemovalStale = errors.New("platform mcp sign-in removal confirmation is stale")
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
	PreviewSignInRemoval(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (SignInRemovalPreview, error)
	RemoveSignIn(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, expectedVersion string) (SignInRemovalResult, error)
}

// SignInRemovalPreview is what turning a server's sign-in off would do, read
// without changing anything: the same decision the removal makes.
type SignInRemovalPreview struct {
	// Configured reports that the server signs in to a provider today. False
	// means the removal has nothing to do.
	Configured bool

	// ProviderURL is the issuer of the provider it signs in to, empty when it
	// signs in to none. Non-secret.
	ProviderURL string

	// ClientStaysBound reports that the provider's OAuth client is kept
	// because another MCP server in the organization still signs in through
	// it. False means removal unbinds it, which signs out everyone holding a
	// session through it.
	ClientStaysBound bool

	// Version identifies everything this preview describes: the server's own
	// sign-in and whether removing it releases the provider's OAuth client.
	// The removal only acts on a confirmation naming the same state, so a
	// confirmation agreed against one described outcome cannot carry out a
	// different one, and cannot be replayed once it has been carried out.
	//
	// Opaque to the caller: it is produced here and only ever handed back.
	Version string
}

// SignInRemovalResult is what turning a server's sign-in off left in place.
type SignInRemovalResult struct {
	// Removed reports that the server now signs in to nothing. True also for a
	// server that already did, which makes a retry safe.
	Removed bool

	// ProviderURL is the issuer of the provider it signed in to, empty when it
	// signed in to none.
	ProviderURL string

	// ClientUnbound reports that the provider's OAuth client was unbound,
	// which signs out everyone holding a session through it.
	ClientUnbound bool
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

	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("load registered MCP server: %w", err)
	}

	if attached, err := s.matchingAttachment(ctx, principal.OrganizationID, project, registration.UserSessionIssuerID.UUID, server.RemoteSessionIssuerID, metadata.Issuer); err != nil {
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

	// The registration names what the first login will request: the row's pin
	// and challenge scopes, else the list this attach just read.
	registrationResource, _, err := remotesessions.CachedResourceScopes(ctx, s.db, project.ID, remote.Url, true)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("read protected resource for attachment: %w", err)
	}
	registrationResource.ScopesSupported = append([]string(nil), resourceMetadata.ScopesSupported...)
	registrationResource.Live = true
	commit := s.identity.Prepare(attachmentIdentityPlan(principal, project, registrationID, registration.UserSessionIssuerID.UUID, registration.McpServerID, metadata, existing, reuse, remote.Url, resourceMetadata, registrationResource))
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
		if err != nil || strings.TrimSpace(metadata.Issuer) == "" || !sameIssuerURL(metadata.Issuer, authorizationServer) || strings.TrimSpace(metadata.AuthorizationEndpoint) == "" || strings.TrimSpace(metadata.TokenEndpoint) == "" || !supportsAutomaticClientRegistration(metadata.RegistrationEndpoint, metadata.ClientIDMetadataDocumentSupported, metadata.TokenEndpointAuthMethodsSupported) {
			continue
		}
		return metadata, nil
	}
	return remotesessions.DiscoveredIssuerMetadata{}, ErrIdentityProviderAttachmentUnsupported
}

// matchingAttachment reports that this registration's MCP server is already
// attached to issuerURL and the commit below has nothing to do: the server
// signs in to that provider and a client for it is bound to its user session
// issuer.
//
// Both halves are required. A client bound without the server naming the
// provider is the state a cleared provider leaves behind, and the server-level
// reconcile does not repair it on a shared issuer, so reporting success there
// would leave the server with no sign-in. The commit instead reuses that
// client and stamps the server.
//
// Other providers bound to the same user session issuer belong to the other
// servers sharing it and are not a conflict. Two clients for this one provider
// are: no reader can tell which the server signs in through.
func (s *CatalogIdentityProviderAttachmentService) matchingAttachment(ctx context.Context, organizationID string, project ResolvedProject, userSessionIssuerID uuid.UUID, serverProvider uuid.NullUUID, issuerURL string) (bool, error) {
	clients, err := remotesessionsrepo.New(s.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(project.ID),
		OrganizationID:      conv.ToPGText(organizationID),
		OrganizationWide:    false,
	})
	if err != nil {
		return false, fmt.Errorf("list registered identity providers: %w", err)
	}
	matching := make([]remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow, 0, 1)
	for _, client := range clients {
		if sameIssuerURL(client.IssuerUrl, issuerURL) {
			matching = append(matching, client)
		}
	}
	if len(matching) > 1 {
		return false, ErrIdentityProviderAttachmentConflict
	}
	if len(matching) == 0 {
		return false, nil
	}
	return serverProvider.Valid && serverProvider.UUID == matching[0].RemoteSessionIssuerID, nil
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
// discovered provider (or the reusable stored one), a client this flow
// registers itself, and the registration's MCP server, which the commit stamps
// with the provider. Dynamic registration stays the path for a provider where
// this flow can use it, so existing attachments are unchanged; otherwise a
// provider is set up through a Client ID Metadata Document when it supports
// one, the same path the dashboard's automatic setup takes. A provider offering
// neither leaves the registration needing manual setup.
func attachmentIdentityPlan(principal Principal, project ResolvedProject, registrationID, userSessionIssuerID uuid.UUID, mcpServerID uuid.NullUUID, metadata remotesessions.DiscoveredIssuerMetadata, existing remotesessionsrepo.RemoteSessionIssuer, reuse bool, resourceURL string, resourceMetadata wellknown.OAuthProtectedResourceMetadata, registrationResource remotesessions.ResourceScopes) remotesessions.IdentityPlan {
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
		// The registration's own MCP server. The provider this attaches is
		// that server's upstream, not the user session issuer's, which other
		// servers in the organization share.
		MCPServerID: mcpServerID,
		Provider:    provider,
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

// PreviewSignInRemoval reads what turning the registration's server sign-in
// off would do, and changes nothing. It answers from the server's own
// configured provider, so the preview and the removal agree on which client is
// at stake and whether another server keeps it bound.
func (s *CatalogIdentityProviderAttachmentService) PreviewSignInRemoval(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (SignInRemovalPreview, error) {
	server, issuer, err := s.signInRemovalTarget(ctx, principal, project, registrationID)
	if err != nil {
		return SignInRemovalPreview{}, err
	}
	serverVersion := remotesessions.ServerIdentityVersion(server)
	if !server.RemoteSessionIssuerID.Valid {
		return SignInRemovalPreview{Configured: false, ProviderURL: "", ClientStaysBound: false, Version: signInRemovalFingerprint(serverVersion, false)}, nil
	}
	provider, err := remotesessionsrepo.New(s.db).GetRemoteSessionIssuerByID(ctx, remotesessionsrepo.GetRemoteSessionIssuerByIDParams{
		ID:                    server.RemoteSessionIssuerID.UUID,
		ProjectID:             conv.ToNullUUID(project.ID),
		OrganizationID:        conv.ToPGText(principal.OrganizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
	})
	if err != nil {
		return SignInRemovalPreview{}, fmt.Errorf("load configured sign-in provider: %w", err)
	}
	others, err := remotesessionsrepo.New(s.db).CountOrganizationMCPServersWithRemoteSessionIssuer(ctx, remotesessionsrepo.CountOrganizationMCPServersWithRemoteSessionIssuerParams{
		UserSessionIssuerID:   conv.ToNullUUID(issuer),
		RemoteSessionIssuerID: server.RemoteSessionIssuerID,
		ExcludedMcpServerID:   server.ID,
		OrganizationID:        principal.OrganizationID,
	})
	if err != nil {
		return SignInRemovalPreview{}, fmt.Errorf("count MCP servers sharing the sign-in provider: %w", err)
	}
	return SignInRemovalPreview{Configured: true, ProviderURL: provider.Issuer, ClientStaysBound: others > 0, Version: signInRemovalFingerprint(serverVersion, others > 0)}, nil
}

// signInRemovalFingerprint encodes what a preview described, so a confirmation
// can be held to all of it rather than only the server row. The pair is
// carried rather than hashed because the removal compares each half under the
// lock that governs it: the server version under the server row lock, the
// client-release outcome under the user session issuer lock.
func signInRemovalFingerprint(serverVersion string, clientStaysBound bool) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("v1\x00%s\x00%t", serverVersion, !clientStaysBound)))
}

// signInRemovalExpectation decodes a fingerprint back into the expectation the
// removal enforces. An unreadable one is stale rather than an error: it was
// not produced by a preview of this server, so nothing may act on it.
func signInRemovalExpectation(fingerprint string) (remotesessions.DisableServerIdentityExpectation, bool) {
	var none remotesessions.DisableServerIdentityExpectation
	raw, err := base64.RawURLEncoding.DecodeString(fingerprint)
	if err != nil {
		return none, false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] == "" {
		return none, false
	}
	unbound, err := strconv.ParseBool(parts[2])
	if err != nil {
		return none, false
	}
	return remotesessions.DisableServerIdentityExpectation{Version: parts[1], ClientUnbound: &unbound}, true
}

// RemoveSignIn turns the registration's server sign-in off through the same
// operation the dashboard runs, so its refusals, audit and client-preservation
// rules are the ones that apply.
func (s *CatalogIdentityProviderAttachmentService) RemoveSignIn(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, expectedVersion string) (SignInRemovalResult, error) {
	// The agent decided against a preview it showed the user, so the version
	// is required. The comparison itself belongs to the write: it runs inside
	// the transaction that holds the server row and clears the provider, so a
	// dashboard commit cannot land between the check and the clear. Taking a
	// lock here instead would be both weaker — it is keyed on this user, and
	// the dashboard takes nothing — and held across an independently
	// transactional call.
	expect, ok := signInRemovalExpectation(expectedVersion)
	if !ok {
		return SignInRemovalResult{}, ErrSignInRemovalStale
	}

	// Read for the provider URL the result reports, not to decide anything.
	preview, err := s.PreviewSignInRemoval(ctx, principal, project, registrationID)
	if err != nil {
		return SignInRemovalResult{}, err
	}
	server, issuer, err := s.signInRemovalTarget(ctx, principal, project, registrationID)
	if err != nil {
		return SignInRemovalResult{}, err
	}
	result, err := s.identity.DisableServerIdentity(ctx, remotesessions.IdentityScope{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            project.ID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName:     nil,
		ActorIsPlatformAdmin: false,
	}, issuer, server.ID, expect)
	if errors.Is(err, remotesessions.ErrIdentityStale) {
		return SignInRemovalResult{}, ErrSignInRemovalStale
	}
	if err != nil {
		return SignInRemovalResult{}, attachmentCommitError("turn off MCP server sign-in", err)
	}
	return SignInRemovalResult{Removed: true, ProviderURL: preview.ProviderURL, ClientUnbound: result.ClientUnbound}, nil
}

// signInRemovalTarget resolves the registration's MCP server and the user
// session issuer it signs in through, refusing a registration that has no
// server to act on.
func (s *CatalogIdentityProviderAttachmentService) signInRemovalTarget(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (mcpserversrepo.McpServer, uuid.UUID, error) {
	var none mcpserversrepo.McpServer
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return none, uuid.Nil, ErrRegistrationInvalid
	}
	if err != nil {
		return none, uuid.Nil, fmt.Errorf("load platform mcp registration: %w", err)
	}
	if !registration.McpServerID.Valid || !registration.UserSessionIssuerID.Valid {
		return none, uuid.Nil, ErrRegistrationInvalid
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, uuid.Nil, ErrRegistrationInvalid
	}
	if err != nil {
		return none, uuid.Nil, fmt.Errorf("load registered MCP server: %w", err)
	}
	if !server.UserSessionIssuerID.Valid || server.UserSessionIssuerID.UUID != registration.UserSessionIssuerID.UUID {
		return none, uuid.Nil, ErrRegistrationInvalid
	}
	return server, registration.UserSessionIssuerID.UUID, nil
}
