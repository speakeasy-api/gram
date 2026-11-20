package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
)

// errStoredTokenUnreadable marks a stored token the current key can't decrypt. A retry never helps; a new token does.
var errStoredTokenUnreadable = errors.New("stored token unreadable")

// StoredTokenUnreadable reports a failure caused by a stored token the current key can't decrypt.
func StoredTokenUnreadable(err error) bool {
	return errors.Is(err, errStoredTokenUnreadable)
}

// storedSignInUnreadableError is ErrStoredSignInUnreadable for one remote session.
type storedSignInUnreadableError struct {
	// remoteSessionID is the grant nothing stored of which can be read.
	remoteSessionID uuid.UUID

	// err is the decrypt or renewal failure behind it.
	err error
}

func (e *storedSignInUnreadableError) Error() string {
	return fmt.Sprintf("%s: %s", ErrStoredSignInUnreadable, e.err)
}

func (e *storedSignInUnreadableError) Unwrap() []error {
	return []error{ErrStoredSignInUnreadable, e.err}
}

// UnreadableRemoteSessionID is the remote session behind an ErrStoredSignInUnreadable failure.
func UnreadableRemoteSessionID(err error) (uuid.UUID, bool) {
	unreadable, ok := errors.AsType[*storedSignInUnreadableError](err)
	if !ok || unreadable.remoteSessionID == uuid.Nil {
		return uuid.Nil, false
	}
	return unreadable.remoteSessionID, true
}

// storedTokenKeyError is a stored token the organization's key could not open
// right now, such as a suspended or unreachable key. It reads as
// ErrRemoteSessionUnavailable and, unlike that sentinel, not as
// ErrNoValidToken, so resolution answers with a retryable outage rather than
// the empty token that sends a user to reconnect.
type storedTokenKeyError struct{ err error }

func (e *storedTokenKeyError) Error() string {
	return "stored token could not be opened right now: " + e.err.Error()
}

func (e *storedTokenKeyError) Unwrap() error { return e.err }

// Is matches only the retryable remedy.
func (e *storedTokenKeyError) Is(target error) bool { return target == ErrRemoteSessionUnavailable }

// errStoredTokenHeldForAdministrator marks an unreadable access token whose renewal the configuration refused:
// the grant is held for the administrator rather than dropped to an anonymous call.
var errStoredTokenHeldForAdministrator = errors.New("unreadable access token held for administrator")

// storedTokens is what the current keys can read of a grant's stored tokens.
type storedTokens struct {
	// access is the decrypted access token; empty when none is stored or it can't be read.
	access string

	// accessErr is why the stored access token can't be decrypted.
	accessErr error

	// refreshErr is why a usable stored refresh token can't be decrypted.
	refreshErr error

	// keyErr is why the organization's key could not open a stored token right
	// now, such as a suspended or unreachable key. It may clear on its own, so
	// it says nothing about whether the grant works and never asks anyone to
	// sign in again.
	keyErr error

	// unreadable is a grant a decrypt failure leaves with nothing to serve or renew it.
	// Only a new sign-in recreates it.
	unreadable bool

	// usable is a grant whose stored access token can serve now or whose refresh token can renew it.
	usable bool

	// refreshRead marks that the refresh token was decrypted, so refreshErr is its answer.
	refreshRead bool
}

// readStoredTokens is the one rule for whether a grant's stored tokens can be used, shared by resolution and status.
// An empty access ciphertext stores no token, so it is never a decrypt failure.
// A key that cannot open a token right now stops the read with keyErr.
func readStoredTokens(ctx context.Context, tokens *upstreamtokens.Tokens, sess remotesessions_repo.RemoteSession, now time.Time) storedTokens {
	var read storedTokens
	if !authorizationUsable(sess, now) {
		return read
	}
	// A lapsed access token is never sent, so it is never decrypted either.
	if upstreamtokens.Has(accessTokenCopies(sess)) && accessTokenLive(sess, now) {
		access, err := openAccessToken(ctx, tokens, sess)
		switch {
		case err != nil && !orgencryption.IsUnreadable(err):
			read.keyErr = err
			return read
		case err != nil:
			read.accessErr = err
		case access != "":
			read.access = access
			read.usable = true
			return read
		}
	}
	if hasRefreshToken(sess) && refreshTokenUsable(sess, now) {
		read.refreshRead = true
		refresh, err := openRefreshToken(ctx, tokens, sess)
		switch {
		case err != nil && !orgencryption.IsUnreadable(err):
			read.keyErr = err
			return read
		case err != nil:
			read.refreshErr = err
		case refresh != "":
			// An empty refresh token renews nothing, so it is never sent.
			read.usable = true
			return read
		}
	}
	read.unreadable = read.accessErr != nil || read.refreshErr != nil
	return read
}

// needsSignIn reports a grant nothing stored can serve or renew. A key that
// could not open a token right now leaves that unknown, so it does not count.
func (s storedTokens) needsSignIn() bool {
	return !s.usable && s.keyErr == nil
}

// accessTokenCopies are a session's stored access token copies.
func accessTokenCopies(sess remotesessions_repo.RemoteSession) orgencryption.Copies {
	return upstreamtokens.Stored(sess.AccessTokenEncrypted, sess.AccessTokenOrgEncrypted)
}

// refreshTokenCopies are a session's stored refresh token copies.
func refreshTokenCopies(sess remotesessions_repo.RemoteSession) orgencryption.Copies {
	return upstreamtokens.Stored(sess.RefreshTokenEncrypted, sess.RefreshTokenOrgEncrypted)
}

// openAccessToken opens a session's stored access token with the key its
// organization's transition state reads.
func openAccessToken(ctx context.Context, tokens *upstreamtokens.Tokens, sess remotesessions_repo.RemoteSession) (string, error) {
	plaintext, err := tokens.Open(ctx, nil, sess.OrganizationID, upstreamtokens.RemoteSessionAccessToken, accessTokenCopies(sess))
	if err != nil {
		return "", fmt.Errorf("open stored access token: %w", err)
	}

	return plaintext, nil
}

// openRefreshToken opens a session's stored refresh token with the key its
// organization's transition state reads.
func openRefreshToken(ctx context.Context, tokens *upstreamtokens.Tokens, sess remotesessions_repo.RemoteSession) (string, error) {
	plaintext, err := tokens.Open(ctx, nil, sess.OrganizationID, upstreamtokens.RemoteSessionRefreshToken, refreshTokenCopies(sess))
	if err != nil {
		return "", fmt.Errorf("open stored refresh token: %w", err)
	}

	return plaintext, nil
}

// StoredSignInUsable reports whether a grant's stored tokens can serve or renew it, by the rule resolution uses.
// A key that cannot open them right now is not a reason to call the grant unusable.
func StoredSignInUsable(ctx context.Context, tokens *upstreamtokens.Tokens, sess remotesessions_repo.RemoteSession, now time.Time) bool {
	return !readStoredTokens(ctx, tokens, sess, now).needsSignIn()
}

// err is the resolution error for an unreadable grant.
func (s storedTokens) err() error {
	return fmt.Errorf("%w: %w", errStoredTokenUnreadable, errors.Join(s.accessErr, s.refreshErr))
}

// reportUnreadable logs and counts a grant only a new sign-in can recreate.
func (m *ChallengeManager) reportUnreadable(ctx context.Context, sess remotesessions_repo.RemoteSession, stored storedTokens) {
	m.logger.ErrorContext(ctx, "remote session stored sign-in could not be decrypted; a new sign-in is needed", storedTokenAttrs(m.tokens, sess, stored.err())...)
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
	// Revoked or replaced meanwhile: the grant is gone, not unreadable, so it reads as never linked.
	if err == nil && res.Outcome == remotesessionmetrics.RefreshOutcomeSessionInactive {
		return "", sess, sess.UpdatedAt.Time, nil
	}
	if errors.Is(err, ErrRemoteSessionMisconfigured) {
		return "", sess, sess.UpdatedAt.Time, fmt.Errorf("%w: %w", errStoredTokenHeldForAdministrator, err)
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
// While the access token can still serve nobody needs to sign in yet, so it is a warning.
func (s *RefreshService) refreshTokenUnreadable(ctx context.Context, sess remotesessions_repo.RemoteSession, err error) {
	level, outcome := slog.LevelError, remotesessionmetrics.StoredTokenNeedsSignIn
	if readStoredTokens(ctx, s.tokens, sess, time.Now()).usable {
		level, outcome = slog.LevelWarn, remotesessionmetrics.StoredTokenAccessStillLive
	}
	s.logger.Log(ctx, level, "remote session stored refresh token could not be decrypted", storedTokenAttrs(s.tokens, sess, err)...)
	s.metrics.RecordStoredTokenUnreadable(ctx, remotesessionmetrics.StoredTokenRefresh, outcome)
}

// readRefreshToken decrypts the stored refresh token. One that can't be read is never sent and needs a new sign-in.
// A key that cannot open it right now leaves the grant intact, so the refresh is only retried later.
func (s *RefreshService) readRefreshToken(ctx context.Context, sess remotesessions_repo.RemoteSession) (string, error) {
	refreshToken, err := openRefreshToken(ctx, s.tokens, sess)
	switch {
	case err != nil && !orgencryption.IsUnreadable(err):
		return "", newTokenRefreshError("the session's stored refresh token could not be opened right now; try again shortly", err, refreshRemedyRetry)
	case err != nil:
		s.refreshTokenUnreadable(ctx, sess, err)
		return "", newTokenRefreshError("the session's stored refresh token could not be read; revoke and re-link the session", fmt.Errorf("%w: %w", errStoredTokenUnreadable, err), refreshRemedyReconnect)
	}
	if refreshToken == "" {
		return "", ErrNoValidToken
	}
	return refreshToken, nil
}

// storedTokenAttrs names the session, its organization and the platform key, for a stored token that could not be decrypted.
func storedTokenAttrs(tokens *upstreamtokens.Tokens, sess remotesessions_repo.RemoteSession, err error) []any {
	return []any{
		attr.SlogRemoteSessionID(sess.ID.String()),
		attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
		attr.SlogUserSessionIssuerID(sess.UserSessionIssuerID.String()),
		attr.SlogOrganizationID(sess.OrganizationID.String),
		attr.SlogEncryptionKeyFingerprint(tokens.PlatformKeyFingerprint()),
		attr.SlogError(err),
	}
}
