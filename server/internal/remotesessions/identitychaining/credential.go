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
	row, err := q.GetEMACredentialForUse(ctx, repo.GetEMACredentialForUseParams{
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
	})
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
// still holds; errStale means the result must be discarded.
func (c *Chainer) publish(ctx context.Context, req Request, sel selection, cred credential) error {
	encrypted, err := c.enc.Encrypt([]byte(cred.accessToken))
	if err != nil {
		return fmt.Errorf("encrypt chained access token: %w", err)
	}
	_, err = repo.New(c.db).UpsertEMACredential(ctx, repo.UpsertEMACredentialParams{
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
	if errors.Is(err, pgx.ErrNoRows) {
		return errStale
	}
	if err != nil {
		return fmt.Errorf("publish chained credential: %w", err)
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
