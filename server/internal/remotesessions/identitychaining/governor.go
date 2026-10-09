package identitychaining

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Governor answers configuration questions about identity chaining from the
// database alone. It never contacts a provider, retires a credential, or
// acquires a token, so status surfaces can share one.
type Governor struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	enc    *encryption.Client
	now    func() time.Time
}

// NewGovernor builds a database-only governor; enc verifies stored credentials.
func NewGovernor(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Governor {
	return &Governor{logger: logger.With(attr.SlogComponent("identity_chaining")), db: db, enc: enc, now: time.Now}
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
// an ambiguous configuration is never presented as configured.
func (g *Governor) Serves(ctx context.Context, req Request) (uuid.UUID, bool) {
	if !req.complete() {
		return uuid.Nil, false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
		return uuid.Nil, false
	}
	return sel.remoteIssuerID, true
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
// latest sign-in, and still decrypts as the runtime requires. Faults read as
// unusable.
func (g *Governor) HasUsableCredential(ctx context.Context, req Request) bool {
	if !req.complete() || req.UserID == "" {
		return false
	}
	sel, outcome := g.selectBinding(ctx, g.logger, req)
	if !outcome.Succeeded() {
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
	authorized, err := q.AuthorizeEMADelegation(ctx, repo.AuthorizeEMADelegationParams{
		UserSessionIssuerID: req.UserSessionIssuerID,
		OrganizationID:      conv.ToPGText(req.OrganizationID),
		TrustedIssuerID:     issuer.TrustedRemoteSessionIssuerID,
		TrustedClientID:     issuer.TrustedRemoteSessionClientID,
		ProjectID:           req.ProjectID,
		UserID:              req.UserID,
	})
	if err != nil {
		g.logger.WarnContext(ctx, "authorize identity chaining delegation", attr.SlogError(err))
		return false
	}
	if !authorized {
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
