package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// credential is an acquired downstream credential awaiting publish.
type credential struct {
	accessToken     string
	expiresAt       time.Time
	grantedScopes   []string
	refreshObserved bool

	// trustedSessionID and trustedObtainedAt are the retained delegation and
	// the sign-in the credential was acquired under.
	trustedSessionID  uuid.UUID
	trustedObtainedAt time.Time
}

// cachedToken releases a stored credential only when its provenance still
// matches the current selection and delegation. A slot that no longer matches
// is retired, erasing its ciphertext, without being decrypted.
func (c *Chainer) cachedToken(ctx context.Context, logger *slog.Logger, req Request, sel selection) (Token, bool) {
	var none Token
	q := repo.New(c.db)
	now := c.now()
	row, err := readCredential(ctx, q, req, sel, now)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, false
	case err != nil:
		logger.ErrorContext(ctx, "read identity chaining credential", attr.SlogError(err))
		return none, false
	}
	if !row.Usable {
		if err := q.RetireEMACredential(context.WithoutCancel(ctx), repo.RetireEMACredentialParams{ID: row.ID, OrganizationID: conv.ToPGText(req.OrganizationID), ProjectID: uuid.NullUUID{UUID: req.ProjectID, Valid: true}, ExpectedUpdatedAt: row.UpdatedAt}); err != nil {
			logger.WarnContext(ctx, "retire stale identity chaining credential", attr.SlogError(err))
		}
		return none, false
	}
	value, err := c.enc.Decrypt(row.AccessTokenEncrypted.String)
	if err != nil || value == "" {
		logger.ErrorContext(ctx, "decrypt identity chaining credential", attr.SlogError(err))
		return none, false
	}
	if err := q.TouchEMACredentialLastUsed(ctx, repo.TouchEMACredentialLastUsedParams{
		ID:         row.ID,
		ProjectID:  uuid.NullUUID{UUID: req.ProjectID, Valid: true},
		NowTs:      conv.ToPGTimestamptz(now),
		UsedCutoff: conv.ToPGTimestamptz(now.Add(-lastUsedCutoff)),
	}); err != nil {
		logger.WarnContext(ctx, "stamp identity chaining credential last_used_at", attr.SlogError(err))
	}
	return Token{value: value, expiresAt: row.AccessExpiresAt.Time.Add(-accessExpirySkew)}, true
}

// publish stores the credential only if the provenance it was acquired under
// still holds, including the vendor's EMA provider selection and the
// binding's ready generation; errStale means the result must be discarded.
// Shared locks keep a selection change, unlink or rebind from committing
// between those checks and the write.
func (c *Chainer) publish(ctx context.Context, logger *slog.Logger, req Request, sel selection, cred credential) error {
	encrypted, err := c.enc.Encrypt([]byte(cred.accessToken))
	if err != nil {
		return fmt.Errorf("encrypt chained access token: %w", err)
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin chained credential publish: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)
	if err := q.LockEMAProvidersShared(ctx, req.OrganizationID); err != nil {
		return fmt.Errorf("lock ema provider selection: %w", err)
	}
	if outcome := c.stillServesWith(ctx, q, logger, req, sel); !outcome.Succeeded() {
		if outcome.Reason == ReasonStaleConfiguration {
			return errStale
		}
		return fmt.Errorf("check ema provider selection: %s", outcome.Reason)
	}
	// Preparation locks the binding row before changing it; sharing that
	// lock here means the publish either sees the change or finishes first.
	if _, err := q.LockEMABindingForPublish(ctx, repo.LockEMABindingForPublishParams{ID: sel.bindingID, OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, Generation: sel.generation}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errStale
		}
		return fmt.Errorf("lock ema binding: %w", err)
	}
	if sel.provider.Kind == remotesessions.EMAProviderSpeakeasy {
		if err := c.checkSpeakeasyPublish(ctx, tx, req, sel); err != nil {
			return err
		}
		err = upsertSpeakeasyCredential(ctx, tx, repo.UpsertSpeakeasyEMACredentialParams{
			OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, UserSessionIssuerID: req.UserSessionIssuerID,
			RemoteSessionIssuerID: sel.remoteIssuerID, RemoteSessionClientID: sel.clientID, Resource: sel.resource,
			SubjectUrn: urn.NewUserSubject(req.UserID).String(), BindingID: sel.bindingID, BindingGeneration: sel.generation,
			InternalSessionIssuerID: sel.provider.InternalSessionIssuerID, InternalSessionIssuerUpdatedAt: conv.ToPGTimestamptz(sel.internalIssuerVersion),
			InternalSessionIssuerKeyID: sel.internalKeyID, SignInID: sel.signInID, UserSessionID: sel.userSessionID,
			RequestedScopes: storedScopes(sel.scopes), GrantedScopes: storedScopes(cred.grantedScopes), AccessTokenEncrypted: encrypted,
			AccessExpiresAt: conv.ToPGTimestamptz(cred.expiresAt), DownstreamRefreshTokenObserved: cred.refreshObserved,
		})
	} else {
		_, err = q.UpsertEMACredential(ctx, repo.UpsertEMACredentialParams{
			OrganizationID:                 req.OrganizationID,
			ProjectID:                      req.ProjectID,
			UserSessionIssuerID:            req.UserSessionIssuerID,
			RemoteSessionIssuerID:          sel.remoteIssuerID,
			RemoteSessionClientID:          sel.clientID,
			Resource:                       sel.resource,
			SubjectUrn:                     urn.NewUserSubject(req.UserID).String(),
			BindingID:                      sel.bindingID,
			BindingGeneration:              sel.generation,
			TrustedIssuerSessionID:         cred.trustedSessionID,
			TrustedCredentialObtainedAt:    conv.PtrToPGTimestamptz(conv.PtrEmpty(cred.trustedObtainedAt)),
			RequestedScopes:                storedScopes(sel.scopes),
			GrantedScopes:                  storedScopes(cred.grantedScopes),
			AccessTokenEncrypted:           encrypted,
			AccessExpiresAt:                pgtype.Timestamptz{Time: cred.expiresAt, Valid: true, InfinityModifier: pgtype.Finite},
			DownstreamRefreshTokenObserved: cred.refreshObserved,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errStale
	}
	if err != nil {
		return fmt.Errorf("publish chained credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit chained credential: %w", err)
	}
	return nil
}

// legacySubjectKey allows one live credential per subject regardless of
// provider or grant. Drop the fallback below with the index.
const legacySubjectKey = "remote_session_ema_credentials_subject_key"

// upsertSpeakeasyCredential writes the grant's own slot. While the legacy
// index exists, another grant's or provider's live row blocks that slot, so it
// is retired and the write retried, matching the old replace-on-publish.
func upsertSpeakeasyCredential(ctx context.Context, tx pgx.Tx, params repo.UpsertSpeakeasyEMACredentialParams) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Speakeasy credential savepoint: %w", err)
	}
	_, err = repo.New(sp).UpsertSpeakeasyEMACredential(ctx, params)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation || pgErr.ConstraintName != legacySubjectKey {
		if err != nil {
			return errors.Join(err, sp.Rollback(ctx))
		}
		if err := sp.Commit(ctx); err != nil {
			return fmt.Errorf("release Speakeasy credential savepoint: %w", err)
		}
		return nil
	}
	if err := sp.Rollback(ctx); err != nil {
		return fmt.Errorf("roll back Speakeasy credential savepoint: %w", err)
	}
	q := repo.New(tx)
	if err := q.RetireLegacyEMACredentialSlot(ctx, repo.RetireLegacyEMACredentialSlotParams{
		OrganizationID: params.OrganizationID, ProjectID: params.ProjectID, UserSessionIssuerID: params.UserSessionIssuerID,
		RemoteSessionClientID: params.RemoteSessionClientID, Resource: params.Resource, SubjectUrn: params.SubjectUrn,
	}); err != nil {
		return fmt.Errorf("retire legacy EMA credential slot: %w", err)
	}
	if _, err := q.UpsertSpeakeasyEMACredential(ctx, params); err != nil {
		return fmt.Errorf("publish Speakeasy EMA credential after retiring legacy slot: %w", err)
	}
	return nil
}

// storedScopes keeps NOT NULL array columns non-NULL for empty scope sets.
func storedScopes(scopes []string) []string {
	if scopes == nil {
		return []string{}
	}
	return scopes
}

// readCredential shares provider-specific storage checks between runtime and status.
func readCredential(ctx context.Context, q *repo.Queries, req Request, sel selection, now time.Time) (repo.GetEMACredentialForUseRow, error) {
	if sel.provider.Kind != remotesessions.EMAProviderSpeakeasy {
		row, err := q.GetEMACredentialForUse(ctx, credentialForUseParams(req, sel, now))
		if err != nil {
			return row, fmt.Errorf("read external EMA credential: %w", err)
		}
		return row, nil
	}
	r, err := q.GetSpeakeasyEMACredentialForUse(ctx, repo.GetSpeakeasyEMACredentialForUseParams{
		OrganizationID: req.OrganizationID, ProjectID: uuid.NullUUID{UUID: req.ProjectID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: req.UserSessionIssuerID, Valid: true}, RemoteSessionIssuerID: sel.remoteIssuerID,
		RemoteSessionClientID: uuid.NullUUID{UUID: sel.clientID, Valid: true}, Resource: sel.resource, SubjectUrn: urn.NewUserSubject(req.UserID).String(),
		BindingID: sel.bindingID, BindingGeneration: sel.generation, RequestedScopes: storedScopes(sel.scopes), UsableAfter: conv.ToPGTimestamptz(now.Add(accessExpirySkew)),
		InternalSessionIssuerID: sel.provider.InternalSessionIssuerID, InternalSessionIssuerUpdatedAt: conv.ToPGTimestamptz(sel.internalIssuerVersion),
		InternalSessionIssuerKeyID: sel.internalKeyID, SignInID: sel.signInID, UserSessionID: sel.userSessionID,
	})
	if err != nil {
		return repo.GetEMACredentialForUseRow(r), fmt.Errorf("read Speakeasy EMA credential: %w", err)
	}
	return repo.GetEMACredentialForUseRow(r), nil
}
