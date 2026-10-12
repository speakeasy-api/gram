package identitychaining

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Governor answers configuration questions about identity chaining from the
// database alone. It never contacts a provider, retires a credential, or
// acquires a token, so status surfaces can share one.
type Governor struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	enc        *encryption.Client
	delegation *remotesessions.DelegationService
	now        func() time.Time

	// sources builds each EMA provider kind's token source.
	sources map[remotesessions.EMAProviderKind]sourceFactory
}

// NewGovernor builds a database-only governor; enc verifies stored credentials
// and delegation checks the human's retained delegation against the current
// trusted registration.
func NewGovernor(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, delegation *remotesessions.DelegationService) *Governor {
	return &Governor{
		logger: logger.With(attr.SlogComponent("identity_chaining")), db: db, enc: enc, delegation: delegation, now: time.Now,
		sources: map[remotesessions.EMAProviderKind]sourceFactory{
			remotesessions.EMAProviderExternal: func(p remotesessions.EMAProvider) tokenSource {
				return externalSource{c: nil, issuer: p.RemoteSessionIssuerID}
			},
			remotesessions.EMAProviderSpeakeasy: func(remotesessions.EMAProvider) tokenSource { return speakeasySource{} },
		},
	}
}

// Governs reports whether identity chaining owns the request's upstream: a
// ready binding names it, even ambiguously. The runtime uses it to decide
// whether chaining answers for the upstream instead of the strict gate.
func (g *Governor) Governs(ctx context.Context, req Request) bool {
	if !req.complete() {
		return false
	}
	_, outcome := g.selectBinding(ctx, g.logger, req)
	return outcome.Applicable() && !outcome.Retryable
}

// Serves reports the remote session issuer whose binding identity chaining
// would use for req. Unlike Governs it requires exactly one ready binding, so
// an ambiguous configuration is never presented as configured, and at least
// one EMA provider for the vendor that can serve now.
func (g *Governor) Serves(ctx context.Context, req Request) (uuid.UUID, bool) {
	if !req.complete() {
		return uuid.Nil, false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return uuid.Nil, false
	}
	if _, outcome := g.servingProviders(ctx, g.logger, req, sel); !outcome.Succeeded() {
		return uuid.Nil, false
	}
	return sel.remoteIssuerID, true
}

// ProviderOrder is req's vendor EMA provider order for this endpoint,
// including providers that cannot serve. False when no single ready binding
// names the upstream or the order cannot be read.
func (g *Governor) ProviderOrder(ctx context.Context, req Request) (remotesessions.EMAProviderOrder, bool) {
	none := remotesessions.EMAProviderOrder{VendorIssuer: "", IsDefault: false, Providers: nil}
	if !req.complete() {
		return none, false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return none, false
	}
	order, outcome := g.providerOrder(ctx, g.logger, req, sel)
	if !outcome.Succeeded() {
		return none, false
	}
	return order, true
}

// Configured reports whether any binding exists for the user session issuer
// in the project, a cheap check before per-upstream decisions. A lookup fault
// logs and reads as unconfigured.
func (g *Governor) Configured(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID) bool {
	if organizationID == "" || projectID == uuid.Nil || userSessionIssuerID == uuid.Nil {
		return false
	}
	count, err := repo.New(g.db).CountActiveEMABindingsForUserIssuer(ctx, repo.CountActiveEMABindingsForUserIssuerParams{
		IssuerID:       userSessionIssuerID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		g.logger.WarnContext(ctx, "count identity chaining bindings", attr.SlogError(err))
		return false
	}
	return count > 0
}

// HasUsableCredential reports whether a stored chained credential for req is
// usable now under the current binding, trusted registration and the human's
// latest sign-in, and still decrypts as the runtime requires. Like Acquire it
// first checks the retained delegation, so a rotated trusted registration
// reads as unusable. Faults read as unusable.
func (g *Governor) HasUsableCredential(ctx context.Context, req Request) bool {
	if !req.complete() || req.UserID == "" {
		return false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return false
	}
	// Stored credentials come from the endpoint's trusted issuer, so one is
	// usable only while an available external provider names it.
	// TODO(AIM-523): record provider provenance on the credential instead.
	providers, outcome := g.servingProviders(ctx, g.logger, req, sel)
	if !outcome.Succeeded() || !slices.ContainsFunc(providers, func(p remotesessions.EMAProvider) bool { return p.Kind == remotesessions.EMAProviderExternal }) {
		return false
	}
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
