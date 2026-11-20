// Package upstreamtokens seals, opens, stores and erases the upstream OAuth
// tokens and related values Speakeasy stores for remote sessions, trusted
// issuer sessions and EMA credentials.
//
// Each value has two stored copies: a *_encrypted column under the platform
// key and a *_org_encrypted column under the owning organization's key. Which
// copies are written and read follows the organization's transition state for
// the oauth-session-tokens use case (package orgencryption). Every reader and
// writer goes through Tokens, so they all follow the same rules, and every
// statement that writes a copy lives in upstreamtoken_queries.sql in package
// remotesessions. Statements elsewhere only erase a value, by setting both of
// its copies to NULL, which is correct under any transition state.
//
// A row without an organization stays on the platform key: a session under a
// global client, and a row whose organization was deleted. Every write fences
// on the row's own organization column, so a row without one can only be
// written under generation 0. The CopyStores count rows without an
// organization that still belong to one as unattributed, which keeps that
// organization from finalizing.
//
// Transition state is read through the database handle the caller holds. The
// organization key's configuration and data key are still loaded from the
// process's pool on a cache miss, so a writer holding a transaction can
// briefly need a second pooled connection when its organization is past
// platform_key. Writers that call a provider first run RequireUsable, which
// also warms that cache, so the write itself rarely calls KMS.
package upstreamtokens

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// The column pairs of the stored values. OrgColumn is bound into every
// organization-key ciphertext, so it must never change.
var (
	// RemoteSessionAccessToken is a remote session's upstream access token.
	RemoteSessionAccessToken = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "remote_sessions.access_token_encrypted",
		OrgColumn:      "remote_sessions.access_token_org_encrypted",
	}

	// RemoteSessionRefreshToken is a remote session's upstream refresh token.
	RemoteSessionRefreshToken = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "remote_sessions.refresh_token_encrypted",
		OrgColumn:      "remote_sessions.refresh_token_org_encrypted",
	}

	// EMACredentialAccessToken is the access token an EMA credential holds.
	EMACredentialAccessToken = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "remote_session_ema_credentials.access_token_encrypted",
		OrgColumn:      "remote_session_ema_credentials.access_token_org_encrypted",
	}

	// TrustedIssuerSessionIdentityAssertion is a trusted issuer session's identity assertion.
	TrustedIssuerSessionIdentityAssertion = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "trusted_issuer_sessions.identity_assertion_encrypted",
		OrgColumn:      "trusted_issuer_sessions.identity_assertion_org_encrypted",
	}

	// TrustedIssuerSessionRefreshToken is a trusted issuer session's refresh token.
	TrustedIssuerSessionRefreshToken = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "trusted_issuer_sessions.refresh_token_encrypted",
		OrgColumn:      "trusted_issuer_sessions.refresh_token_org_encrypted",
	}

	// TrustedIssuerSessionUpstreamSubject is a trusted issuer session's upstream subject.
	TrustedIssuerSessionUpstreamSubject = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "trusted_issuer_sessions.upstream_subject_encrypted",
		OrgColumn:      "trusted_issuer_sessions.upstream_subject_org_encrypted",
	}

	// TrustedIssuerSessionNonce is a trusted issuer session's nonce.
	TrustedIssuerSessionNonce = orgencryption.CopyColumns{
		UseCase:        orgencryption.UseCaseOAuthSessionTokens,
		PlatformColumn: "trusted_issuer_sessions.nonce_encrypted",
		OrgColumn:      "trusted_issuer_sessions.nonce_org_encrypted",
	}
)

// maxWriteAttempts bounds how often a write decides, seals and writes again
// after the transition moved under it. Each retry follows a committed
// transition, and an organization changes state a handful of times in its
// life, so a third consecutive move means something is wrong.
const maxWriteAttempts = 3

// ErrSessionNotFound means the remote session to write does not exist, is
// deleted, or belongs to another organization.
var ErrSessionNotFound = errors.New("upstreamtokens: remote session not found")

// ErrTrustedIssuerSessionNotFound means the trusted issuer session to write
// does not exist, is deleted, or belongs to another organization.
var ErrTrustedIssuerSessionNotFound = errors.New("upstreamtokens: trusted issuer session not found")

// ErrEMACredentialNotFound means the EMA credential to write does not exist,
// is retired, or belongs to another organization.
var ErrEMACredentialNotFound = errors.New("upstreamtokens: EMA credential not found")

// Stored returns the copies a row stores for one value.
func Stored(platform, org pgtype.Text) orgencryption.Copies {
	return orgencryption.Copies{Platform: platform, Org: org}
}

// Has reports whether a row stores the value in either copy. An empty
// ciphertext stores nothing: rows written before the access token column was
// nullable used it for "no token".
func Has(stored orgencryption.Copies) bool {
	return (stored.Platform.Valid && stored.Platform.String != "") || (stored.Org.Valid && stored.Org.String != "")
}

// Tokens is the one seam for stored upstream OAuth tokens.
type Tokens struct {
	logger   *slog.Logger
	cipher   *orgencryption.Cipher
	platform *encryption.Client
}

// New builds Tokens over the process's cipher and platform key.
func New(logger *slog.Logger, cipher *orgencryption.Cipher, platform *encryption.Client) *Tokens {
	return &Tokens{
		logger:   logger.With(attr.SlogComponent("upstreamtokens")),
		cipher:   cipher,
		platform: platform,
	}
}

// WithoutOrganizationEncryption builds Tokens for a process without
// organization encryption, over its own transition state: organizations in
// platform_key work, and any other transition state fails closed. A process
// with organization encryption builds Tokens with New over its shared cipher
// instead.
func WithoutOrganizationEncryption(logger *slog.Logger, db *pgxpool.Pool, platform *encryption.Client) *Tokens {
	transitions := orgencryption.NewTransitions(logger, db, nil, nil, orgencryption.TransitionsConfig{ClearSettleDelay: 0})

	return New(logger, orgencryption.NewCipher(logger, platform, nil, transitions), platform)
}

// PlatformKeyFingerprint identifies the platform key, for logs about a value
// it could not decrypt.
func (t *Tokens) PlatformKeyFingerprint() string {
	return t.platform.Fingerprint()
}

// decision is what a value is sealed and opened under: the organization's
// transition decision, or the platform key alone for a row without an
// organization.
type decision struct {
	// platformOnly marks a row without an organization.
	platformOnly bool

	// d is the organization's decision; zero when platformOnly.
	d orgencryption.Decision
}

// generation is the transition generation fenced writes compare against.
func (dc decision) generation() int64 {
	if dc.platformOnly {
		return 0
	}

	return dc.d.Generation
}

// platformOnly reports whether a row with organizationID stays on the
// platform key.
func platformOnly(organizationID pgtype.Text) bool {
	return !organizationID.Valid || organizationID.String == ""
}

// decide returns the decision for a row's organization, reading transition
// state through db on a cache miss, or through the process's pool when db is
// nil.
func (t *Tokens) decide(ctx context.Context, db repo.DBTX, organizationID pgtype.Text) (decision, error) {
	if platformOnly(organizationID) {
		return decision{platformOnly: true, d: orgencryption.Decision{OrganizationID: "", UseCase: "", State: "", Generation: 0}}, nil
	}

	var d orgencryption.Decision
	var err error
	if db == nil {
		d, err = t.cipher.Decide(ctx, organizationID.String, orgencryption.UseCaseOAuthSessionTokens)
	} else {
		d, err = t.cipher.DecideIn(ctx, db, organizationID.String, orgencryption.UseCaseOAuthSessionTokens)
	}
	if err != nil {
		return decision{}, fmt.Errorf("decide session token state: %w", err)
	}

	return decision{platformOnly: false, d: d}, nil
}

// redecide reads the organization's transition state again through db.
func (t *Tokens) redecide(ctx context.Context, db repo.DBTX, stale decision) (decision, error) {
	if stale.platformOnly {
		return stale, nil
	}

	d, err := t.cipher.RedecideIn(ctx, db, stale.d)
	if err != nil {
		return decision{}, fmt.Errorf("decide session token state again: %w", err)
	}

	return decision{platformOnly: false, d: d}, nil
}

// Open decrypts the copy of one value that the organization's transition
// state reads. db is whatever the caller already holds (a connection or a
// transaction); a cache miss reads transition state through it. A nil db
// reads through the process's pool, for callers that hold no connection.
//
// orgencryption.IsUnreadable reports a value that is missing or does not
// decrypt; every other error, such as an unusable organization key, may clear
// on its own and is never a reason to sign in again.
func (t *Tokens) Open(ctx context.Context, db repo.DBTX, organizationID pgtype.Text, columns orgencryption.CopyColumns, stored orgencryption.Copies) (string, error) {
	// An empty ciphertext stores nothing, so it reads as missing rather than
	// failing to decrypt.
	if stored.Platform.String == "" {
		stored.Platform = pgtype.Text{String: "", Valid: false}
	}
	if stored.Org.String == "" {
		stored.Org = pgtype.Text{String: "", Valid: false}
	}

	dc, err := t.decide(ctx, db, organizationID)
	if err != nil {
		return "", err
	}

	if dc.platformOnly {
		if !stored.Platform.Valid {
			return "", orgencryption.ErrMissing
		}

		plaintext, err := t.platform.Decrypt(stored.Platform.String)
		if err != nil {
			return "", fmt.Errorf("%w: %w", orgencryption.ErrUnreadable, err)
		}

		return plaintext, nil
	}

	plaintext, err := t.cipher.Open(ctx, dc.d, columns, stored)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", columns.OrgColumn, err)
	}

	return string(plaintext), nil
}

// RequireUsable checks that a value could be sealed for the organization now.
// Callers run it before an irreversible call to a provider whose result must
// then be stored, such as a refresh that may rotate the refresh token, and
// skip the call when it fails. db is whatever the caller already holds, or
// nil.
//
// It reads cached state, so a key revoked within the cache periods can still
// pass, and a write after the provider call can still fail. Then the
// provider's result is lost, and a rotated refresh token with it.
func (t *Tokens) RequireUsable(ctx context.Context, db repo.DBTX, organizationID pgtype.Text) error {
	dc, err := t.decide(ctx, db, organizationID)
	if err != nil {
		return err
	}
	if dc.platformOnly {
		return nil
	}

	if err := t.cipher.RequireUsable(ctx, dc.d); err != nil {
		return fmt.Errorf("session token key: %w", err)
	}

	return nil
}

// seal encrypts plaintext into the copies the decision writes.
func (t *Tokens) seal(ctx context.Context, dc decision, columns orgencryption.CopyColumns, plaintext string) (orgencryption.Copies, error) {
	if dc.platformOnly {
		platform, err := t.platform.Encrypt([]byte(plaintext))
		if err != nil {
			return orgencryption.Copies{}, fmt.Errorf("encrypt %s with platform key: %w", columns.PlatformColumn, err)
		}

		return orgencryption.Copies{Platform: conv.ToPGText(platform), Org: pgtype.Text{String: "", Valid: false}}, nil
	}

	copies, err := t.cipher.Seal(ctx, dc.d, columns, []byte(plaintext))
	if err != nil {
		return orgencryption.Copies{}, fmt.Errorf("seal %s: %w", columns.OrgColumn, err)
	}

	return copies, nil
}

// write decides, then runs attempt, which seals under the decision and runs
// one fenced statement. attempt reports whether the statement matched a row.
// When it did not, write reads the transition again: if the generation moved,
// it seals again under the new decision and retries; otherwise the row itself
// is missing and write returns notFound. It never asks the caller to repeat
// whatever produced the plaintext.
func (t *Tokens) write(ctx context.Context, dbtx repo.DBTX, organizationID pgtype.Text, notFound error, attempt func(decision) (bool, error)) error {
	dc, err := t.decide(ctx, dbtx, organizationID)
	if err != nil {
		return err
	}

	for range maxWriteAttempts {
		landed, err := attempt(dc)
		if errors.Is(err, orgencryption.ErrDecisionExpired) {
			dc, err = t.redecide(ctx, dbtx, dc)
			if err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if landed {
			return nil
		}
		if dc.platformOnly {
			return notFound
		}

		fresh, err := t.redecide(ctx, dbtx, dc)
		if err != nil {
			return err
		}
		if fresh.generation() == dc.generation() {
			return notFound
		}

		t.logger.InfoContext(ctx, "session token write fenced by a transition, sealing again",
			attr.SlogOrganizationID(organizationID.String), attr.SlogOrgEncryptionUseCase(orgencryption.UseCaseOAuthSessionTokens))
		dc = fresh
	}

	return fmt.Errorf("store session tokens: %w", orgencryption.ErrFenced)
}

// RemoteSessionTokens are the tokens a sign-in or refresh stores for a remote
// session.
type RemoteSessionTokens struct {
	// AccessToken is the access token. It is always stored, even when empty.
	AccessToken string

	// RefreshToken is the refresh token; empty stores none.
	RefreshToken string

	// KeepRefreshToken leaves both stored refresh token copies as they are,
	// for a refresh whose provider echoed the same refresh token. RefreshToken
	// is ignored when it is set.
	KeepRefreshToken bool
}

// SetRemoteSession stores a remote session's tokens and returns the updated
// row. Run it in the transaction of the statement that created or changed the
// session, which owns every other column, including updated_at.
// organizationID is the row's organization_id as that statement returned it.
func (t *Tokens) SetRemoteSession(ctx context.Context, dbtx repo.DBTX, sessionID uuid.UUID, organizationID pgtype.Text, tokens RemoteSessionTokens) (repo.RemoteSession, error) {
	var row repo.RemoteSession
	err := t.write(ctx, dbtx, organizationID, ErrSessionNotFound, func(dc decision) (bool, error) {
		access, err := t.seal(ctx, dc, RemoteSessionAccessToken, tokens.AccessToken)
		if err != nil {
			return false, err
		}

		refresh := orgencryption.ErasedCopies()
		if !tokens.KeepRefreshToken && tokens.RefreshToken != "" {
			refresh, err = t.seal(ctx, dc, RemoteSessionRefreshToken, tokens.RefreshToken)
			if err != nil {
				return false, err
			}
		}

		row, err = repo.New(dbtx).SetRemoteSessionTokens(ctx, repo.SetRemoteSessionTokensParams{
			AccessPlatformCopy:  access.Platform,
			AccessOrgCopy:       access.Org,
			ReplaceRefreshToken: !tokens.KeepRefreshToken,
			RefreshPlatformCopy: refresh.Platform,
			RefreshOrgCopy:      refresh.Org,
			ID:                  sessionID,
			OrganizationID:      organizationID,
			DecidedGeneration:   dc.generation(),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("store remote session tokens: %w", err)
		default:
			return true, nil
		}
	})
	if err != nil {
		return repo.RemoteSession{}, err
	}

	return row, nil
}

// SetEMACredential stores an EMA credential's access token and returns the
// stored copies. Run it in the transaction of the statement that published
// the slot, which owns every other column, including updated_at.
// organizationID is the slot's organization_id as that statement returned it.
func (t *Tokens) SetEMACredential(ctx context.Context, dbtx repo.DBTX, credentialID uuid.UUID, organizationID pgtype.Text, accessToken string) (repo.SetEMACredentialAccessTokenRow, error) {
	var row repo.SetEMACredentialAccessTokenRow
	err := t.write(ctx, dbtx, organizationID, ErrEMACredentialNotFound, func(dc decision) (bool, error) {
		access, err := t.seal(ctx, dc, EMACredentialAccessToken, accessToken)
		if err != nil {
			return false, err
		}

		row, err = repo.New(dbtx).SetEMACredentialAccessToken(ctx, repo.SetEMACredentialAccessTokenParams{
			PlatformCopy:      access.Platform,
			OrgCopy:           access.Org,
			ID:                credentialID,
			OrganizationID:    organizationID,
			DecidedGeneration: dc.generation(),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("store EMA credential access token: %w", err)
		default:
			return true, nil
		}
	})
	if err != nil {
		return repo.SetEMACredentialAccessTokenRow{}, err
	}

	return row, nil
}

// TrustedIssuerSessionValues are the values a trusted issuer session write
// stores. A nil value keeps both stored copies as they are; an empty one
// erases both.
type TrustedIssuerSessionValues struct {
	// IdentityAssertion is the identity assertion (ID token).
	IdentityAssertion *string

	// RefreshToken is the refresh token.
	RefreshToken *string

	// UpstreamSubject is the upstream subject identifier.
	UpstreamSubject *string

	// Nonce is the sign-in nonce the refresh family is bound to.
	Nonce *string
}

// sealedValue is one value of a write: whether it replaces the stored
// copies, and the copies that replace them.
type sealedValue struct {
	replace bool
	copies  orgencryption.Copies
}

// sealValue seals a value of a write: nil keeps the stored copies, an empty
// value erases them.
func (t *Tokens) sealValue(ctx context.Context, dc decision, columns orgencryption.CopyColumns, value *string) (sealedValue, error) {
	switch {
	case value == nil:
		return sealedValue{replace: false, copies: orgencryption.ErasedCopies()}, nil
	case *value == "":
		return sealedValue{replace: true, copies: orgencryption.ErasedCopies()}, nil
	}

	copies, err := t.seal(ctx, dc, columns, *value)
	if err != nil {
		return sealedValue{}, err
	}

	return sealedValue{replace: true, copies: copies}, nil
}

// SetTrustedIssuerSession stores, keeps or erases each value of a trusted
// issuer session and returns the updated row. Run it in the transaction of
// the statement that changed the session, which owns every other column,
// including updated_at. organizationID is the row's organization_id.
func (t *Tokens) SetTrustedIssuerSession(ctx context.Context, dbtx repo.DBTX, sessionID uuid.UUID, organizationID pgtype.Text, values TrustedIssuerSessionValues) (repo.TrustedIssuerSession, error) {
	var row repo.TrustedIssuerSession
	err := t.write(ctx, dbtx, organizationID, ErrTrustedIssuerSessionNotFound, func(dc decision) (bool, error) {
		assertion, err := t.sealValue(ctx, dc, TrustedIssuerSessionIdentityAssertion, values.IdentityAssertion)
		if err != nil {
			return false, err
		}
		refresh, err := t.sealValue(ctx, dc, TrustedIssuerSessionRefreshToken, values.RefreshToken)
		if err != nil {
			return false, err
		}
		subject, err := t.sealValue(ctx, dc, TrustedIssuerSessionUpstreamSubject, values.UpstreamSubject)
		if err != nil {
			return false, err
		}
		nonce, err := t.sealValue(ctx, dc, TrustedIssuerSessionNonce, values.Nonce)
		if err != nil {
			return false, err
		}

		row, err = repo.New(dbtx).SetTrustedIssuerSessionTokens(ctx, repo.SetTrustedIssuerSessionTokensParams{
			ReplaceIdentityAssertion:      assertion.replace,
			IdentityAssertionPlatformCopy: assertion.copies.Platform,
			IdentityAssertionOrgCopy:      assertion.copies.Org,
			ReplaceRefreshToken:           refresh.replace,
			RefreshTokenPlatformCopy:      refresh.copies.Platform,
			RefreshTokenOrgCopy:           refresh.copies.Org,
			ReplaceUpstreamSubject:        subject.replace,
			UpstreamSubjectPlatformCopy:   subject.copies.Platform,
			UpstreamSubjectOrgCopy:        subject.copies.Org,
			ReplaceNonce:                  nonce.replace,
			NoncePlatformCopy:             nonce.copies.Platform,
			NonceOrgCopy:                  nonce.copies.Org,
			ID:                            sessionID,
			OrganizationID:                organizationID,
			DecidedGeneration:             dc.generation(),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("store trusted issuer session tokens: %w", err)
		default:
			return true, nil
		}
	})
	if err != nil {
		return repo.TrustedIssuerSession{}, err
	}

	return row, nil
}
