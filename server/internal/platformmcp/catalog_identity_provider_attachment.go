//nolint:exhaustruct // OAuth persistence values intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
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
	db        *pgxpool.Pool
	enc       *encryption.Client
	policy    *guardian.Policy
	audit     *audit.Logger
	serverURL *url.URL
}

func NewCatalogIdentityProviderAttachmentService(db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy, auditLogger *audit.Logger, serverURL *url.URL) *CatalogIdentityProviderAttachmentService {
	if serverURL == nil {
		return &CatalogIdentityProviderAttachmentService{}
	}
	serverURLCopy := *serverURL
	return &CatalogIdentityProviderAttachmentService{db: db, enc: enc, policy: policy, audit: auditLogger, serverURL: &serverURLCopy}
}

// Attach discovers the exact provider advertised by the lifecycle-owned Remote
// MCP source, creates a project-owned remote-session issuer/client when needed,
// and binds it to the registration's existing user-session issuer. It is safe
// to retry after a successful call: the existing matching binding is reused.
func (s *CatalogIdentityProviderAttachmentService) Attach(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	if s == nil || s.db == nil || s.enc == nil || s.policy == nil || s.audit == nil || s.serverURL == nil || principal.UserID == "" || principal.OrganizationID == "" || project.ID == uuid.Nil || registrationID == uuid.Nil {
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

	attached, err := s.attachProvider(ctx, lockQ, principal, project, registration.UserSessionIssuerID.UUID, remote.Url, providerAttachMode{
		requireConfidential: true,
		sharedIssuer:        false,
		issuerSlug:          func(string) string { return attachmentIssuerSlug(registrationID) },
		issuerName:          "",
	})
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: attached.IssuerURL}, nil
}

// UpstreamProviderAttachment is the client attachProvider bound. Bound is true
// when this call created the binding, so a caller that fails afterwards knows
// it is its own to remove.
type UpstreamProviderAttachment struct {
	Bound                 bool
	IssuerURL             string
	RemoteSessionIssuerID uuid.UUID
	ClientID              uuid.UUID
}

// UpstreamProviderAttachmentInput names the issuer to bind and the resource
// whose provider to register with. IssuerSlug names a newly discovered
// provider's issuer row from its issuer URL.
type UpstreamProviderAttachmentInput struct {
	UserSessionIssuerID uuid.UUID
	ResourceURL         string
	IssuerSlug          func(issuerURL string) string
	// IssuerName labels a newly discovered provider on consent pages.
	IssuerName string
}

// providerAttachMode is the per-caller shape of attachProvider.
type providerAttachMode struct {
	// requireConfidential rejects public clients, as the browser catalog does.
	requireConfidential bool
	// sharedIssuer: the issuer fronts several providers, so only clients for
	// this provider are matched and an existing project client for it is
	// bound rather than registering another.
	sharedIssuer bool
	issuerSlug   func(issuerURL string) string
	issuerName   string
}

// AttachUpstreamProvider binds the OAuth provider protecting input.ResourceURL
// to an issuer that may already front other providers, reusing a client this
// project registered with it and registering one otherwise. Trusted
// server-side callers only: the resource URL must come from a persisted
// definition, never from an MCP or browser input. Safe to retry.
func (s *CatalogIdentityProviderAttachmentService) AttachUpstreamProvider(ctx context.Context, principal Principal, project ResolvedProject, input UpstreamProviderAttachmentInput) (UpstreamProviderAttachment, error) {
	if s == nil || s.db == nil || s.enc == nil || s.policy == nil || s.audit == nil || s.serverURL == nil || principal.UserID == "" || principal.OrganizationID == "" || project.ID == uuid.Nil || input.UserSessionIssuerID == uuid.Nil || input.ResourceURL == "" || input.IssuerSlug == nil {
		return UpstreamProviderAttachment{}, ErrIdentityProviderAttachmentUnavailable
	}

	// Before any upstream registration, so a stale issuer cannot orphan one.
	if _, err := remotesessionsrepo.New(s.db).GetUserSessionIssuerForProject(ctx, remotesessionsrepo.GetUserSessionIssuerForProjectParams{ID: input.UserSessionIssuerID, ProjectID: project.ID, OrganizationID: principal.OrganizationID}); err != nil {
		return UpstreamProviderAttachment{}, fmt.Errorf("validate registered MCP session issuer: %w: %w", ErrIdentityProviderAttachmentConflict, err)
	}

	lockTx, err := s.db.Begin(ctx)
	if err != nil {
		return UpstreamProviderAttachment{}, fmt.Errorf("begin identity-provider attachment lock: %w", err)
	}
	defer func() { _ = lockTx.Rollback(ctx) }()

	result, err := s.attachProvider(ctx, platformrepo.New(lockTx), principal, project, input.UserSessionIssuerID, input.ResourceURL, providerAttachMode{
		requireConfidential: false,
		sharedIssuer:        true,
		issuerSlug:          input.IssuerSlug,
		issuerName:          input.IssuerName,
	})
	if err != nil {
		return UpstreamProviderAttachment{}, err
	}
	if err := lockTx.Commit(ctx); err != nil {
		return UpstreamProviderAttachment{}, fmt.Errorf("commit identity-provider attachment lock: %w", err)
	}
	return result, nil
}

// attachProvider discovers the authorization server protecting resourceURL and
// binds a client for it to userSessionIssuerID, registering one when needed.
func (s *CatalogIdentityProviderAttachmentService) attachProvider(ctx context.Context, lockQ *platformrepo.Queries, principal Principal, project ResolvedProject, userSessionIssuerID uuid.UUID, resourceURL string, mode providerAttachMode) (UpstreamProviderAttachment, error) {
	resourceMetadata, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, s.policy, resourceURL)
	if err != nil {
		return UpstreamProviderAttachment{}, fmt.Errorf("discover registered MCP identity provider: %w: %w", discoveryFailureKind(err), err)
	}
	metadata, err := s.discoverSupportedIssuerMetadata(ctx, resourceMetadata.AuthorizationServers)
	if err != nil {
		return UpstreamProviderAttachment{}, err
	}
	if err := lockQ.LockPlatformMCPRemoteIssuerAttachment(ctx, platformrepo.LockPlatformMCPRemoteIssuerAttachmentParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
		Issuer:         strings.TrimRight(metadata.Issuer, "/"),
	}); err != nil {
		return UpstreamProviderAttachment{}, fmt.Errorf("lock identity-provider issuer attachment: %w", err)
	}

	if match, attached, err := s.matchingAttachment(ctx, principal.OrganizationID, project, userSessionIssuerID, metadata.Issuer, mode.sharedIssuer); err != nil {
		return UpstreamProviderAttachment{}, err
	} else if attached {
		return UpstreamProviderAttachment{Bound: false, IssuerURL: metadata.Issuer, RemoteSessionIssuerID: match.RemoteSessionIssuerID, ClientID: match.ClientID}, nil
	}

	// A shared issuer reuses a client the project already registered with
	// the provider rather than registering another; ensureIssuer is idempotent
	// so running it ahead of registration costs nothing.
	if mode.sharedIssuer {
		issuer, err := s.ensureIssuer(ctx, principal, project, mode.issuerSlug(metadata.Issuer), mode.issuerName, metadata)
		if err != nil {
			return UpstreamProviderAttachment{}, err
		}
		if clientID, bound, ok, err := s.bindExistingProjectClient(ctx, principal, project, userSessionIssuerID, issuer.ID); err != nil {
			return UpstreamProviderAttachment{}, err
		} else if ok {
			return UpstreamProviderAttachment{Bound: bound, IssuerURL: metadata.Issuer, RemoteSessionIssuerID: issuer.ID, ClientID: clientID}, nil
		}
	}

	// This is server-to-server; any client secret stays in the stack frame only
	// until createAndAttachClient encrypts it for persistence.
	scope := strings.Join(resourceMetadata.ScopesSupported, " ")
	registered, err := remotesessions.RegisterDynamicClient(ctx, s.policy, s.serverURL, remotesessions.ProxyRegisterRequest{
		RegistrationEndpoint:    metadata.RegistrationEndpoint,
		Scope:                   optionalString(scope),
		TokenEndpointAuthMethod: optionalString(browserCatalogDCRAuthMethod),
	})
	if err != nil {
		return UpstreamProviderAttachment{}, identityProviderDynamicRegistrationError(err)
	}
	if mode.requireConfidential && !validBrowserCatalogDynamicClient(registered) {
		return UpstreamProviderAttachment{}, ErrIdentityProviderAttachmentUnsupported
	}
	if !mode.requireConfidential && !validDynamicClient(registered) {
		return UpstreamProviderAttachment{}, ErrIdentityProviderAttachmentUnsupported
	}
	issuer, err := s.ensureIssuer(ctx, principal, project, mode.issuerSlug(metadata.Issuer), mode.issuerName, metadata)
	if err != nil {
		return UpstreamProviderAttachment{}, err
	}

	clientID, bound, err := s.createAndAttachClient(ctx, principal, project, userSessionIssuerID, issuer.ID, resourceMetadata.ScopesSupported, registered)
	if err != nil {
		return UpstreamProviderAttachment{}, err
	}
	return UpstreamProviderAttachment{Bound: bound, IssuerURL: metadata.Issuer, RemoteSessionIssuerID: issuer.ID, ClientID: clientID}, nil
}

// bindExistingProjectClient binds a client this project already registered
// with the provider, reporting the client and whether this call bound it.
// ok is false when the project holds none.
func (s *CatalogIdentityProviderAttachmentService) bindExistingProjectClient(ctx context.Context, principal Principal, project ResolvedProject, userSessionIssuerID, issuerID uuid.UUID) (clientID uuid.UUID, bound, ok bool, err error) {
	clients, err := remotesessionsrepo.New(s.db).ListRemoteSessionClientsByProjectID(ctx, remotesessionsrepo.ListRemoteSessionClientsByProjectIDParams{
		ProjectID:             project.ID,
		OrganizationID:        principal.OrganizationID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Cursor:                uuid.NullUUID{},
		LimitValue:            1,
	})
	if err != nil {
		return uuid.Nil, false, false, fmt.Errorf("list identity-provider clients: %w", err)
	}
	if len(clients) == 0 {
		return uuid.Nil, false, false, nil
	}
	clientID = clients[0].RemoteSessionClient.ID

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, false, fmt.Errorf("begin identity-provider client binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := remotesessionsrepo.New(tx)
	if err := q.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return uuid.Nil, false, false, fmt.Errorf("lock identity provider for client binding: %w", err)
	}
	// Under the lock: a concurrent writer may have bound a client already.
	already, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		ProjectID:             project.ID,
		UserSessionIssuerID:   userSessionIssuerID,
		OrganizationID:        principal.OrganizationID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Cursor:                uuid.NullUUID{},
		LimitValue:            1,
	})
	if err != nil {
		return uuid.Nil, false, false, fmt.Errorf("check existing identity-provider client attachment: %w", err)
	}
	if len(already) > 0 {
		return already[0].RemoteSessionClient.ID, false, true, nil
	}
	if err := q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userSessionIssuerID}); err != nil {
		return uuid.Nil, false, false, fmt.Errorf("bind identity-provider client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, false, fmt.Errorf("commit identity-provider client binding: %w", err)
	}
	return clientID, true, true, nil
}

func (s *CatalogIdentityProviderAttachmentService) discoverSupportedIssuerMetadata(ctx context.Context, authorizationServers []string) (remotesessions.DiscoveredIssuerMetadata, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for _, rawAuthorizationServer := range authorizationServers {
		authorizationServer := strings.TrimSpace(rawAuthorizationServer)
		if authorizationServer == "" {
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

// matchingAttachment reports the provider client already bound to the issuer.
// A single-provider issuer holding a client for another provider conflicts; a
// shared issuer only counts clients for this provider.
func (s *CatalogIdentityProviderAttachmentService) matchingAttachment(ctx context.Context, organizationID string, project ResolvedProject, userSessionIssuerID uuid.UUID, issuerURL string, sharedIssuer bool) (remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow, bool, error) {
	clients, err := remotesessionsrepo.New(s.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(project.ID),
		OrganizationID:      conv.ToPGText(organizationID),
	})
	if err != nil {
		return remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow{}, false, fmt.Errorf("list registered identity providers: %w", err)
	}
	if sharedIssuer {
		clients = slices.DeleteFunc(clients, func(c remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow) bool {
			return !sameIssuerURL(c.IssuerUrl, issuerURL)
		})
	}
	if len(clients) == 0 {
		return remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow{}, false, nil
	}
	if len(clients) != 1 || !sameIssuerURL(clients[0].IssuerUrl, issuerURL) {
		return remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow{}, false, ErrIdentityProviderAttachmentConflict
	}
	return clients[0], true, nil
}

func (s *CatalogIdentityProviderAttachmentService) ensureIssuer(ctx context.Context, principal Principal, project ResolvedProject, issuerSlug, issuerName string, metadata remotesessions.DiscoveredIssuerMetadata) (remotesessionsrepo.RemoteSessionIssuer, error) {
	if strings.TrimSpace(issuerName) == "" {
		issuerName = "Remote identity provider"
	}
	issuers, err := remotesessionsrepo.New(s.db).ListRemoteSessionIssuersByIssuerURL(ctx, remotesessionsrepo.ListRemoteSessionIssuersByIssuerURLParams{
		Issuers:               []string{metadata.Issuer},
		ProjectID:             conv.ToNullUUID(project.ID),
		IncludeOrganizational: false,
		OrganizationID:        conv.ToPGText(principal.OrganizationID),
		IncludeGlobal:         false,
	})
	if err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("find registered identity provider: %w", err)
	}
	if len(issuers) == 1 {
		issuer := issuers[0]
		if !issuer.ProjectID.Valid || issuer.ProjectID.UUID != project.ID || !issuer.OrganizationID.Valid || issuer.OrganizationID.String != principal.OrganizationID || !issuer.AuthorizationEndpoint.Valid || issuer.AuthorizationEndpoint.String == "" || !issuer.TokenEndpoint.Valid || issuer.TokenEndpoint.String == "" || !issuer.RegistrationEndpoint.Valid || issuer.RegistrationEndpoint.String == "" {
			return remotesessionsrepo.RemoteSessionIssuer{}, ErrIdentityProviderAttachmentConflict
		}
		return issuer, nil
	}
	if len(issuers) > 1 {
		return remotesessionsrepo.RemoteSessionIssuer{}, ErrIdentityProviderAttachmentConflict
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("begin identity-provider issuer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := remotesessionsrepo.New(tx)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                   conv.ToNullUUID(project.ID),
		OrganizationID:              conv.ToPGText(principal.OrganizationID),
		Slug:                        issuerSlug,
		Issuer:                      metadata.Issuer,
		Name:                        conv.ToPGText(issuerName),
		LogoAssetID:                 uuid.NullUUID{},
		ClientSetupDocumentationUrl: pgtype.Text{},
		AuthorizationEndpoint:       conv.ToPGText(metadata.AuthorizationEndpoint),
		TokenEndpoint:               conv.ToPGText(metadata.TokenEndpoint),
		RegistrationEndpoint:        conv.ToPGText(metadata.RegistrationEndpoint),
		JwksUri:                     pgtype.Text{},
		ServiceDocumentation:        pgtype.Text{},
		OpPolicyUri:                 pgtype.Text{},
		OpTosUri:                    pgtype.Text{},
		// NOT NULL columns: an issuer document that omits a list must persist {}.
		ScopesSupported:                   nonNilStrings(metadata.ScopesSupported),
		GrantTypesSupported:               nonNilStrings(metadata.GrantTypesSupported),
		ResponseTypesSupported:            nonNilStrings(metadata.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: nonNilStrings(metadata.TokenEndpointAuthMethodsSupported),
		// Nullable column: {} records "advertises no methods", NULL "not
		// captured"; DiscoveredIssuerMetadata guarantees the field non-nil.
		CodeChallengeMethodsSupported:     slices.Clone(metadata.CodeChallengeMethodsSupported),
		ClientIDMetadataDocumentSupported: metadata.ClientIDMetadataDocumentSupported,
		Oidc:                              false,
		Passthrough:                       false,
	})
	if err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("create discovered identity provider: %w", err)
	}
	if err := s.audit.LogRemoteSessionIssuerCreate(ctx, tx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:         principal.OrganizationID,
		ProjectID:              project.ID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(issuer.ID),
		Slug:                   issuer.Slug,
		IssuerURL:              issuer.Issuer,
		Name:                   conv.FromPGText[string](issuer.Name),
	}); err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("audit discovered identity provider: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return remotesessionsrepo.RemoteSessionIssuer{}, fmt.Errorf("commit discovered identity provider: %w", err)
	}
	return issuer, nil
}

// createAndAttachClient persists the registered client bound to the issuer,
// returning the bound client and whether this call created the binding.
func (s *CatalogIdentityProviderAttachmentService) createAndAttachClient(ctx context.Context, principal Principal, project ResolvedProject, userSessionIssuerID, issuerID uuid.UUID, scopes []string, registered remotesessions.ProxyRegisterResponse) (uuid.UUID, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("begin identity-provider client transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := remotesessionsrepo.New(tx)
	if err := q.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return uuid.Nil, false, fmt.Errorf("lock identity provider for client attachment: %w", err)
	}
	if _, err := q.GetUserSessionIssuerForProject(ctx, remotesessionsrepo.GetUserSessionIssuerForProjectParams{ID: userSessionIssuerID, ProjectID: project.ID, OrganizationID: principal.OrganizationID}); err != nil {
		return uuid.Nil, false, fmt.Errorf("validate registered MCP session issuer: %w", err)
	}
	bound, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		ProjectID:             project.ID,
		UserSessionIssuerID:   userSessionIssuerID,
		OrganizationID:        principal.OrganizationID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Cursor:                uuid.NullUUID{},
		LimitValue:            2,
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("check existing identity-provider client attachment: %w", err)
	}
	if len(bound) == 1 {
		if bound[0].RemoteSessionClient.RemoteSessionIssuerID != issuerID {
			return uuid.Nil, false, ErrIdentityProviderAttachmentConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, false, fmt.Errorf("commit existing identity-provider attachment: %w", err)
		}
		return bound[0].RemoteSessionClient.ID, false, nil
	}
	if len(bound) > 1 {
		return uuid.Nil, false, ErrIdentityProviderAttachmentConflict
	}

	var secret pgtype.Text
	if registered.ClientSecret != "" {
		ciphertext, err := s.enc.Encrypt([]byte(registered.ClientSecret))
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("encrypt identity-provider client secret: %w", err)
		}
		secret = conv.ToPGText(ciphertext)
	}
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(project.ID),
		OrganizationID:          conv.ToPGText(principal.OrganizationID),
		RemoteSessionIssuerID:   issuerID,
		ClientID:                registered.ClientID,
		ClientSecretEncrypted:   secret,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   registered.ClientSecretExpiresAt,
		TokenEndpointAuthMethod: optionalText(registered.TokenEndpointAuthMethod),
		Scope:                   append([]string(nil), scopes...),
		Audience:                pgtype.Text{},
		LegacyCallbackUrl:       false,
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("create identity-provider client: %w", err)
	}
	if err := q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: userSessionIssuerID}); err != nil {
		return uuid.Nil, false, fmt.Errorf("attach identity-provider client to registered MCP: %w", err)
	}
	if err := s.audit.LogRemoteSessionClientCreate(ctx, tx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         principal.OrganizationID,
		ProjectID:              project.ID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
	}); err != nil {
		return uuid.Nil, false, fmt.Errorf("audit identity-provider client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, fmt.Errorf("commit identity-provider client attachment: %w", err)
	}
	return client.ID, true, nil
}

// identityProviderDynamicRegistrationError preserves the important distinction
// between a provider rejecting Gram's fixed DCR contract (which cannot succeed
// unchanged) and a temporary upstream/transport failure (which can be retried).
// It intentionally does not carry an upstream response detail into the MCP tool
// result or logs.
func identityProviderDynamicRegistrationError(err error) error {
	var registrationErr *remotesessions.DynamicClientRegistrationError
	if errors.As(err, &registrationErr) && registrationErr.StatusCode >= 400 && registrationErr.StatusCode < 500 && registrationErr.StatusCode != http.StatusRequestTimeout && registrationErr.StatusCode != http.StatusTooManyRequests {
		return fmt.Errorf("register identity-provider client: %w", ErrIdentityProviderAttachmentUnsupported)
	}
	return fmt.Errorf("register identity-provider client: %w", ErrIdentityProviderAttachmentUnavailable)
}

// discoveryFailureKind keeps a transient probe failure retryable; anything
// the resource answered with is a property of the resource.
func discoveryFailureKind(err error) error {
	var probe *wellknown.ProtectedResourceDiscoveryError
	if errors.As(err, &probe) {
		switch probe.Code() {
		case "timeout", "transport_error":
			return ErrIdentityProviderAttachmentUnavailable
		case "http_error":
			if probe.Status >= http.StatusInternalServerError {
				return ErrIdentityProviderAttachmentUnavailable
			}
		}
	}
	return ErrIdentityProviderAttachmentUnsupported
}

func attachmentIssuerSlug(registrationID uuid.UUID) string {
	sum := sha256.Sum256([]byte(registrationID.String()))
	return "platform-mcp-auto-" + hex.EncodeToString(sum[:8])
}

func sameIssuerURL(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

func validDynamicClientRegistrationEndpoint(raw string) bool {
	endpoint, err := url.Parse(raw)
	return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil
}

// validBrowserCatalogDynamicClient requires a confidential client for the
// browser-catalog flow. The local fixture deliberately registers public clients
// through its separate configurator path and never reaches this boundary.
func validBrowserCatalogDynamicClient(registered remotesessions.ProxyRegisterResponse) bool {
	return registered.ClientID != "" &&
		registered.ClientSecret != "" &&
		(registered.TokenEndpointAuthMethod == "" || registered.TokenEndpointAuthMethod == browserCatalogDCRAuthMethod)
}

// validDynamicClient accepts the confidential and public client shapes the
// remote-session token exchange can drive.
func validDynamicClient(registered remotesessions.ProxyRegisterResponse) bool {
	if registered.ClientID == "" {
		return false
	}
	switch registered.TokenEndpointAuthMethod {
	case "", string(remotesessions.TokenEndpointAuthMethodBasic), string(remotesessions.TokenEndpointAuthMethodPost):
		return registered.ClientSecret != ""
	case string(remotesessions.TokenEndpointAuthMethodNone):
		return true
	default:
		return false
	}
}

func nonNilStrings(values []string) []string {
	return append([]string{}, values...)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
