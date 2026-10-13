package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
// still holds, including its EMA provider's place in the vendor's order;
// errStale means the result must be discarded. A shared lock keeps an order
// replace from committing between that check and the write.
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
		return fmt.Errorf("lock ema provider order: %w", err)
	}
	if outcome := c.stillServesWith(ctx, q, logger, req, sel); !outcome.Succeeded() {
		if outcome.Reason == ReasonStaleConfiguration {
			return errStale
		}
		return fmt.Errorf("check ema provider order: %s", outcome.Reason)
	}
	if sel.provider.Kind == remotesessions.EMAProviderSpeakeasy {
		if err := c.checkSpeakeasyPublish(ctx, tx, req, sel); err != nil {
			return err
		}
		_, err = q.UpsertSpeakeasyEMACredential(ctx, repo.UpsertSpeakeasyEMACredentialParams{
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
