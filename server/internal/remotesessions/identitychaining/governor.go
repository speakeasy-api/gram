package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Governor answers configuration questions about identity chaining. It may
// resolve public resource metadata but never acquires a token or retires a
// credential, so authenticated status surfaces can share one.
type Governor struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	enc        *encryption.Client
	delegation *remotesessions.DelegationService
	now        func() time.Time

	// sources builds each EMA provider kind's token source.
	sources map[remotesessions.EMAProviderKind]sourceFactory

	// resolveResource maps a direct HTTP transport to its authorization resource.
	resolveResource func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error)

	// cachedResource is resolveResource answered from its shared cache alone,
	// reporting false on a miss; nil reads every lookup as a miss.
	cachedResource func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error)

	// recentRefusal reports a provider's recent definitive refusal of sel, as
	// the runtime shares it; nil on a Governor, which never exchanges.
	recentRefusal func(context.Context, Request, selection) (Outcome, bool)
}

// NewGovernor builds a configuration governor; enc verifies stored credentials
// and delegation checks the human's retained delegation against the current
// trusted registration.
func NewGovernor(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, delegation *remotesessions.DelegationService) *Governor {
	g := &Governor{
		logger: logger.With(attr.SlogComponent("identity_chaining")), db: db, enc: enc, delegation: delegation, now: time.Now,
		resolveResource: nil,
		cachedResource:  nil,
		recentRefusal:   nil,
		sources:         nil,
	}
	g.sources = map[remotesessions.EMAProviderKind]sourceFactory{
		remotesessions.EMAProviderExternal: func(p remotesessions.EMAProvider) tokenSource {
			return externalSource{g: g, c: nil, issuer: p.RemoteSessionIssuerID}
		},
		remotesessions.EMAProviderSpeakeasy: func(remotesessions.EMAProvider) tokenSource { return new(speakeasySource) },
	}
	return g
}

// SetResourceResolver installs public metadata resolution before serving any
// authenticated runtime, consent, or management requests.
func (g *Governor) SetResourceResolver(resolve func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error)) {
	g.resolveResource = resolve
}

// SetCachedResourceResolver installs the cache-only view of the resource
// resolver, which RefreshDecision reads so the token endpoint never probes an
// upstream.
func (g *Governor) SetCachedResourceResolver(cached func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error)) {
	g.cachedResource = cached
}

// ResolveResource returns the resource identity used by binding selection.
// Tunneled resources are explicit operator configuration and are not probed.
func (g *Governor) ResolveResource(ctx context.Context, req Request) (protectedresource.EndpointResource, error) {
	if g.resolveResource == nil || req.RemoteSessionIssuerID.Valid || !g.Configured(ctx, req.OrganizationID, req.ProjectID, req.UserSessionIssuerID) {
		return protectedresource.EndpointResource{Resource: req.UpstreamResource, AuthorizationServers: nil, Discovered: false}, nil
	}
	return g.resolveResource(ctx, req.OrganizationID, req.ProjectID, req.UpstreamResource)
}

// Governs reports whether identity chaining owns the request's upstream: a
// ready binding names it, even ambiguously, or a bound resource on its origin
// could not be confirmed just now. The runtime uses it to decide whether
// chaining answers for the upstream instead of the strict gate, so a
// temporary metadata failure reports as retryable instead of asking the
// human to reconnect.
func (g *Governor) Governs(ctx context.Context, req Request) bool {
	if !req.complete() {
		return false
	}
	_, outcome := g.selectBinding(ctx, g.logger, req)
	return outcome.Applicable()
}

// Serves reports the remote session issuer whose binding identity chaining
// would use for req. Unlike Governs it requires exactly one ready binding, so
// an ambiguous configuration is never presented as configured, and a provider
// in the vendor's order that can serve now.
func (g *Governor) Serves(ctx context.Context, req Request) (uuid.UUID, bool) {
	if !req.complete() {
		return uuid.Nil, false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return uuid.Nil, false
	}
	if order, outcome := g.readyProviderOrder(ctx, req, sel); !outcome.Succeeded() || len(order.Available()) == 0 {
		return uuid.Nil, false
	}
	return sel.remoteIssuerID, true
}

// ErrProviderOrderUnavailable means a single ready binding names the
// upstream but its vendor's EMA provider order could not be read.
var ErrProviderOrderUnavailable = errors.New("identitychaining: ema provider order unavailable")

// ProviderOrder is req's vendor EMA provider order for this endpoint, each
// provider flagged when it cannot serve. False when no single ready binding
// names the upstream; ErrProviderOrderUnavailable when the binding is ready
// but its order cannot be read.
func (g *Governor) ProviderOrder(ctx context.Context, req Request) (remotesessions.EMAProviderOrder, bool, error) {
	var none remotesessions.EMAProviderOrder
	if !req.complete() {
		return none, false, nil
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return none, false, nil
	}
	order, outcome := g.readyProviderOrder(ctx, req, sel)
	if !outcome.Succeeded() {
		return none, false, ErrProviderOrderUnavailable
	}
	return order, true, nil
}

// Configured reports whether any binding exists for the user session issuer
// in the project, a cheap check before per-upstream decisions. A lookup fault
// logs and reads as unconfigured.
func (g *Governor) Configured(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID) bool {
	configured, err := g.configured(ctx, organizationID, projectID, userSessionIssuerID)
	if err != nil {
		g.logger.WarnContext(ctx, "count identity chaining bindings", attr.SlogError(err))
		return false
	}
	return configured
}

// configured is Configured with a lookup fault returned instead of read as
// unconfigured.
func (g *Governor) configured(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID) (bool, error) {
	if organizationID == "" || projectID == uuid.Nil || userSessionIssuerID == uuid.Nil {
		return false, nil
	}
	count, err := repo.New(g.db).CountActiveEMABindingsForUserIssuer(ctx, repo.CountActiveEMABindingsForUserIssuerParams{
		IssuerID:       userSessionIssuerID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return false, fmt.Errorf("count identity chaining bindings: %w", err)
	}
	return count > 0, nil
}

// HasUsableCredential reports whether a stored chained credential for req is
// usable now under the current binding, trusted registration and the human's
// latest sign-in, and still decrypts as the runtime requires. Like Acquire it
// accepts a credential from any provider in the order that can serve and
// first checks that provider's authority, so a rotated trusted registration
// reads as unusable. Faults read as unusable.
func (g *Governor) HasUsableCredential(ctx context.Context, req Request) bool {
	if !req.complete() || req.UserID == "" {
		return false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return false
	}
	order, outcome := g.providerOrder(ctx, g.logger, req, sel)
	if !outcome.Succeeded() {
		return false
	}
	for _, p := range order.Available() {
		if g.hasUsableCredentialFrom(ctx, req, sel, p) {
			return true
		}
	}
	return false
}

// hasUsableCredentialFrom reports whether p's stored credential for req is
// usable now.
func (g *Governor) hasUsableCredentialFrom(ctx context.Context, req Request, sel selection, p remotesessions.EMAProvider) bool {
	current := sel
	current.provider = p
	if p.Kind != remotesessions.EMAProviderSpeakeasy {
		return g.hasExternalCredential(ctx, req, current)
	}
	statusReq := req
	if statusReq.UserSessionJTI == "" {
		jti, err := repo.New(g.db).GetSpeakeasyEMAStoredGrant(ctx, repo.GetSpeakeasyEMAStoredGrantParams{
			OrganizationID: conv.ToPGText(req.OrganizationID), ProjectID: uuid.NullUUID{UUID: req.ProjectID, Valid: true}, UserSessionIssuerID: uuid.NullUUID{UUID: req.UserSessionIssuerID, Valid: true},
			RemoteSessionIssuerID: uuid.NullUUID{UUID: current.remoteIssuerID, Valid: true}, RemoteSessionClientID: uuid.NullUUID{UUID: current.clientID, Valid: true},
			Resource: current.resource, SubjectUrn: urn.NewUserSubject(req.UserID).String(),
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				g.logger.WarnContext(ctx, "read retained Speakeasy EMA grant", attr.SlogError(err))
			}
			return false
		}
		statusReq.UserSessionJTI = jti
	}
	if !g.source(p).authorize(ctx, g.logger, statusReq, &current).Succeeded() {
		return false
	}
	usable, err := g.storedCredentialUsable(ctx, req, current)
	if err != nil {
		g.logger.WarnContext(ctx, "read identity chaining credential", attr.SlogError(err))
		return false
	}
	return usable
}

func (g *Governor) hasExternalCredential(ctx context.Context, req Request, sel selection) bool {
	q := repo.New(g.db)
	issuer, err := q.GetEMAChainingUserIssuer(ctx, repo.GetEMAChainingUserIssuerParams{ID: req.UserSessionIssuerID, OrganizationID: conv.ToPGText(req.OrganizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false
	case err != nil:
		g.logger.WarnContext(ctx, "read identity chaining user session issuer", attr.SlogError(err))
		return false
	}
	if g.delegation == nil {
		return false
	}
	binding := remotesessions.DelegationBinding{OrganizationID: req.OrganizationID, IssuerID: issuer.TrustedRemoteSessionIssuerID.UUID, ClientID: issuer.TrustedRemoteSessionClientID.UUID, HumanID: req.UserID}
	if err := g.delegation.Check(ctx, binding, authority{db: g.db, req: req}); err != nil {
		if errors.Is(err, remotesessions.ErrDelegationTemporary) {
			g.logger.WarnContext(ctx, "check identity chaining delegation", attr.SlogError(err))
		}
		return false
	}
	sel.trustedClientID = issuer.TrustedRemoteSessionClientID.UUID
	row, err := q.GetEMACredentialForUse(ctx, credentialForUseParams(req, sel, g.now()))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false
	case err != nil:
		g.logger.WarnContext(ctx, "read identity chaining credential", attr.SlogError(err))
		return false
	}
	if !row.Usable || g.enc == nil {
		return false
	}
	value, err := g.enc.Decrypt(row.AccessTokenEncrypted.String)
	if err != nil {
		g.logger.WarnContext(ctx, "decrypt identity chaining credential", attr.SlogError(err))
		return false
	}
	return value != ""
}

// credentialForUseParams reads the live credential slot for req under sel.
func credentialForUseParams(req Request, sel selection, now time.Time) repo.GetEMACredentialForUseParams {
	return repo.GetEMACredentialForUseParams{
		OrganizationID:        req.OrganizationID,
		RemoteSessionIssuerID: sel.remoteIssuerID,
		BindingID:             sel.bindingID,
		BindingGeneration:     sel.generation,
		RequestedScopes:       storedScopes(sel.scopes),
		UsableAfter:           conv.ToPGTimestamptz(now.Add(accessExpirySkew)),
		TrustedClientID:       sel.trustedClientID,
		ProjectID:             uuid.NullUUID{UUID: req.ProjectID, Valid: true},
		UserSessionIssuerID:   uuid.NullUUID{UUID: req.UserSessionIssuerID, Valid: true},
		RemoteSessionClientID: uuid.NullUUID{UUID: sel.clientID, Valid: true},
		Resource:              sel.resource,
		SubjectUrn:            urn.NewUserSubject(req.UserID).String(),
	}
}
