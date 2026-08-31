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
		Issuer:         strings.TrimRight(metadata.Issuer, "/"),
	}); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("lock identity-provider issuer attachment: %w", err)
	}

	if attached, err := s.matchingAttachment(ctx, principal.OrganizationID, project, registration.UserSessionIssuerID.UUID, metadata.Issuer); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	} else if attached {
		return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: metadata.Issuer}, nil
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
		return CatalogIdentityProviderAttachmentResult{}, identityProviderDynamicRegistrationError(err)
	}
	if !validBrowserCatalogDynamicClient(registered) {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnsupported
	}
	issuer, err := s.ensureIssuer(ctx, principal, project, registrationID, metadata)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}

	attached, err := s.createAndAttachClient(ctx, principal, project, registration.UserSessionIssuerID.UUID, issuer.ID, resourceMetadata.ScopesSupported, registered)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	return CatalogIdentityProviderAttachmentResult{Attached: attached, ProviderURL: metadata.Issuer}, nil
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

func (s *CatalogIdentityProviderAttachmentService) ensureIssuer(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, metadata remotesessions.DiscoveredIssuerMetadata) (remotesessionsrepo.RemoteSessionIssuer, error) {
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

func (s *CatalogIdentityProviderAttachmentService) createAndAttachClient(ctx context.Context, principal Principal, project ResolvedProject, userSessionIssuerID, issuerID uuid.UUID, scopes []string, registered remotesessions.ProxyRegisterResponse) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin identity-provider client transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := remotesessionsrepo.New(tx)
	if err := q.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return false, fmt.Errorf("lock identity provider for client attachment: %w", err)
	}
	if _, err := q.GetUserSessionIssuerForProject(ctx, remotesessionsrepo.GetUserSessionIssuerForProjectParams{ID: userSessionIssuerID, ProjectID: project.ID}); err != nil {
		return false, fmt.Errorf("validate registered MCP session issuer: %w", err)
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
		return false, fmt.Errorf("check existing identity-provider client attachment: %w", err)
	}
	if len(bound) == 1 {
		if bound[0].RemoteSessionClient.RemoteSessionIssuerID != issuerID {
			return false, ErrIdentityProviderAttachmentConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit existing identity-provider attachment: %w", err)
		}
		return true, nil
	}
	if len(bound) > 1 {
		return false, ErrIdentityProviderAttachmentConflict
	}

	var secret pgtype.Text
	if registered.ClientSecret != "" {
		ciphertext, err := s.enc.Encrypt([]byte(registered.ClientSecret))
		if err != nil {
			return false, fmt.Errorf("encrypt identity-provider client secret: %w", err)
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
		return false, fmt.Errorf("create identity-provider client: %w", err)
	}
	if err := q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: userSessionIssuerID}); err != nil {
		return false, fmt.Errorf("attach identity-provider client to registered MCP: %w", err)
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
		return false, fmt.Errorf("audit identity-provider client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit identity-provider client attachment: %w", err)
	}
	return true, nil
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

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
