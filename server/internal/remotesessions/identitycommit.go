// identitycommit.go configures a Remote Identity Provider for a user session
// issuer: the provider, the client that talks to it, and the binding between
// that client and the user session issuer. Every flow that does this runs the
// same steps on an IdentityCommit the IdentityCommitter prepares from an
// IdentityPlan:
//
//	commit := committer.Prepare(plan)
//	commit.Preflight(ctx)     // every refusal the writes enforce, as plain reads
//	commit.Register(ctx)      // link, manual, Client ID Metadata Document or DCR
//	tx := commit.Begin(ctx)   // the caller may take its own locks on tx.DB()
//	commit.Lock(ctx, tx)      // user session issuer, then the stored provider
//	commit.Bind(ctx, tx, reg) // provider, replaced or reused clients, client, binding
//	commit.Commit(ctx, tx)    // commit, then restamp the MCP servers on the issuer
//
// The refusals run twice from the same plan: in Preflight, before any
// upstream side effect, and again under the locks, where they are
// authoritative.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sort"
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
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrIdentityNotFound marks a row the scope cannot see.
	ErrIdentityNotFound = errors.New("identity: not found")

	// ErrIdentityInvalid marks a plan the stored configuration cannot serve,
	// such as a linked client of a different provider.
	ErrIdentityInvalid = errors.New("identity: invalid request")

	// ErrIdentityForbidden marks a write the actor's grants do not reach, such
	// as registering through a tunnel without platform admin.
	ErrIdentityForbidden = errors.New("identity: forbidden")

	// ErrIdentityConflict marks a write that no longer fits current state.
	ErrIdentityConflict = errors.New("identity: conflict")

	// ErrIdentityInvariant marks stored state no identity write produces, which
	// the commit refuses to build on.
	ErrIdentityInvariant = errors.New("identity: invariant violation")

	// ErrIdentityOrgWideBinding marks a binding write that would reach beyond
	// the scope's project by an actor without organization admin: linking or
	// unlinking an organization-owned client on an organization-level user
	// session issuer, whose binding every project's MCP servers on that issuer
	// share.
	ErrIdentityOrgWideBinding = errors.New("identity: organization-wide binding change")

	// ErrIdentityStepOrder marks a commit step run out of order, which is a
	// caller bug.
	ErrIdentityStepOrder = errors.New("identity commit: step run out of order")
)

// OrgWideBindingMessage is the caller-facing message for
// ErrIdentityOrgWideBinding.
const OrgWideBindingMessage = "this MCP server shares its sign-in client with MCP servers in other projects; changing it needs organization admin access"

const (
	agentBindingsOnReplacedClientMessage = "agents are still bound to this MCP server through its current client; unlink them before changing its Remote Identity Provider"
	identityChainingBindingsMessage      = "active identity-chaining bindings reference this configuration; explicitly unlink them before changing or deleting it"
	ambiguousCurrentProviderMessage      = "this MCP server's user session issuer is bound to more than one Remote Identity Provider; remove the extra clients from the Remote Identity Provider settings before changing it"
	clientTakenMessage                   = "a remote session client is already bound to this user session issuer for the same remote session issuer"
	providerClientInUseMessage           = "this Remote Identity Provider already has a client on this server's User Identity issuer, and each issuer keeps one client per provider. Use the existing client instead"
	replacementNotConfirmedMessage       = "the MCP servers using this client changed; review them and save again"
	replacementReadsBindingsMessage      = "a gateway or a server that is not directly Remote MCP-backed shares this User Identity issuer and may use this client, so it cannot be replaced from here. Assign this server a dedicated issuer instead"
	replacementOtherProjectMessage       = "an MCP server in another project uses this client, so it cannot be replaced from here. Assign this server a dedicated issuer instead"
)

// IdentityError is a refusal whose Message is safe to show the caller. Kind is
// one of the ErrIdentity* sentinels and is matched with errors.Is.
type IdentityError struct {
	// Kind classifies the refusal.
	Kind error

	// Message is safe to show the caller.
	Message string

	// Err is the underlying cause, if any.
	Err error
}

func (e *IdentityError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *IdentityError) Unwrap() []error {
	if e.Err == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Err}
}

func identityRefusal(kind error, cause error, message string) *IdentityError {
	return &IdentityError{Kind: kind, Message: message, Err: cause}
}

// IdentityCommitter prepares identity commits. It is stateless and shared by
// every flow that configures a Remote Identity Provider.
type IdentityCommitter struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *encryption.Client
	audit     *audit.Logger
	origins   CallbackOrigins
	policy    *guardian.Policy
	tunnels   *tunnelrouting.HTTPClient
	telemetry registration.Recorder
}

func NewIdentityCommitter(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, auditLogger *audit.Logger, serverURL *url.URL, policy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, telemetry registration.Recorder) *IdentityCommitter {
	return &IdentityCommitter{logger: logger, db: db, enc: enc, audit: auditLogger, origins: DefaultCallbackOrigins(serverURL), policy: policy, tunnels: tunnels, telemetry: telemetry}
}

// SetCallbackOrigins replaces the default origins, which pin every client to
// the server URL. Call it during wiring, before any commit runs.
func (c *IdentityCommitter) SetCallbackOrigins(origins CallbackOrigins) {
	c.origins = origins
}

// newClientBaseURL is the callback_base_url recorded on a client this commit
// creates. The DCR registration and the stored row must agree on it.
func (c *IdentityCommit) newClientBaseURL(ctx context.Context) (pgtype.Text, error) {
	if c.callbackBaseURL != nil {
		return *c.callbackBaseURL, nil
	}
	origin, err := c.committer.origins.ResolveNewClientBaseURL(ctx, c.committer.db, c.plan.Scope.OrganizationID)
	if err != nil {
		return pgtype.Text{}, err
	}
	c.callbackBaseURL = &origin
	return origin, nil
}

// IdentityScope is the tenant and actor every read and write of a commit uses.
type IdentityScope struct {
	// OrganizationID scopes every read and write.
	OrganizationID string

	// ProjectID owns every provider and client the commit creates.
	ProjectID uuid.UUID

	// Actor is recorded on every audit event.
	Actor urn.Principal

	// ActorDisplayName is the actor's display name, if known.
	ActorDisplayName *string

	// ActorIsPlatformAdmin gates the registration paths that reach a private
	// network. mcp:write on one server is not enough to borrow a tunnel.
	ActorIsPlatformAdmin bool

	// ActorIsOrganizationAdmin gates linking and unlinking organization-owned
	// clients on an organization-level user session issuer, which every
	// project's servers on that issuer share.
	ActorIsOrganizationAdmin bool
}

// IdentityPlan is what a caller wants configured. It holds only the choices
// where flows differ; every step of the commit reads it.
type IdentityPlan struct {
	// Scope is the tenant and actor.
	Scope IdentityScope

	// UserSessionIssuerID is the user session issuer the client is bound to.
	UserSessionIssuerID uuid.UUID

	// MCPServerID is the MCP server being configured, which Bind associates
	// with the bound client; uuid.Nil configures the issuer alone. The caller
	// locks the server row before Lock, keeping the server-then-issuer lock
	// order of MCP server updates.
	MCPServerID uuid.UUID

	// Provider is UseProvider or CreateProvider.
	Provider ProviderChoice

	// Client is LinkClient, ManualClient or RegisterClient.
	Client ClientChoice

	// Bound is what happens to clients already bound to the user session
	// issuer: ReplaceBound or ReuseBound.
	Bound BoundPolicy

	// Replacement, with ReplaceBound, is the administrator's explicit choice to
	// replace the plan's provider's bound client; nil reuses or refuses instead.
	Replacement *ClientReplacement

	// ResourceDisplay, when set, stores RFC 9728 display members on the bound
	// client.
	ResourceDisplay *ResourceDisplay
}

// ClientReplacement confirms replacing the client of the plan's provider.
type ClientReplacement struct {
	// ServerIDs lists exactly the other MCP servers the administrator was told
	// use the client. They move to the new client, and the commit is refused if
	// the list no longer matches.
	ServerIDs []uuid.UUID
}

// ProviderChoice is the Remote Identity Provider a plan configures.
type ProviderChoice struct {
	id     uuid.UUID
	create *repo.CreateRemoteSessionIssuerParams
}

// UseProvider configures a stored provider.
func UseProvider(id uuid.UUID) ProviderChoice {
	return ProviderChoice{id: id, create: nil}
}

// CreateProvider configures a new project-owned provider.
func CreateProvider(params repo.CreateRemoteSessionIssuerParams) ProviderChoice {
	return ProviderChoice{id: uuid.Nil, create: &params}
}

type clientChoiceKind int

const (
	clientLink clientChoiceKind = iota + 1
	clientManual
	clientRegister
)

// ClientChoice is how a plan gets the client it binds.
type ClientChoice struct {
	kind        clientChoiceKind
	linkID      uuid.UUID
	credentials ClientCredentials
	policy      RegistrationPolicy
}

// LinkClient binds a stored client of the plan's provider.
func LinkClient(id uuid.UUID) ClientChoice {
	return ClientChoice{kind: clientLink, linkID: id, credentials: ClientCredentials{}, policy: RegistrationPolicy{}} //nolint:exhaustruct // Unused for a linked client.
}

// ManualClient creates a client from credentials the provider issued out of
// band.
func ManualClient(credentials ClientCredentials) ClientChoice {
	return ClientChoice{kind: clientManual, linkID: uuid.Nil, credentials: credentials, policy: RegistrationPolicy{}} //nolint:exhaustruct // Unused for a manual client.
}

// RegisterClient obtains a client from the provider: a Speakeasy-hosted Client ID
// Metadata Document when the policy allows one and the provider supports it,
// otherwise dynamic client registration.
func RegisterClient(policy RegistrationPolicy) ClientChoice {
	return ClientChoice{kind: clientRegister, linkID: uuid.Nil, credentials: ClientCredentials{}, policy: policy} //nolint:exhaustruct // Registration fills the credentials.
}

// keep is the client a commit links again and therefore never unbinds.
func (c ClientChoice) keep() uuid.UUID {
	return conv.Ternary(c.kind == clientLink, c.linkID, uuid.Nil)
}

// RegistrationPolicy is how RegisterClient obtains a client.
type RegistrationPolicy struct {
	// Scope is requested at registration and authorization time.
	Scope []string

	// RegistrationResource is the protected resource a client without Scope
	// will discover at login. Registration names what that login would request
	// from it, so a provider that limits a client to its registered scopes
	// allows them; nil names what the provider alone decides. Never stored.
	RegistrationResource *ResourceScopes

	// Audience is sent as the resource/audience parameter, if set.
	Audience *string

	// TokenEndpointAuthMethod is the client authentication method asked of
	// the provider, if any.
	TokenEndpointAuthMethod *string

	// RequireClientSecret refuses a registration that is not a confidential
	// client using TokenEndpointAuthMethod.
	RequireClientSecret bool

	// AllowCIMD prefers a Client ID Metadata Document when the provider
	// supports one.
	AllowCIMD bool
}

// BoundPolicy is what a commit does with clients already bound to the user
// session issuer.
type BoundPolicy int

const (
	// ReplaceBound changes the plan's MCP server to the plan's client without
	// taking a client from another server: decideBoundClients says what happens
	// to each client bound to the issuer. A plan with no MCP server configures
	// the issuer alone and unbinds the clients replacedClients picks.
	ReplaceBound BoundPolicy = iota + 1

	// ReuseBound keeps a client already bound for the plan's provider, which
	// makes a retried commit idempotent.
	ReuseBound
)

// ResourceDisplay is a protected resource's RFC 9728 display members. They are
// stored only when Metadata names ResourceURL, since a document read from an
// origin-style well-known path may describe a sibling resource.
type ResourceDisplay struct {
	// ResourceURL is the protected resource the client serves.
	ResourceURL string

	// Metadata is the resource's protected resource metadata document.
	Metadata wellknown.OAuthProtectedResourceMetadata
}

// ClientCredentials are provider-issued client credentials, entered by hand or
// obtained through dynamic client registration.
type ClientCredentials struct {
	// ClientID is the provider-issued client_id.
	ClientID string

	// ClientSecret is encrypted before it is stored; empty for a public client.
	ClientSecret string

	// SecretExpiresAt is the provider-reported secret expiry, if any.
	SecretExpiresAt pgtype.Timestamptz

	// IssuedAt is when the provider says the credential began, if it said.
	IssuedAt pgtype.Timestamptz

	// TokenEndpointAuthMethod is the client authentication method, if known.
	TokenEndpointAuthMethod *string

	// Scope is requested at authorization time.
	Scope []string

	// Audience is sent as the resource/audience parameter, if set.
	Audience *string

	// TemplateClientID, when valid, is a stored client of the plan's provider
	// with the same ClientID whose upstream registration the new client
	// reuses: its stored secret, unless ClientSecret is set, and the callback
	// that registration lists. The template is only read.
	TemplateClientID uuid.NullUUID
}

// RegistrationMethod is how a commit got its client.
type RegistrationMethod string

const (
	RegistrationLinked RegistrationMethod = "existing"
	RegistrationManual RegistrationMethod = "manual"
	// RegistrationReused keeps the client already bound for the provider, so
	// ReuseBound registers nothing upstream.
	RegistrationReused RegistrationMethod = "reused"
	RegistrationCIMD   RegistrationMethod = RegistrationMethod(registration.MethodCIMD)
	RegistrationDCR    RegistrationMethod = RegistrationMethod(registration.MethodDCR)
)

// Registration is what Register produced. A provider refusing registration or
// needing manual setup is an outcome, not an error. Any client secret stays
// inside the commit.
type Registration struct {
	// Method is how the client is obtained; empty when manual setup is
	// required.
	Method RegistrationMethod

	// Provider is the stored provider; its ID is uuid.Nil when the plan
	// creates it.
	Provider repo.RemoteSessionIssuer

	// ManualSetupRequired reports a provider that supports neither a Client
	// ID Metadata Document nor dynamic registration.
	ManualSetupRequired bool

	// Failure is the classified refusal of a dynamic registration, if any.
	Failure *registration.Failure

	credentials ClientCredentials
}

// Ready reports whether Bind can use the registration.
func (r Registration) Ready() bool {
	return !r.ManualSetupRequired && r.Failure == nil
}

// IdentityResult is what a commit left in place.
type IdentityResult struct {
	// Provider is the provider the client belongs to.
	Provider repo.RemoteSessionIssuer

	// Client is the client bound to the user session issuer.
	Client repo.RemoteSessionClient

	// Bindings lists every user session issuer the client is bound to, sorted
	// by id.
	Bindings []uuid.UUID

	// Method is how the client was obtained.
	Method RegistrationMethod

	// ProviderCreated reports that the commit created Provider.
	ProviderCreated bool

	// ClientCreated reports that the commit created Client.
	ClientCreated bool

	// Reused reports that ReuseBound kept an already-bound client.
	Reused bool
}

// IdentityCommit is one prepared plan. Build one per request with
// IdentityCommitter.Prepare and run its steps in order.
type IdentityCommit struct {
	committer       *IdentityCommitter
	callbackBaseURL *pgtype.Text
	plan            IdentityPlan

	// provider is the stored provider: read by Preflight or Register, locked
	// by Lock, or created by Bind.
	provider           repo.RemoteSessionIssuer
	registration       Registration
	registeredUpstream bool
	orphanRecorded     bool
	// reusable reports that Preflight found the client ReuseBound keeps.
	reusable           bool
	locked             bool
	userIssuerOrgLevel bool
	result             IdentityResult
	// createCapabilities pins registration to the submitted form, before create-time discovery fills it in.
	createCapabilities providerCapabilities
}

// IdentityTx is the transaction a commit writes in.
type IdentityTx struct {
	commit    *IdentityCommit
	tx        pgx.Tx
	q         *repo.Queries
	committed bool
}

// Prepare builds the commit for plan.
func (c *IdentityCommitter) Prepare(plan IdentityPlan) *IdentityCommit {
	var commit IdentityCommit
	commit.committer = c
	commit.plan = plan
	if create := plan.Provider.create; create != nil {
		commit.createCapabilities = providerCapabilities{
			registrationEndpoint:              create.RegistrationEndpoint,
			tokenEndpointAuthMethodsSupported: slices.Clone(create.TokenEndpointAuthMethodsSupported),
			clientIDMetadataDocumentSupported: create.ClientIDMetadataDocumentSupported,
		}
	}
	return &commit
}

// Preflight applies every refusal the commit's writes enforce, as plain reads,
// so a plan that cannot land is refused before Register makes an upstream
// client. The writes repeat each check under their locks.
func (c *IdentityCommit) Preflight(ctx context.Context) error {
	q := repo.New(c.committer.db)
	issuer, err := q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             c.plan.UserSessionIssuerID,
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "user session issuer not found")
	}
	if err != nil {
		return fmt.Errorf("get user session issuer: %w", err)
	}
	c.userIssuerOrgLevel = !issuer.ProjectID.Valid

	if create := c.plan.Provider.create; create != nil {
		if _, err := q.GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{Slug: create.Slug, ProjectID: create.ProjectID}); err == nil {
			return identityRefusal(ErrIdentityConflict, nil, "an issuer with this slug already exists")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check remote session issuer slug: %w", err)
		}
	} else if err := c.readProvider(ctx, q); err != nil {
		return err
	}

	if c.plan.Client.kind == clientLink {
		if err := c.preflightLinkedClient(ctx, q); err != nil {
			return err
		}
	}
	if c.plan.Client.kind == clientManual && c.plan.Client.credentials.TemplateClientID.Valid {
		if _, err := c.loadTemplate(ctx, q, c.plan.Client.credentials); err != nil {
			return err
		}
	}

	bound, err := listUserSessionIssuerClients(ctx, q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, c.plan.UserSessionIssuerID)
	if err != nil {
		return err
	}
	switch c.plan.Bound {
	case ReplaceBound:
		// A new provider has no id yet, so every bound client belongs to another.
		decision, err := decideBoundClients(ctx, q, boundClientsRequest{
			OrganizationID:      c.plan.Scope.OrganizationID,
			UserSessionIssuerID: c.plan.UserSessionIssuerID,
			MCPServerID:         c.plan.MCPServerID,
			Bound:               bound,
			ProviderID:          c.provider.ID,
			Keep:                c.plan.Client.keep(),
			RegistersClient:     c.plan.Client.kind == clientRegister,
			ReplaceClient:       c.plan.Replacement != nil,
			Leaving:             false,
		})
		if err != nil {
			return err
		}
		if err := c.refuseUnconfirmedReplacement(ctx, mcpserversrepo.New(c.committer.db), decision); err != nil {
			return err
		}
		c.reusable = decision.reuse
		for _, client := range decision.detach {
			if err := c.refuseOrgWideBinding(client); err != nil {
				return err
			}
			if err := refuseLiveAgentBindings(ctx, q, c.plan.Scope, client.ID, c.plan.UserSessionIssuerID); err != nil {
				return err
			}
			if err := refuseIdentityChainingBindings(ctx, q, c.plan.Scope, client, c.plan.UserSessionIssuerID); err != nil {
				return err
			}
		}
	case ReuseBound:
		if c.provider.ID != uuid.Nil {
			switch countProviderClients(bound, c.provider.ID) {
			case 0:
			case 1:
				c.reusable = true
			default:
				return identityRefusal(ErrIdentityInvariant, nil, "more than one client is bound to this user session issuer for the same Remote Identity Provider")
			}
		}
	}
	return nil
}

func (c *IdentityCommit) preflightLinkedClient(ctx context.Context, q *repo.Queries) error {
	if c.plan.Provider.create != nil {
		return identityRefusal(ErrIdentityInvalid, nil, "an existing client requires an existing provider")
	}
	existing, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
		ID:             c.plan.Client.linkID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "remote session client not found")
	}
	if err != nil {
		return fmt.Errorf("get remote session client: %w", err)
	}
	if existing.RemoteSessionClient.RemoteSessionIssuerID != c.provider.ID {
		return identityRefusal(ErrIdentityInvalid, nil, "existing client does not belong to the selected provider")
	}
	if !slices.Contains(existing.UserSessionIssuerIds, c.plan.UserSessionIssuerID) {
		return c.refuseOrgWideBinding(existing.RemoteSessionClient)
	}
	return nil
}

// loadTemplate reads the template credentials names and refuses one that
// cannot supply this client's registration.
func (c *IdentityCommit) loadTemplate(ctx context.Context, q *repo.Queries, credentials ClientCredentials) (clientTemplate, error) {
	if c.plan.Provider.create != nil {
		return clientTemplate{}, identityRefusal(ErrIdentityInvalid, nil, "a template client requires an existing provider")
	}
	template, err := loadClientTemplate(ctx, q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, credentials.TemplateClientID.UUID, c.provider.ID, credentials.ClientID)
	if err != nil {
		return clientTemplate{}, err
	}
	if err := template.requireSecret(conv.PtrValOr(credentials.TokenEndpointAuthMethod, ""), credentials.ClientSecret != ""); err != nil {
		return clientTemplate{}, err
	}
	return template, nil
}

// readProvider loads the plan's stored provider once, for Preflight and
// Register.
func (c *IdentityCommit) readProvider(ctx context.Context, q *repo.Queries) error {
	if c.plan.Provider.create != nil || c.provider.ID != uuid.Nil {
		return nil
	}
	provider, err := q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    c.plan.Provider.id,
		ProjectID:             conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:        conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "Remote Identity Provider not found")
	}
	if err != nil {
		return fmt.Errorf("get Remote Identity Provider: %w", err)
	}
	c.provider = provider
	return nil
}

// Register obtains the plan's client. A linked or manual client passes
// through; RegisterClient uses a Client ID Metadata Document when allowed and
// supported, otherwise dynamic registration when the provider offers it, and
// otherwise reports that manual setup is required.
func (c *IdentityCommit) Register(ctx context.Context) (Registration, error) {
	if err := c.readProvider(ctx, repo.New(c.committer.db)); err != nil {
		return Registration{}, err
	}
	reg := Registration{Method: "", Provider: c.provider, ManualSetupRequired: false, Failure: nil, credentials: ClientCredentials{}} //nolint:exhaustruct // Filled per client choice below.
	switch {
	case c.reusable:
		// ReuseBound keeps the bound client; registering another would leave
		// it behind upstream.
		reg.Method = RegistrationReused
	case c.plan.Client.kind == clientLink:
		reg.Method = RegistrationLinked
	case c.plan.Client.kind == clientManual:
		reg.Method = RegistrationManual
		reg.credentials = c.plan.Client.credentials
	case c.plan.Client.kind == clientRegister:
		var err error
		reg, err = c.register(ctx, reg)
		if err != nil {
			return Registration{}, err
		}
	default:
		return Registration{}, errors.New("identity plan has no client choice")
	}
	// Resolve before Begin for manual and CIMD clients too: Bind must not
	// acquire a second pool connection while holding its transaction.
	if reg.Method == RegistrationManual || reg.Method == RegistrationCIMD {
		if _, err := c.newClientBaseURL(ctx); err != nil {
			return Registration{}, err
		}
	}
	c.registration = reg
	return reg, nil
}

func (c *IdentityCommit) register(ctx context.Context, reg Registration) (Registration, error) {
	policy := c.plan.Client.policy
	capabilities := c.capabilities()
	if policy.AllowCIMD && capabilities.supportsCIMD() {
		reg.Method = RegistrationCIMD
		return reg, nil
	}
	endpoint := strings.TrimSpace(capabilities.registrationEndpoint.String)
	if !capabilities.registrationEndpoint.Valid || endpoint == "" {
		reg.ManualSetupRequired = true
		return reg, nil
	}
	if !urls.IsAbsoluteHTTPSOrLoopback(endpoint) {
		return reg, identityRefusal(ErrIdentityInvalid, nil, "registration endpoint must be an absolute https URL, or http on loopback")
	}

	reg.Method = RegistrationDCR
	// Registering through a tunnel reaches a private network the project
	// cannot otherwise address, so it carries the same platform-admin gate the
	// management path applies when the binding is created.
	if c.provider.TunneledMcpServerID.Valid && !c.plan.Scope.ActorIsPlatformAdmin {
		return reg, identityRefusal(ErrIdentityForbidden, nil, "registering through an MCP tunnel requires a platform admin")
	}
	callbackBaseURL, err := c.newClientBaseURL(ctx)
	if err != nil {
		return reg, err
	}
	response, err := RegisterDynamicClient(ctx, c.committer.policy, c.committer.tunnels, c.committer.origins.ForClient(callbackBaseURL), ProxyRegisterRequest{
		RegistrationEndpoint:    endpoint,
		TunneledMcpServerID:     conv.PtrEmpty(tunnelBindingID(c.provider.TunneledMcpServerID)),
		Scope:                   conv.PtrEmpty(strings.Join(c.registrationScope(policy), " ")),
		TokenEndpointAuthMethod: policy.TokenEndpointAuthMethod,
	}, c.committer.telemetry)
	if err != nil {
		// The caller going away is not a registration outcome.
		if errors.Is(err, context.Canceled) {
			return reg, err
		}
		failure := registration.ClassifyDCR(err)
		reg.Failure = &failure
		return reg, nil
	}
	// The provider now holds a client; one this commit fails to store is left
	// behind upstream, since dynamic registration may have no delete API.
	c.registeredUpstream = true
	method, ok := registeredAuthMethod(response, policy)
	if !ok {
		failure := registration.InvalidSuccessResponse(0)
		if c.committer.telemetry != nil {
			c.committer.telemetry.RecordFailure(ctx, registration.MethodDCR, failure)
		}
		reg.Failure = &failure
		return reg, nil
	}
	reg.credentials = ClientCredentials{
		ClientID:                response.ClientID,
		ClientSecret:            response.ClientSecret,
		SecretExpiresAt:         response.ClientSecretExpiresAt,
		IssuedAt:                response.ClientIDIssuedAt,
		TokenEndpointAuthMethod: &method,
		Scope:                   policy.Scope,
		Audience:                policy.Audience,
		TemplateClientID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	return reg, nil
}

// registeredAuthMethod checks a dynamic registration response against the
// policy and returns the client authentication method to store.
func registeredAuthMethod(response ProxyRegisterResponse, policy RegistrationPolicy) (string, bool) {
	method := response.TokenEndpointAuthMethod
	if method == "" {
		method = conv.PtrValOr(policy.TokenEndpointAuthMethod, "")
	}
	if method == "" {
		method = conv.Ternary(response.ClientSecret == "", string(TokenEndpointAuthMethodNone), string(TokenEndpointAuthMethodBasic))
	}
	secretMethod := method == string(TokenEndpointAuthMethodBasic) || method == string(TokenEndpointAuthMethodPost)
	switch {
	case response.ClientID == "":
		return "", false
	case !secretMethod && method != string(TokenEndpointAuthMethodNone):
		return "", false
	case secretMethod && response.ClientSecret == "":
		return "", false
	case policy.RequireClientSecret && (!secretMethod || (policy.TokenEndpointAuthMethod != nil && method != *policy.TokenEndpointAuthMethod)):
		return "", false
	}
	return method, true
}

type providerCapabilities struct {
	registrationEndpoint              pgtype.Text
	tokenEndpointAuthMethodsSupported []string
	clientIDMetadataDocumentSupported bool
}

func (p providerCapabilities) supportsCIMD() bool {
	return SupportsClientIDMetadataDocument(p.clientIDMetadataDocumentSupported, p.tokenEndpointAuthMethodsSupported)
}

// capabilities is what registration is chosen from: the new provider's submitted
// parameters or the stored provider's row.
// registrationScope is the scope a dynamic registration names: the client's
// own, else what its first login would request through RequestedScopes.
func (c *IdentityCommit) registrationScope(policy RegistrationPolicy) []string {
	if len(policy.Scope) > 0 {
		return policy.Scope
	}
	resource := issuerLevelResource()
	if policy.RegistrationResource != nil {
		resource = *policy.RegistrationResource
	}
	if create := c.plan.Provider.create; create != nil {
		return issuerRegistrationScope(create.ScopesSupported, create.ScopeOverride, create.OmitScopeFallback, resource)
	}
	return issuerRegistrationScope(c.provider.ScopesSupported, c.provider.ScopeOverride, c.provider.OmitScopeFallback, resource)
}

// issuerLevelResource is a login that names no resource.
func issuerLevelResource() ResourceScopes {
	return ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: true}
}

// issuerRegistrationScope is what a login with no client scope requests from
// an issuer with these settings.
func issuerRegistrationScope(supported, override []string, omitFallback pgtype.Bool, resource ResourceScopes) []string {
	login := Client{ //nolint:exhaustruct // Only scope inputs are used by RequestedScopes.
		IssuerScopesSupported:   supported,
		IssuerScopeOverride:     override,
		IssuerOmitScopeFallback: omitFallback.Valid && omitFallback.Bool,
	}
	return login.RequestedScopes(resource).Scopes
}

func (c *IdentityCommit) capabilities() providerCapabilities {
	if c.plan.Provider.create != nil {
		return c.createCapabilities
	}
	return providerCapabilities{
		registrationEndpoint:              c.provider.RegistrationEndpoint,
		tokenEndpointAuthMethodsSupported: c.provider.TokenEndpointAuthMethodsSupported,
		clientIDMetadataDocumentSupported: c.provider.ClientIDMetadataDocumentSupported,
	}
}

// Begin opens the transaction and locks the complete source/server scope before
// callers can lock an issuer or mutate OAuth associations. Defer tx.Rollback.
func (c *IdentityCommit) Begin(ctx context.Context) (*IdentityTx, error) {
	tx, err := c.committer.db.Begin(ctx)
	if err != nil {
		c.recordOrphanedRegistration(ctx)
		return nil, fmt.Errorf("begin identity transaction: %w", err)
	}
	if err := authclientbackfill.LockEMAWriterScope(ctx, tx, c.plan.Scope.OrganizationID, []uuid.UUID{c.plan.Scope.ProjectID}); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		c.recordOrphanedRegistration(ctx)
		return nil, fmt.Errorf("lock identity writer scope: %w", err)
	}
	return &IdentityTx{commit: c, tx: tx, q: repo.New(tx), committed: false}, nil
}

// DB is the underlying transaction, for a caller's own locks and rechecks.
func (t *IdentityTx) DB() pgx.Tx {
	return t.tx
}

// Rollback rolls back an uncommitted transaction and, when a dynamic
// registration succeeded, records that its client is left behind upstream.
func (t *IdentityTx) Rollback(ctx context.Context) error {
	if !t.committed {
		t.commit.recordOrphanedRegistration(ctx)
	}
	if err := t.tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("roll back identity transaction: %w", err)
	}
	return nil
}

func (c *IdentityCommit) recordOrphanedRegistration(ctx context.Context) {
	if !c.registeredUpstream || c.orphanRecorded {
		return
	}
	c.orphanRecorded = true
	registration.RecordPostRegistrationCommitFailure(ctx, c.committer.telemetry, registration.MethodDCR)
}

// Lock serializes the commit's binding writes: the user session issuer first,
// as every client attachment path does, then a stored provider's
// client-binding lock and row. A caller that locks an MCP server row must do
// so before Lock, matching MCP server update and deletion. The locked provider
// is rechecked against what Register used.
func (c *IdentityCommit) Lock(ctx context.Context, tx *IdentityTx) error {
	if tx.commit != c {
		return fmt.Errorf("%w: identity transaction belongs to another commit", ErrIdentityStepOrder)
	}
	if err := c.lockMovedServers(ctx, tx); err != nil {
		return err
	}
	err := lockUserSessionIssuers(ctx, tx.tx, tx.q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, []uuid.UUID{c.plan.UserSessionIssuerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "user session issuer not found")
	}
	if err != nil {
		return err
	}
	issuer, err := tx.q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             c.plan.UserSessionIssuerID,
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("get locked user session issuer: %w", err)
	}
	c.userIssuerOrgLevel = !issuer.ProjectID.Valid
	// The user session issuer row comes before any client row, the order
	// identity-chaining preparation and client detach use.
	if _, err := tx.q.LockProjectUserIssuerForDetach(ctx, repo.LockProjectUserIssuerForDetachParams{
		ID:             c.plan.UserSessionIssuerID,
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "user session issuer not found")
	} else if err != nil {
		return fmt.Errorf("lock user session issuer row: %w", err)
	}

	if c.plan.Provider.create == nil {
		if err := c.lockProvider(ctx, tx); err != nil {
			return err
		}
	}
	c.locked = true
	return nil
}

// lockMovedServers locks the servers a replacement moves, before the issuer as
// server rows always are, in id order so two replacements agree.
func (c *IdentityCommit) lockMovedServers(ctx context.Context, tx *IdentityTx) error {
	if c.plan.Replacement == nil {
		return nil
	}
	serverIDs := slices.Clone(c.plan.Replacement.ServerIDs)
	slices.SortFunc(serverIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	q := mcpserversrepo.New(tx.tx)
	for _, serverID := range serverIDs {
		if _, err := q.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: c.plan.Scope.ProjectID}); errors.Is(err, pgx.ErrNoRows) {
			return identityRefusal(ErrIdentityConflict, err, replacementOtherProjectMessage)
		} else if err != nil {
			return fmt.Errorf("lock mcp server using replaced client: %w", err)
		}
	}
	return nil
}

// lockProvider takes the stored provider's client-binding lock and reads it
// FOR UPDATE. The binding lock comes first, the order every provider writer
// uses, so a concurrent provider edit cannot deadlock against this commit.
func (c *IdentityCommit) lockProvider(ctx context.Context, tx *IdentityTx) error {
	if err := tx.q.LockRemoteSessionIssuerForClientBinding(ctx, c.plan.Provider.id); err != nil {
		return fmt.Errorf("lock remote session issuer for client binding: %w", err)
	}
	current, err := tx.q.GetRemoteSessionIssuerByIDForConfigurationCommit(ctx, repo.GetRemoteSessionIssuerByIDForConfigurationCommitParams{
		ID:                    c.plan.Provider.id,
		ProjectID:             conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:        conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "Remote Identity Provider not found")
	}
	if err != nil {
		return fmt.Errorf("lock remote session issuer: %w", err)
	}
	// Register read the provider before this lock; refuse if what it
	// registered against, or how it reached the provider, has since changed.
	switch c.registration.Method {
	case RegistrationDCR:
		if current.RegistrationEndpoint != c.provider.RegistrationEndpoint || current.TunneledMcpServerID != c.provider.TunneledMcpServerID {
			return identityRefusal(ErrIdentityConflict, nil, "Remote Identity Provider changed while registering the client")
		}
	case RegistrationCIMD:
		if preflightCIMDIssuer(current) != nil {
			return identityRefusal(ErrIdentityConflict, nil, "Remote Identity Provider changed while preparing the client")
		}
	case RegistrationLinked, RegistrationManual, RegistrationReused:
	}
	c.provider = current
	return nil
}

// Bind writes the plan under the locks Lock took: it creates the provider if
// the plan says so, replaces or reuses the clients already bound, creates or
// links the client, stores its resource display and binds it to the user
// session issuer.
func (c *IdentityCommit) Bind(ctx context.Context, tx *IdentityTx, reg Registration) error {
	if tx.commit != c || !c.locked {
		return fmt.Errorf("%w: Bind before Lock", ErrIdentityStepOrder)
	}
	if !reg.Ready() {
		return errors.New("identity commit: Bind with a registration that is not ready")
	}
	if create := c.plan.Provider.create; create != nil {
		if err := c.createProvider(ctx, tx, *create); err != nil {
			return err
		}
	}

	var client repo.RemoteSessionClient
	var attached []uuid.UUID
	var moved []uuid.UUID
	reused := false
	switch c.plan.Bound {
	case ReplaceBound:
		bound, err := listUserSessionIssuerClients(ctx, tx.q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, c.plan.UserSessionIssuerID)
		if err != nil {
			return err
		}
		decision, err := decideBoundClients(ctx, tx.q, boundClientsRequest{
			OrganizationID:      c.plan.Scope.OrganizationID,
			UserSessionIssuerID: c.plan.UserSessionIssuerID,
			MCPServerID:         c.plan.MCPServerID,
			Bound:               bound,
			ProviderID:          c.provider.ID,
			Keep:                c.plan.Client.keep(),
			RegistersClient:     c.plan.Client.kind == clientRegister,
			ReplaceClient:       c.plan.Replacement != nil,
			Leaving:             false,
		})
		if err != nil {
			return err
		}
		if err := c.refuseUnconfirmedReplacement(ctx, mcpserversrepo.New(tx.tx), decision); err != nil {
			return err
		}
		moved = decision.moved
		for _, current := range decision.detach {
			if err := c.detachClient(ctx, tx, current); err != nil {
				return err
			}
		}
		if len(moved) > 0 {
			if err := c.releaseMovedServers(ctx, tx, decision); err != nil {
				return err
			}
		}
		for _, current := range decision.release {
			if err := c.releaseClient(ctx, tx, current); err != nil {
				return err
			}
		}
		if decision.reuse {
			row, found, err := c.boundClient(ctx, tx)
			if err != nil {
				return err
			}
			if found {
				client, attached, reused = row.RemoteSessionClient, row.UserSessionIssuerIds, true
				// Another commit bound this provider's client after Register
				// made one upstream, which is now left behind there.
				c.recordOrphanedRegistration(ctx)
			}
		}
	case ReuseBound:
		row, found, err := c.boundClient(ctx, tx)
		if err != nil {
			return err
		}
		if found {
			client, attached, reused = row.RemoteSessionClient, row.UserSessionIssuerIds, true
		}
	}

	if !reused {
		var err error
		switch reg.Method {
		case RegistrationLinked:
			client, attached, err = c.linkClient(ctx, tx)
		case RegistrationCIMD:
			client, err = c.createCIMDClient(ctx, tx)
		case RegistrationManual, RegistrationDCR:
			client, err = c.createClient(ctx, tx, reg.credentials)
		case RegistrationReused:
			err = identityRefusal(ErrIdentityConflict, nil, "the Remote Identity Provider's client changed while preparing the request; retry")
		default:
			err = fmt.Errorf("identity commit: unknown registration method %q", reg.Method)
		}
		if err != nil {
			return err
		}
	}

	if display := c.plan.ResourceDisplay; display != nil {
		if err := c.storeResourceDisplay(ctx, tx, client, *display); err != nil {
			return err
		}
	}
	bindings, err := c.attach(ctx, tx, client, attached)
	if err != nil {
		return err
	}
	if c.plan.MCPServerID != uuid.Nil {
		associated, err := upstreamclientsrepo.New(tx.tx).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
			OrganizationID:        c.plan.Scope.OrganizationID,
			RemoteSessionClientID: client.ID,
			McpServerID:           c.plan.MCPServerID,
			ProjectID:             c.plan.Scope.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("associate mcp server with remote session client: %w", err)
		}
		if associated == 0 {
			return identityRefusal(ErrIdentityConflict, nil, "the MCP server or its client changed while saving; retry")
		}
		for _, serverID := range moved {
			movedAssociated, err := upstreamclientsrepo.New(tx.tx).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
				OrganizationID:        c.plan.Scope.OrganizationID,
				RemoteSessionClientID: client.ID,
				McpServerID:           serverID,
				ProjectID:             c.plan.Scope.ProjectID,
			})
			if err != nil {
				return fmt.Errorf("move mcp server to replacement client: %w", err)
			}
			if movedAssociated == 0 {
				return identityRefusal(ErrIdentityConflict, nil, replacementNotConfirmedMessage)
			}
		}
		// Begin acquired the full writer scope before the binding/association
		// mutations. Synchronize typed clients and both OAuth and EMA copies
		// under those locks; taking that scope here would be too late.
		if err := authclientbackfill.SyncServerAssociations(ctx, tx.tx, c.plan.Scope.OrganizationID, append([]uuid.UUID{c.plan.MCPServerID}, moved...)...); err != nil {
			return fmt.Errorf("sync mcp server auth clients: %w", err)
		}
	}
	c.result = IdentityResult{
		Provider:        c.provider,
		Client:          client,
		Bindings:        bindings,
		Method:          reg.Method,
		ProviderCreated: c.plan.Provider.create != nil,
		ClientCreated:   !reused && reg.Method != RegistrationLinked,
		Reused:          reused,
	}
	return nil
}

// Commit commits, then restamps mcp_servers.remote_session_issuer_id for the
// user session issuer. The restamp is best effort and runs after the commit,
// like every other client binding write: restamping inside the transaction
// would write MCP server rows while holding the user session issuer lock, the
// reverse of MCP server update and deletion, which lock the server row first,
// so the two could deadlock. A failed restamp is logged and leaves a stale
// value the next resync heals.
func (c *IdentityCommit) Commit(ctx context.Context, tx *IdentityTx) (IdentityResult, error) {
	if tx.commit != c || !c.locked {
		return IdentityResult{}, fmt.Errorf("%w: Commit before Lock", ErrIdentityStepOrder)
	}
	if err := tx.tx.Commit(ctx); err != nil {
		return IdentityResult{}, fmt.Errorf("commit identity transaction: %w", err)
	}
	tx.committed = true
	BestEffortResyncMCPServerRemoteSessionIssuers(ctx, c.committer.logger, c.committer.db, c.plan.Scope.OrganizationID, c.plan.Scope.ProjectID, []uuid.UUID{c.plan.UserSessionIssuerID})
	return c.result, nil
}

// replacedClients picks, from the clients bound to the user session issuer,
// the ones a commit for providerID unbinds. The server's current Remote
// Identity Provider comes from those live bindings, not the best-effort
// mcp_servers.remote_session_issuer_id copy, which can be NULL or stale: it is
// the one provider other than providerID with a bound client, or providerID
// itself when no other has one. Its clients are replaced, except keep. A user
// session issuer may bind distinct clients for distinct providers, so two or
// more other providers leave the replacement ambiguous and are refused, as is
// a client of providerID other than keep that would stay bound beside the new
// one. providerID is uuid.Nil for a provider the commit creates.
func replacedClients(bound []repo.RemoteSessionClient, providerID, keep uuid.UUID) ([]repo.RemoteSessionClient, error) {
	var others, same []repo.RemoteSessionClient
	for _, client := range bound {
		if client.ID == keep {
			continue
		}
		switch {
		case client.RemoteSessionIssuerID == providerID:
			same = append(same, client)
		case len(others) > 0 && client.RemoteSessionIssuerID != others[0].RemoteSessionIssuerID:
			return nil, identityRefusal(ErrIdentityConflict, nil, ambiguousCurrentProviderMessage)
		default:
			others = append(others, client)
		}
	}
	if len(others) == 0 {
		return same, nil
	}
	if len(same) > 0 {
		return nil, identityRefusal(ErrIdentityConflict, nil, clientTakenMessage)
	}
	return others, nil
}

// boundClientsRequest is what decideBoundClients decides from.
type boundClientsRequest struct {
	// OrganizationID scopes the reads.
	OrganizationID string

	// UserSessionIssuerID is the issuer the clients are bound to.
	UserSessionIssuerID uuid.UUID

	// MCPServerID is the server being configured; uuid.Nil configures the
	// issuer alone.
	MCPServerID uuid.UUID

	// Bound lists the clients bound to the issuer that the project can see.
	Bound []repo.RemoteSessionClient

	// ProviderID is the plan's provider; uuid.Nil is one the commit creates.
	ProviderID uuid.UUID

	// Keep is the client the commit links, if any.
	Keep uuid.UUID

	// RegistersClient reports that the plan asks for a client automatically,
	// without naming one or supplying credentials.
	RegistersClient bool

	// ReplaceClient reports the administrator's explicit choice to replace the
	// plan's provider's bound client.
	ReplaceClient bool

	// Leaving reports that the server is giving up User Identity altogether, so
	// no client of its own stands in for the others, no choice between providers
	// is ambiguous, and an unclaimed client it would otherwise fall back to
	// requiring has to go too.
	Leaving bool
}

// boundClientsDecision is what a commit does with the clients bound to the user
// session issuer. A client in none of its lists is left as it is.
type boundClientsDecision struct {
	// detach lists clients only the configured server uses, which the commit
	// unbinds.
	detach []repo.RemoteSessionClient

	// release lists clients other servers also use. They stay bound and only
	// the configured server's association with them is removed.
	release []repo.RemoteSessionClient

	// reuse reports that the plan's provider already has a client on the issuer
	// and the commit uses it rather than making another.
	reuse bool

	// moved lists the other MCP servers that use a client being replaced, which
	// move to the new client.
	moved []uuid.UUID

	// replaced is the client being replaced, whose servers are in moved; uuid.Nil
	// when none is.
	replaced uuid.UUID
}

// decideBoundClients decides what changing one MCP server to the plan's client
// does to the clients bound to its user session issuer. A binding is shared by
// every consumer of the issuer, so the server's current clients come from its
// own associations, and no client another consumer uses is ever unbound.
//
//   - A client of another provider is released when others use it and detached
//     when they do not. One this server does not use is left alone.
//   - A client of the plan's provider is reused when the plan asks for a client
//     automatically and is kept when the plan links it. A plan naming a
//     different client replaces it only when this server alone uses it, since an
//     issuer keeps one client per provider. Otherwise the plan is refused,
//     because the choice between them is the administrator's.
//   - ReplaceClient is that choice: the client is replaced and the servers that
//     use it move to the new one. It is refused when a consumer without an
//     association shares the issuer, since nothing can say what it would lose.
//
// Asking for a client automatically never replaces a per-user client unless
// ReplaceClient says so: only credentials or a client the administrator named can
// do that. A service account client is not one it can reuse, so it follows the
// rule for a plan naming a different client.
//
// A server leaving User Identity also gives up the clients no server claims,
// which its callers would otherwise fall back to needing. Its siblings on the
// issuer may fall back to the same client, so unless the server is the issuer's
// only consumer the client stays bound and only this server's detach is recorded.
func decideBoundClients(ctx context.Context, q *repo.Queries, req boundClientsRequest) (boundClientsDecision, error) {
	var decision boundClientsDecision
	if req.MCPServerID == uuid.Nil {
		replaced, err := replacedClients(req.Bound, req.ProviderID, req.Keep)
		if err != nil {
			return decision, err
		}
		decision.detach = replaced
		return decision, nil
	}
	if len(req.Bound) == 0 {
		return decision, nil
	}

	ids := make([]uuid.UUID, 0, len(req.Bound))
	for _, client := range req.Bound {
		ids = append(ids, client.ID)
	}
	uses, err := q.DescribeBoundClientUse(ctx, repo.DescribeBoundClientUseParams{
		McpServerID:            req.MCPServerID,
		OrganizationID:         req.OrganizationID,
		UserSessionIssuerID:    req.UserSessionIssuerID,
		RemoteSessionClientIds: ids,
	})
	if err != nil {
		return decision, fmt.Errorf("describe bound client use: %w", err)
	}
	consumers, err := q.DescribeUserSessionIssuerConsumers(ctx, repo.DescribeUserSessionIssuerConsumersParams{
		UserSessionIssuerID: req.UserSessionIssuerID,
		McpServerID:         req.MCPServerID,
		OrganizationID:      req.OrganizationID,
	})
	if err != nil {
		return decision, fmt.Errorf("describe user session issuer consumers: %w", err)
	}
	use := make(map[uuid.UUID]repo.DescribeBoundClientUseRow, len(uses))
	for _, row := range uses {
		use[row.RemoteSessionClientID] = row
	}
	// The issuer's only consumer owns its bindings whether or not an association
	// records it.
	ownClient := func(id uuid.UUID) bool {
		return use[id].AssociatedWithMcpServer || consumers.SoleConsumer || (req.Leaving && clientUnclaimed(use[id]))
	}
	sharedClient := func(id uuid.UUID) bool {
		return len(use[id].OtherMcpServerIds) > 0 || consumers.ReadsBindings || (!use[id].AssociatedWithMcpServer && !consumers.SoleConsumer)
	}

	if countProviderClients(req.Bound, req.ProviderID) > 1 {
		return decision, identityRefusal(ErrIdentityInvariant, nil, "more than one client is bound to this user session issuer for the same Remote Identity Provider")
	}
	var detachProvider uuid.UUID
	for _, client := range req.Bound {
		switch {
		case client.ID == req.Keep:
		case client.RemoteSessionIssuerID == req.ProviderID && req.ReplaceClient:
			if consumers.ReadsBindings {
				return boundClientsDecision{}, identityRefusal(ErrIdentityConflict, nil, replacementReadsBindingsMessage)
			}
			decision.detach = append(decision.detach, client)
			decision.moved = use[client.ID].OtherMcpServerIds
			decision.replaced = client.ID
		case client.RemoteSessionIssuerID == req.ProviderID && req.RegistersClient && CredentialOwner(client.CredentialOwner) != CredentialOwnerSelf:
			// A service account client is one credential every caller shares, so
			// it does not serve a server asking for per-user sign-in.
			decision.reuse = true
		case client.RemoteSessionIssuerID == req.ProviderID:
			if !ownClient(client.ID) || sharedClient(client.ID) {
				return boundClientsDecision{}, identityRefusal(ErrIdentityConflict, nil, providerClientInUseMessage)
			}
			decision.detach = append(decision.detach, client)
		case !ownClient(client.ID):
		case sharedClient(client.ID):
			decision.release = append(decision.release, client)
		default:
			if !req.Leaving && detachProvider != uuid.Nil && detachProvider != client.RemoteSessionIssuerID {
				return boundClientsDecision{}, identityRefusal(ErrIdentityConflict, nil, ambiguousCurrentProviderMessage)
			}
			detachProvider = client.RemoteSessionIssuerID
			decision.detach = append(decision.detach, client)
		}
	}
	return decision, nil
}

// refuseUnconfirmedReplacement refuses a replacement unless the administrator
// confirmed exactly the servers it moves, and they are all in the project the
// commit's writer scope covers.
func (c *IdentityCommit) refuseUnconfirmedReplacement(ctx context.Context, q *mcpserversrepo.Queries, decision boundClientsDecision) error {
	if c.plan.Replacement == nil {
		return nil
	}
	confirmed := slices.Clone(c.plan.Replacement.ServerIDs)
	moved := slices.Clone(decision.moved)
	compare := func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }
	slices.SortFunc(confirmed, compare)
	slices.SortFunc(moved, compare)
	if !slices.Equal(confirmed, moved) {
		return identityRefusal(ErrIdentityConflict, nil, replacementNotConfirmedMessage)
	}
	return refuseMovedServersOutsideProject(ctx, q, c.plan.Scope.ProjectID, moved)
}

// refuseMovedServersOutsideProject refuses a replacement that would move a server
// in another project, which the commit's writer scope does not reach.
func refuseMovedServersOutsideProject(ctx context.Context, q *mcpserversrepo.Queries, projectID uuid.UUID, moved []uuid.UUID) error {
	for _, serverID := range moved {
		if _, err := q.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID}); errors.Is(err, pgx.ErrNoRows) {
			return identityRefusal(ErrIdentityConflict, err, replacementOtherProjectMessage)
		} else if err != nil {
			return fmt.Errorf("get mcp server using replaced client: %w", err)
		}
	}
	return nil
}

// releaseMovedServers removes the associations a detached client can no longer
// serve, in the transaction so the moved servers never hold both clients, and
// records each server the replacement took the client from.
func (c *IdentityCommit) releaseMovedServers(ctx context.Context, tx *IdentityTx, decision boundClientsDecision) error {
	if _, err := upstreamclientsrepo.New(tx.tx).PruneRemoteSessionClientAssociations(ctx, upstreamclientsrepo.PruneRemoteSessionClientAssociationsParams{
		RemoteSessionClientID: decision.replaced,
		OrganizationID:        c.plan.Scope.OrganizationID,
	}); err != nil {
		return fmt.Errorf("prune replaced client associations: %w", err)
	}
	replaced, err := tx.q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
		ID:             decision.replaced,
	})
	if err != nil {
		return fmt.Errorf("get replaced client: %w", err)
	}
	q := mcpserversrepo.New(tx.tx)
	for _, serverID := range decision.moved {
		server, err := q.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: c.plan.Scope.ProjectID})
		if err != nil {
			return fmt.Errorf("get moved mcp server: %w", err)
		}
		if err := c.committer.audit.LogRemoteSessionClientDetachMcpServer(ctx, tx.tx, audit.LogRemoteSessionClientDetachMcpServerEvent{
			OrganizationID:         c.plan.Scope.OrganizationID,
			ProjectID:              c.plan.Scope.ProjectID,
			Actor:                  c.plan.Scope.Actor,
			ActorDisplayName:       c.plan.Scope.ActorDisplayName,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(replaced.RemoteSessionClient.ID),
			ClientID:               replaced.RemoteSessionClient.ClientID,
			McpServerURN:           urn.NewMcpServer(server.ID),
			McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
		}); err != nil {
			return fmt.Errorf("audit moved mcp server: %w", err)
		}
	}
	return nil
}

// releaseClient removes the configured server's association with a client that
// stays bound for other servers. The row is soft-deleted, the record of an
// explicit detach, so capture does not hand the client back to the server. Bind
// synchronizes the server's typed auth clients afterwards.
func (c *IdentityCommit) releaseClient(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient) error {
	if _, err := upstreamclientsrepo.New(tx.tx).DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           c.plan.MCPServerID,
		RemoteSessionClientID: client.ID,
		ProjectID:             c.plan.Scope.ProjectID,
	}); err != nil {
		return fmt.Errorf("release client from mcp server: %w", err)
	}
	server, err := mcpserversrepo.New(tx.tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        c.plan.MCPServerID,
		ProjectID: c.plan.Scope.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("get mcp server releasing client: %w", err)
	}
	if err := c.committer.audit.LogRemoteSessionClientDetachMcpServer(ctx, tx.tx, audit.LogRemoteSessionClientDetachMcpServerEvent{
		OrganizationID:         c.plan.Scope.OrganizationID,
		ProjectID:              c.plan.Scope.ProjectID,
		Actor:                  c.plan.Scope.Actor,
		ActorDisplayName:       c.plan.Scope.ActorDisplayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		McpServerURN:           urn.NewMcpServer(server.ID),
		McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
	}); err != nil {
		return fmt.Errorf("audit client release: %w", err)
	}
	return nil
}

func countProviderClients(bound []repo.RemoteSessionClient, providerID uuid.UUID) int {
	n := 0
	for _, client := range bound {
		if client.RemoteSessionIssuerID == providerID {
			n++
		}
	}
	return n
}

// listUserSessionIssuerClients pages through every client bound to
// userIssuerID, whatever its Remote Identity Provider.
func listUserSessionIssuerClients(ctx context.Context, q *repo.Queries, projectID uuid.UUID, organizationID string, userIssuerID uuid.UUID) ([]repo.RemoteSessionClient, error) {
	const pageSize = 100
	var clients []repo.RemoteSessionClient
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	for {
		page, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
			UserSessionIssuerID:   userIssuerID,
			ProjectID:             projectID,
			OrganizationID:        organizationID,
			RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Cursor:                cursor,
			LimitValue:            pageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("list clients bound to user session issuer: %w", err)
		}
		for _, row := range page {
			clients = append(clients, row.RemoteSessionClient)
		}
		if len(page) < pageSize {
			return clients, nil
		}
		cursor = uuid.NullUUID{UUID: page[len(page)-1].RemoteSessionClient.ID, Valid: true}
	}
}

// refuseOrgWideBinding refuses to link or unlink client when that binding is
// shared across projects and the actor is not an organization admin. A
// project-owned client is invisible to other projects, so its binding only
// ever changes the scope's own project.
func (c *IdentityCommit) refuseOrgWideBinding(client repo.RemoteSessionClient) error {
	return refuseOrgWideBinding(c.userIssuerOrgLevel, c.plan.Scope.ActorIsOrganizationAdmin, client.ProjectID)
}

func refuseOrgWideBinding(userIssuerOrgLevel, actorIsOrganizationAdmin bool, clientProjectID uuid.NullUUID) error {
	if userIssuerOrgLevel && !clientProjectID.Valid && !actorIsOrganizationAdmin {
		return identityRefusal(ErrIdentityOrgWideBinding, nil, OrgWideBindingMessage)
	}
	return nil
}

// refuseLiveAgentBindings refuses to unbind a client that unrevoked agent
// attachments go through: deleting its binding would cascade-delete them
// unaudited.
func refuseLiveAgentBindings(ctx context.Context, q *repo.Queries, scope IdentityScope, clientID, userIssuerID uuid.UUID) error {
	live, err := q.HasLivePrincipalRemoteSessionBindingsForClientBinding(ctx, repo.HasLivePrincipalRemoteSessionBindingsForClientBindingParams{
		OrganizationID:        scope.OrganizationID,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	})
	if err != nil {
		return fmt.Errorf("check agent attachments through client binding: %w", err)
	}
	if live {
		return identityRefusal(ErrIdentityConflict, nil, agentBindingsOnReplacedClientMessage)
	}
	return nil
}

func (c *IdentityCommit) createProvider(ctx context.Context, tx *IdentityTx, params repo.CreateRemoteSessionIssuerParams) error {
	provider, err := tx.q.CreateRemoteSessionIssuer(ctx, params)
	if err != nil {
		if isRemoteSessionIssuerSlugConflict(err) {
			return identityRefusal(ErrIdentityConflict, err, "an issuer with this slug already exists")
		}
		return fmt.Errorf("create remote session issuer: %w", err)
	}
	if err := c.committer.audit.LogRemoteSessionIssuerCreate(ctx, tx.tx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:         c.plan.Scope.OrganizationID,
		ProjectID:              c.plan.Scope.ProjectID,
		Actor:                  c.plan.Scope.Actor,
		ActorDisplayName:       c.plan.Scope.ActorDisplayName,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(provider.ID),
		Slug:                   provider.Slug,
		IssuerURL:              provider.Issuer,
		Name:                   conv.FromPGText[string](provider.Name),
	}); err != nil {
		return fmt.Errorf("audit remote session issuer creation: %w", err)
	}
	if err := tx.q.LockRemoteSessionIssuerForClientBinding(ctx, provider.ID); err != nil {
		return fmt.Errorf("lock remote session issuer for client binding: %w", err)
	}
	c.provider = provider
	return nil
}

// boundClient returns the client bound to the user session issuer for the
// commit's provider, and whether there is one. Every identity write keeps that
// pair to a single client, so finding more is refused.
func (c *IdentityCommit) boundClient(ctx context.Context, tx *IdentityTx) (repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerRow, bool, error) {
	var none repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerRow
	// Two rows are enough to tell none, one and too many apart.
	bound, err := tx.q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		UserSessionIssuerID:   c.plan.UserSessionIssuerID,
		ProjectID:             c.plan.Scope.ProjectID,
		OrganizationID:        c.plan.Scope.OrganizationID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: c.provider.ID, Valid: true},
		Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:            2,
	})
	if err != nil {
		return none, false, fmt.Errorf("list clients bound to user session issuer: %w", err)
	}
	if len(bound) > 1 {
		return none, false, identityRefusal(ErrIdentityInvariant, nil, "more than one client is bound to this user session issuer for the same Remote Identity Provider")
	}
	if len(bound) == 0 {
		return none, false, nil
	}
	return bound[0], true, nil
}

// detachClient unbinds client from the user session issuer. It refuses an
// organization-wide binding, a binding that live agent attachments go through,
// and one identity-chaining bindings reference. It locks the client row, then
// the join row an agent attachment's foreign key check reads, after the user
// session issuer row Lock took: the order client detach and identity-chaining
// preparation use.
func (c *IdentityCommit) detachClient(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient) error {
	if err := c.refuseOrgWideBinding(client); err != nil {
		return err
	}
	if _, err := tx.q.LockEMAClient(ctx, repo.LockEMAClientParams{
		ID:             client.ID,
		ProjectID:      conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID: conv.ToPGText(c.plan.Scope.OrganizationID),
	}); errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "remote session client not found")
	} else if err != nil {
		return fmt.Errorf("lock remote session client: %w", err)
	}
	if err := tx.q.LockRemoteSessionClientUserSessionIssuerLink(ctx, repo.LockRemoteSessionClientUserSessionIssuerLinkParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   c.plan.UserSessionIssuerID,
	}); err != nil {
		return fmt.Errorf("lock client binding to user session issuer: %w", err)
	}
	if err := refuseLiveAgentBindings(ctx, tx.q, c.plan.Scope, client.ID, c.plan.UserSessionIssuerID); err != nil {
		return err
	}
	if c.plan.MCPServerID != uuid.Nil {
		// The decision read other projects' associations before this client's
		// lock, which their capture takes a share of, so read them again here.
		uses, err := tx.q.DescribeBoundClientUse(ctx, repo.DescribeBoundClientUseParams{
			McpServerID:            c.plan.MCPServerID,
			OrganizationID:         c.plan.Scope.OrganizationID,
			UserSessionIssuerID:    c.plan.UserSessionIssuerID,
			RemoteSessionClientIds: []uuid.UUID{client.ID},
		})
		if err != nil {
			return fmt.Errorf("recheck bound client use: %w", err)
		}
		var confirmed []uuid.UUID
		if c.plan.Replacement != nil {
			confirmed = c.plan.Replacement.ServerIDs
		}
		for _, use := range uses {
			for _, serverID := range use.OtherMcpServerIds {
				if !slices.Contains(confirmed, serverID) {
					return identityRefusal(ErrIdentityConflict, nil, "another MCP server started using this client while saving; retry")
				}
			}
		}
	}
	affected, err := tx.q.DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   c.plan.UserSessionIssuerID,
		ProjectID:             c.plan.Scope.ProjectID,
		OrganizationID:        c.plan.Scope.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("detach client from user session issuer: %w", err)
	}
	if affected == 0 {
		return nil
	}
	// Checked after the delete, like client detach: a refusal rolls the
	// tentative removal back with the transaction.
	if err := refuseIdentityChainingBindings(ctx, tx.q, c.plan.Scope, client, c.plan.UserSessionIssuerID); err != nil {
		return err
	}
	if err := c.committer.audit.LogRemoteSessionClientDetachUserSessionIssuer(ctx, tx.tx, c.attachmentEvent(client)); err != nil {
		return fmt.Errorf("audit client detachment: %w", err)
	}
	// The project's associations the detached client no longer serves, and
	// the typed links derived from them, go in the transaction rather than
	// with the post-commit resync. Begin locked this project's scope only, so
	// servers of other projects on an organization-level issuer are left to
	// that resync.
	if err := upstreamclients.ReconcileInLockedTransaction(ctx, tx.tx, c.plan.Scope.OrganizationID, c.plan.Scope.ProjectID, nil); err != nil {
		return fmt.Errorf("prune detached client associations: %w", err)
	}
	return nil
}

// refuseIdentityChainingBindings refuses to unbind a client that active
// identity-chaining bindings reference. An organization-owned client can carry
// a shared organization user session issuer link, so every project using it
// is checked.
func refuseIdentityChainingBindings(ctx context.Context, q *repo.Queries, scope IdentityScope, client repo.RemoteSessionClient, userIssuerID uuid.UUID) error {
	projectID := scope.ProjectID
	if !client.ProjectID.Valid {
		projectID = uuid.Nil
	}
	count, err := q.CountActiveEMABindingsForClientUserIssuer(ctx, repo.CountActiveEMABindingsForClientUserIssuerParams{
		ClientID:            conv.ToNullUUID(client.ID),
		UserSessionIssuerID: userIssuerID,
		OrganizationID:      scope.OrganizationID,
		ProjectID:           projectID,
	})
	if err != nil {
		return fmt.Errorf("check identity-chaining bindings: %w", err)
	}
	if count > 0 {
		return identityRefusal(ErrIdentityConflict, nil, identityChainingBindingsMessage)
	}
	return nil
}

// linkClient loads the plan's stored client and the user session issuers it is
// already bound to.
func (c *IdentityCommit) linkClient(ctx context.Context, tx *IdentityTx) (repo.RemoteSessionClient, []uuid.UUID, error) {
	existing, err := tx.q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      c.plan.Scope.ProjectID,
		OrganizationID: c.plan.Scope.OrganizationID,
		ID:             c.plan.Client.linkID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RemoteSessionClient{}, nil, identityRefusal(ErrIdentityNotFound, err, "remote session client not found")
	}
	if err != nil {
		return repo.RemoteSessionClient{}, nil, fmt.Errorf("get remote session client: %w", err)
	}
	if existing.RemoteSessionClient.RemoteSessionIssuerID != c.provider.ID {
		return repo.RemoteSessionClient{}, nil, identityRefusal(ErrIdentityConflict, nil, "existing client no longer belongs to the selected provider")
	}
	return existing.RemoteSessionClient, existing.UserSessionIssuerIds, nil
}

// createCIMDClient creates a client identified by a Speakeasy-hosted Client ID
// Metadata Document.
func (c *IdentityCommit) createCIMDClient(ctx context.Context, tx *IdentityTx) (repo.RemoteSessionClient, error) {
	// Generated here so the document URL, which embeds the id, can be the
	// client_id on a single insert.
	id, err := uuid.NewV7()
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("generate client id: %w", err)
	}
	policy := c.plan.Client.policy
	callbackBaseURL, err := c.newClientBaseURL(ctx)
	if err != nil {
		return repo.RemoteSessionClient{}, err
	}
	client, err := tx.q.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ForwardClientName:     false,
		ID:                    id,
		ProjectID:             conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:        conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		RemoteSessionIssuerID: c.provider.ID,
		ClientIDMetadataUri:   ClientMetadataDocumentURL(c.committer.origins.ForClient(callbackBaseURL), id),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 policy.Scope,
		Audience:              conv.PtrToPGText(policy.Audience),
		CallbackBaseUrl:       callbackBaseURL,
	})
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("create cimd client: %w", err)
	}
	return client, c.auditClientCreate(ctx, tx, client)
}

// createClient creates a client from provider-issued credentials.
func (c *IdentityCommit) createClient(ctx context.Context, tx *IdentityTx, credentials ClientCredentials) (repo.RemoteSessionClient, error) {
	callbackBaseURL, err := c.newClientBaseURL(ctx)
	if err != nil {
		return repo.RemoteSessionClient{}, err
	}
	var secret pgtype.Text
	if credentials.ClientSecret != "" {
		encrypted, err := c.committer.enc.Encrypt([]byte(credentials.ClientSecret))
		if err != nil {
			return repo.RemoteSessionClient{}, fmt.Errorf("encrypt client secret: %w", err)
		}
		secret = conv.ToPGText(encrypted)
	}
	// Keep what the provider said it issued: rewriting it to our own clock
	// loses the only record of when the credential began, which is what a
	// rotation window is measured against. RFC 7591 makes it optional.
	clientIDIssuedAt := conv.ToPGTimestamptz(time.Now().UTC())
	if credentials.IssuedAt.Valid {
		clientIDIssuedAt = credentials.IssuedAt
	}
	secretExpiresAt := credentials.SecretExpiresAt
	legacyCallbackURL := false
	if credentials.TemplateClientID.Valid {
		// Read again and share-lock the row for the rest of the transaction:
		// the copy is of the row stored now, and a template that stopped
		// qualifying since Preflight is refused.
		template, err := c.loadTemplate(ctx, tx.q, credentials)
		if err != nil {
			return repo.RemoteSessionClient{}, err
		}
		if !secret.Valid {
			secret = template.secretEncrypted
			secretExpiresAt = template.clientSecretExpiresAt
		}
		clientIDIssuedAt = template.clientIDIssuedAt
		callbackBaseURL = template.callbackBaseURL
		legacyCallbackURL = template.legacyCallbackURL
	}
	client, err := tx.q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ForwardClientName:               false,
		ProjectID:                       conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:                  conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		RemoteSessionIssuerID:           c.provider.ID,
		ClientID:                        strings.TrimSpace(credentials.ClientID),
		ClientSecretEncrypted:           secret,
		ClientIDIssuedAt:                clientIDIssuedAt,
		ClientSecretExpiresAt:           secretExpiresAt,
		TokenEndpointAuthAudienceFormat: pgtype.Text{String: "", Valid: false},
		// Neither is set here; a JWKS is attached afterwards through the
		// client JWKS API.
		JsonWebKeySetID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TokenEndpointAuthMethod:      conv.PtrToPGText(credentials.TokenEndpointAuthMethod),
		Scope:                        credentials.Scope,
		Audience:                     conv.PtrToPGText(credentials.Audience),
		LegacyCallbackUrl:            legacyCallbackURL,
		CallbackBaseUrl:              callbackBaseURL,
		GrantTypes:                   nil,
		CredentialOwner:              pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("create client: %w", err)
	}
	if credentials.TemplateClientID.Valid {
		return client, c.auditClientCopy(ctx, tx, client, credentials.TemplateClientID.UUID)
	}
	return client, c.auditClientCreate(ctx, tx, client)
}

// storeResourceDisplay writes the resource's RFC 9728 display members onto its
// client, but only when the document names the resource. An
// organization-owned client is shared by every project, so a project-scoped
// commit leaves it alone.
func (c *IdentityCommit) storeResourceDisplay(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient, display ResourceDisplay) error {
	if !display.Metadata.IdentifiesResource(display.ResourceURL) || !client.ProjectID.Valid {
		return nil
	}
	if _, err := tx.q.UpdateRemoteSessionClientResourceDisplay(ctx, repo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(display.ResourceURL),
		ResourceName:          display.Metadata.ResourceName,
		ResourceDocumentation: display.Metadata.ResourceDocumentation,
		ResourcePolicyUri:     display.Metadata.ResourcePolicyURI,
		ResourceTosUri:        display.Metadata.ResourceTosURI,
		ID:                    client.ID,
		ProjectID:             c.plan.Scope.ProjectID,
		OrganizationID:        c.plan.Scope.OrganizationID,
	}); err != nil {
		return fmt.Errorf("store client resource display: %w", err)
	}
	return nil
}

// attach binds client to the user session issuer unless attached, the user
// session issuers it is already bound to, lists it. It refuses a new
// organization-wide binding and returns the client's bindings sorted by id,
// matching the ORDER BY of the join-table reads.
func (c *IdentityCommit) attach(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient, attached []uuid.UUID) ([]uuid.UUID, error) {
	userIssuerID := c.plan.UserSessionIssuerID
	if slices.Contains(attached, userIssuerID) {
		return attached, nil
	}
	if err := c.refuseOrgWideBinding(client); err != nil {
		return nil, err
	}
	if err := tx.q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	}); err != nil {
		return nil, fmt.Errorf("attach client to user session issuer: %w", err)
	}
	if err := c.committer.audit.LogRemoteSessionClientAttachUserSessionIssuer(ctx, tx.tx, c.attachmentEvent(client)); err != nil {
		return nil, fmt.Errorf("audit client attachment: %w", err)
	}
	bindings := append(slices.Clone(attached), userIssuerID)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].String() < bindings[j].String() })
	return bindings, nil
}

func (c *IdentityCommit) auditClientCreate(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient) error {
	// The client is audited before attach binds it, and the binding records
	// its own event, so the snapshot carries no user session issuers.
	snapshot, err := mv.BuildRemoteSessionClientView(client, nil)
	if err != nil {
		return fmt.Errorf("build created client view: %w", err)
	}

	if err := c.committer.audit.LogRemoteSessionClientCreate(ctx, tx.tx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         c.plan.Scope.OrganizationID,
		ProjectID:              c.plan.Scope.ProjectID,
		Actor:                  c.plan.Scope.Actor,
		ActorDisplayName:       c.plan.Scope.ActorDisplayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		SnapshotAfter:          snapshot,
	}); err != nil {
		return fmt.Errorf("audit client creation: %w", err)
	}
	return nil
}

// auditClientCopy records a client created from a template's registration,
// naming the template it left unchanged.
func (c *IdentityCommit) auditClientCopy(ctx context.Context, tx *IdentityTx, client repo.RemoteSessionClient, sourceID uuid.UUID) error {
	snapshot, err := mv.BuildRemoteSessionClientView(client, nil)
	if err != nil {
		return fmt.Errorf("build created client view: %w", err)
	}

	if err := c.committer.audit.LogRemoteSessionClientDuplicate(ctx, tx.tx, audit.LogRemoteSessionClientDuplicateEvent{
		OrganizationID:               c.plan.Scope.OrganizationID,
		ProjectID:                    c.plan.Scope.ProjectID,
		Actor:                        c.plan.Scope.Actor,
		ActorDisplayName:             c.plan.Scope.ActorDisplayName,
		ActorSlug:                    nil,
		RemoteSessionClientURN:       urn.NewRemoteSessionClient(client.ID),
		ClientID:                     client.ClientID,
		SourceRemoteSessionClientURN: urn.NewRemoteSessionClient(sourceID),
		Registration:                 audit.RemoteSessionClientDuplicateRegistrationCopied,
		SnapshotAfter:                snapshot,
	}); err != nil {
		return fmt.Errorf("audit client copy: %w", err)
	}
	return nil
}

func (c *IdentityCommit) attachmentEvent(client repo.RemoteSessionClient) audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent {
	return audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
		OrganizationID:         c.plan.Scope.OrganizationID,
		ProjectID:              c.plan.Scope.ProjectID,
		Actor:                  c.plan.Scope.Actor,
		ActorDisplayName:       c.plan.Scope.ActorDisplayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		UserSessionIssuerURN:   urn.NewUserSessionIssuer(c.plan.UserSessionIssuerID),
	}
}
