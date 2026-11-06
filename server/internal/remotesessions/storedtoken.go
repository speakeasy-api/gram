package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// errStoredTokenUnreadable marks a stored token the current key can't decrypt. A retry never helps; a new token does.
var errStoredTokenUnreadable = errors.New("stored token unreadable")

// storedTokens is what the current key can read of a grant's stored tokens.
type storedTokens struct {
	// access is the decrypted access token; empty when none is stored or it can't be read.
	access string

	// accessErr is why the stored access token can't be decrypted.
	accessErr error

	// refreshErr is why a usable stored refresh token can't be decrypted.
	refreshErr error

	// unreadable is a grant a decrypt failure leaves with nothing to serve or renew it.
	// Only a new sign-in recreates it.
	unreadable bool
}

// readStoredTokens is the one rule for whether a grant's stored tokens can be used, shared by resolution and status.
// An empty access ciphertext stores no token, so it is never a decrypt failure.
func readStoredTokens(enc *encryption.Client, sess remotesessions_repo.RemoteSession, now time.Time) storedTokens {
	var read storedTokens
	if !authorizationUsable(sess, now) {
		return read
	}
	if sess.AccessTokenEncrypted != "" {
		read.access, read.accessErr = enc.Decrypt(sess.AccessTokenEncrypted)
		if read.accessErr == nil && accessTokenLive(sess, now) {
			return read
		}
	}
	if hasRefreshToken(sess) && refreshTokenUsable(sess, now) {
		if _, read.refreshErr = enc.Decrypt(sess.RefreshTokenEncrypted.String); read.refreshErr == nil {
			return read
		}
	}
	read.unreadable = read.accessErr != nil || read.refreshErr != nil
	return read
}

// err is the resolution error for an unreadable grant.
func (s storedTokens) err() error {
	return fmt.Errorf("%w: %w", errStoredTokenUnreadable, errors.Join(s.accessErr, s.refreshErr))
}

// reportUnreadable logs and counts a grant only a new sign-in can recreate.
func (m *ChallengeManager) reportUnreadable(ctx context.Context, sess remotesessions_repo.RemoteSession, stored storedTokens) {
	m.logger.ErrorContext(ctx, "remote session stored sign-in could not be decrypted; a new sign-in is needed", storedTokenAttrs(m.enc, sess, stored.err())...)
	if stored.accessErr != nil {
		m.refresher.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn)
	}
	if stored.refreshErr != nil {
		m.refresher.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenRefresh, remotesessionmetrics.StoredTokenNeedsSignIn)
	}
}

// renewedUnreadableAccess answers the refresh that replaces an unreadable access token.
// A refusal leaves nothing usable, so it needs a new sign-in; an outage, cancel or configuration fault stands as it is.
func (m *ChallengeManager) renewedUnreadableAccess(ctx context.Context, sess remotesessions_repo.RemoteSession, res RefreshResult, err error) (string, remotesessions_repo.RemoteSession, time.Time, error) {
	if err == nil && res.AccessToken != "" {
		m.refresher.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenHealed)
		return res.AccessToken, res.Session, res.SourceUpdatedAt, nil
	}
	if err != nil && refreshLeftGrantUnjudged(err) {
		return "", sess, sess.UpdatedAt.Time, err
	}
	m.refresher.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenAccess, remotesessionmetrics.StoredTokenNeedsSignIn)
	if err == nil {
		err = errors.New("refresh returned no token")
	}
	return "", sess, sess.UpdatedAt.Time, fmt.Errorf("%w: renew unreadable access token: %w", errStoredTokenUnreadable, err)
}

// refreshLeftGrantUnjudged reports a refresh failure that says nothing about whether the grant still works.
func refreshLeftGrantUnjudged(err error) bool {
	if refreshErr, ok := errors.AsType[*RefreshError](err); ok && refreshErr.Outcome == remotesessionmetrics.RefreshOutcomeCanceled {
		return true
	}
	return errors.Is(err, ErrRemoteSessionUnavailable) || errors.Is(err, ErrRemoteSessionMisconfigured) ||
		errors.Is(err, context.Canceled) || oops.IsStoreFailure(err)
}

// refreshTokenUnreadable logs and counts an unreadable refresh token met while refreshing.
// While the access token is still live nobody needs to sign in yet, so it is a warning.
func (s *RefreshService) refreshTokenUnreadable(ctx context.Context, sess remotesessions_repo.RemoteSession, err error) {
	level, outcome := slog.LevelError, remotesessionmetrics.StoredTokenNeedsSignIn
	if accessTokenLive(sess, time.Now()) {
		level, outcome = slog.LevelWarn, remotesessionmetrics.StoredTokenAccessStillLive
	}
	s.logger.Log(ctx, level, "remote session stored refresh token could not be decrypted", storedTokenAttrs(s.enc, sess, err)...)
	s.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenRefresh, outcome)
}

// storedTokenAttrs names the session and the key that could not decrypt its stored token.
func storedTokenAttrs(enc *encryption.Client, sess remotesessions_repo.RemoteSession, err error) []any {
	return []any{
		attr.SlogRemoteSessionID(sess.ID.String()),
		attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
		attr.SlogUserSessionIssuerID(sess.UserSessionIssuerID.String()),
		attr.SlogEncryptionKeyFingerprint(enc.Fingerprint()),
		attr.SlogError(err),
	}
}
