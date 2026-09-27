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
	// the scope's project: linking or unlinking an organization-owned client on
	// an organization-level user session issuer, whose binding every project's
	// MCP servers on that issuer share.
	ErrIdentityOrgWideBinding = errors.New("identity: organization-wide binding change")

	// ErrIdentityStepOrder marks a commit step run out of order, which is a
	// caller bug.
	ErrIdentityStepOrder = errors.New("identity commit: step run out of order")
)

// OrgWideBindingMessage is the caller-facing message for
// ErrIdentityOrgWideBinding.
const OrgWideBindingMessage = "this MCP server shares an organization identity binding; change it from the organization's identity provider settings"

const (
	agentBindingsOnReplacedClientMessage = "agents are still bound to this MCP server through its current client; unlink them before changing its Remote Identity Provider"
	identityChainingBindingsMessage      = "active identity-chaining bindings reference this configuration; explicitly unlink them before changing or deleting it"
	ambiguousCurrentProviderMessage      = "this MCP server's user session issuer is bound to more than one Remote Identity Provider; remove the extra clients from the Remote Identity Provider settings before changing it"
	clientTakenMessage                   = "a remote session client is already bound to this user session issuer for the same remote session issuer"
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
	serverURL *url.URL
	policy    *guardian.Policy
	tunnels   *tunnelrouting.HTTPClient
	telemetry registration.Recorder
}

func NewIdentityCommitter(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, auditLogger *audit.Logger, serverURL *url.URL, policy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, telemetry registration.Recorder) *IdentityCommitter {
	return &IdentityCommitter{logger: logger, db: db, enc: enc, audit: auditLogger, serverURL: serverURL, policy: policy, tunnels: tunnels, telemetry: telemetry}
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
}

// IdentityPlan is what a caller wants configured. It holds only the choices
// where flows differ; every step of the commit reads it.
type IdentityPlan struct {
	// Scope is the tenant and actor.
	Scope IdentityScope

	// UserSessionIssuerID is the user session issuer the client is bound to.
	UserSessionIssuerID uuid.UUID

	// Provider is UseProvider or CreateProvider.
	Provider ProviderChoice

	// Client is LinkClient, ManualClient or RegisterClient.
	Client ClientChoice

	// Bound is what happens to clients already bound to the user session
	// issuer: ReplaceBound or ReuseBound.
	Bound BoundPolicy

	// ResourceDisplay, when set, stores RFC 9728 display members on the bound
	// client.
	ResourceDisplay *ResourceDisplay
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

// RegisterClient obtains a client from the provider: a Gram-hosted Client ID
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
	// ReplaceBound unbinds the clients of the server's current provider, read
	// from the live bindings, and requires the plan's provider to have no
	// other client bound.
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
	committer *IdentityCommitter
	plan      IdentityPlan

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

	bound, err := listUserSessionIssuerClients(ctx, q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, c.plan.UserSessionIssuerID)
	if err != nil {
		return err
	}
	switch c.plan.Bound {
	case ReplaceBound:
		// A new provider has no id yet, so every bound client belongs to another.
		replaced, err := replacedClients(bound, c.provider.ID, c.plan.Client.keep())
		if err != nil {
			return err
		}
		for _, client := range replaced {
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
	response, err := RegisterDynamicClient(ctx, c.committer.policy, c.committer.tunnels, c.committer.serverURL, ProxyRegisterRequest{
		RegistrationEndpoint:    endpoint,
		TunneledMcpServerID:     conv.PtrEmpty(tunnelBindingID(c.provider.TunneledMcpServerID)),
		Scope:                   conv.PtrEmpty(strings.Join(policy.Scope, " ")),
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
	if !p.clientIDMetadataDocumentSupported {
		return false
	}
	methods := p.tokenEndpointAuthMethodsSupported
	return len(methods) == 0 || slices.Contains(methods, string(TokenEndpointAuthMethodNone))
}

// capabilities is what registration is chosen from: the new provider's
// parameters or the stored provider's row.
func (c *IdentityCommit) capabilities() providerCapabilities {
	if create := c.plan.Provider.create; create != nil {
		return providerCapabilities{
			registrationEndpoint:              create.RegistrationEndpoint,
			tokenEndpointAuthMethodsSupported: create.TokenEndpointAuthMethodsSupported,
			clientIDMetadataDocumentSupported: create.ClientIDMetadataDocumentSupported,
		}
	}
	return providerCapabilities{
		registrationEndpoint:              c.provider.RegistrationEndpoint,
		tokenEndpointAuthMethodsSupported: c.provider.TokenEndpointAuthMethodsSupported,
		clientIDMetadataDocumentSupported: c.provider.ClientIDMetadataDocumentSupported,
	}
}

// Begin opens the commit's transaction. The caller may take its own locks on
// tx.DB() before Lock; defer tx.Rollback, a no-op after Commit.
func (c *IdentityCommit) Begin(ctx context.Context) (*IdentityTx, error) {
	tx, err := c.committer.db.Begin(ctx)
	if err != nil {
		c.recordOrphanedRegistration(ctx)
		return nil, fmt.Errorf("begin identity transaction: %w", err)
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
	reused := false
	switch c.plan.Bound {
	case ReplaceBound:
		bound, err := listUserSessionIssuerClients(ctx, tx.q, c.plan.Scope.ProjectID, c.plan.Scope.OrganizationID, c.plan.UserSessionIssuerID)
		if err != nil {
			return err
		}
		replaced, err := replacedClients(bound, c.provider.ID, c.plan.Client.keep())
		if err != nil {
			return err
		}
		for _, current := range replaced {
			if err := c.detachClient(ctx, tx, current); err != nil {
				return err
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
// shared across projects. A project-owned client is invisible to other
// projects, so its binding only ever changes the scope's own project.
func (c *IdentityCommit) refuseOrgWideBinding(client repo.RemoteSessionClient) error {
	if c.userIssuerOrgLevel && !client.ProjectID.Valid {
		return identityRefusal(ErrIdentityOrgWideBinding, nil, OrgWideBindingMessage)
	}
	return nil
}

// refuseLiveAgentBindings refuses to unbind a client that unrevoked agent
// attachments go through: deleting its binding would cascade-delete them
// unaudited.
func refuseLiveAgentBindings(ctx context.Context, q *repo.Queries, scope IdentityScope, clientID, userIssuerID uuid.UUID) error {
	live, err := q.HasLivePrincipalRemoteSessionBindingsForClientBinding(ctx, repo.HasLivePrincipalRemoteSessionBindingsForClientBindingParams{
		ProjectID:             scope.ProjectID,
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

// createCIMDClient creates a client identified by a Gram-hosted Client ID
// Metadata Document.
func (c *IdentityCommit) createCIMDClient(ctx context.Context, tx *IdentityTx) (repo.RemoteSessionClient, error) {
	// Generated here so the document URL, which embeds the id, can be the
	// client_id on a single insert.
	id, err := uuid.NewV7()
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("generate client id: %w", err)
	}
	policy := c.plan.Client.policy
	client, err := tx.q.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    id,
		ProjectID:             conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:        conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		RemoteSessionIssuerID: c.provider.ID,
		ClientIDMetadataUri:   ClientMetadataDocumentURL(c.committer.serverURL, id),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 policy.Scope,
		Audience:              conv.PtrToPGText(policy.Audience),
	})
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("create cimd client: %w", err)
	}
	return client, c.auditClientCreate(ctx, tx, client)
}

// createClient creates a client from provider-issued credentials.
func (c *IdentityCommit) createClient(ctx context.Context, tx *IdentityTx, credentials ClientCredentials) (repo.RemoteSessionClient, error) {
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
	client, err := tx.q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:                       conv.ToNullUUID(c.plan.Scope.ProjectID),
		OrganizationID:                  conv.ToPGTextEmpty(c.plan.Scope.OrganizationID),
		RemoteSessionIssuerID:           c.provider.ID,
		ClientID:                        strings.TrimSpace(credentials.ClientID),
		ClientSecretEncrypted:           secret,
		ClientIDIssuedAt:                clientIDIssuedAt,
		ClientSecretExpiresAt:           credentials.SecretExpiresAt,
		TokenEndpointAuthAudienceFormat: pgtype.Text{String: "", Valid: false},
		// Neither is set here; a JWKS is attached afterwards through the
		// client JWKS API.
		JsonWebKeySetID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TokenEndpointAuthMethod:      conv.PtrToPGText(credentials.TokenEndpointAuthMethod),
		Scope:                        credentials.Scope,
		Audience:                     conv.PtrToPGText(credentials.Audience),
		LegacyCallbackUrl:            false,
	})
	if err != nil {
		return repo.RemoteSessionClient{}, fmt.Errorf("create client: %w", err)
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
	if err := c.committer.audit.LogRemoteSessionClientCreate(ctx, tx.tx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         c.plan.Scope.OrganizationID,
		ProjectID:              c.plan.Scope.ProjectID,
		Actor:                  c.plan.Scope.Actor,
		ActorDisplayName:       c.plan.Scope.ActorDisplayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
	}); err != nil {
		return fmt.Errorf("audit client creation: %w", err)
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
