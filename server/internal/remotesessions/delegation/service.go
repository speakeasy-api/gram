package delegation

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Delegation errors are deliberately stable and contain no provider payloads.
var (
	ErrTemporary        = errors.New("delegation temporarily unavailable")
	ErrReauthentication = errors.New("delegation reauthentication required")
	ErrConfiguration    = errors.New("delegation configuration requires remediation")
)

// Assertion is an internal-only result, never a management API model.
type Assertion struct {
	value     string
	expiresAt time.Time
}

func (a Assertion) Value() string                { return a.value }
func (a Assertion) ExpiresAt() time.Time         { return a.expiresAt }
func (a Assertion) String() string               { return "[redacted delegation assertion]" }
func (a Assertion) GoString() string             { return a.String() }
func (a Assertion) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (a Assertion) LogValue() slog.Value         { return slog.StringValue(a.String()) }

// Binding describes the human whose authority the caller must prove.
// Client identifiers alone never authorize use of a retained credential.
type Binding struct {
	OrganizationID string
	IssuerID       uuid.UUID
	ClientID       uuid.UUID
	HumanID        string
}

// Authorizer must check current issuer/client binding, organization
// access and human-delegation authority. Agent/workload authority is separate;
// retaining a human's credential never grants it to an autonomous agent.
type Authorizer interface {
	AuthorizeDelegation(context.Context, Binding) error
}

type OfflineStatus struct {
	UsableRefresh bool
	Refused       bool
}

type delegationCredential struct {
	generation      int64
	claim           uuid.UUID
	assertion       string
	assertionExpiry time.Time
	refresh         string
	refreshExpiry   time.Time
	subject         string
	nonce           string
	config          string
	refusedAt       time.Time
	requestConfig   string
	status          string
	observedAt      time.Time
	obtainedAt      time.Time
	refreshedAt     time.Time
	retryAfter      time.Time
}

func (c delegationCredential) String() string               { return "[encrypted delegation credential]" }
func (c delegationCredential) GoString() string             { return c.String() }
func (c delegationCredential) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

type delegationStore interface {
	release(context.Context, Binding, int64, uuid.UUID) (bool, error)
	revoke(context.Context, Binding) error
	markRefreshAttempt(context.Context, Binding, int64, uuid.UUID, time.Time) (bool, error)
	load(context.Context, Binding) (delegationCredential, error)
	save(context.Context, Binding, int64, delegationCredential) (bool, error)
	claim(context.Context, Binding, int64, uuid.UUID, time.Time) (bool, error)
	finish(context.Context, Binding, int64, uuid.UUID, delegationCredential) (bool, error)
	clearExpired(context.Context, Binding, time.Time) error
}

// Service lazily renews upstream credentials. A durable claim is never
// stolen after a lease timeout: an ambiguous rotating POST cannot be replayed.
// A new validated login is the recovery path for an abandoned claim.
type Service struct {
	store           delegationStore
	enc             *encryption.Client
	now             func() time.Time
	loadBinding     func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error)
	loadProvider    func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error)
	refreshIdentity func(context.Context, Provider, string, string, string) (*RefreshResult, error)
}

func New(db *pgxpool.Pool, enc *encryption.Client, dependencies Dependencies) *Service {
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: &delegationRepository{db: db}, enc: enc, now: now, loadBinding: dependencies.LoadBinding, loadProvider: dependencies.LoadProvider, refreshIdentity: dependencies.RefreshIdentity}
}
func delegationBinding(p Provider, humanID string) Binding { return p.Binding(humanID) }
func validDelegationBinding(b Binding) bool {
	if b.OrganizationID == "" || b.IssuerID == uuid.Nil || b.ClientID == uuid.Nil || b.HumanID == "" {
		return false
	}
	subject, err := urn.ParseSessionSubject(urn.NewUserSubject(b.HumanID).String())
	return err == nil && subject.String() == urn.NewUserSubject(b.HumanID).String()
}
func (s *Service) encrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	encrypted, err := s.enc.Encrypt([]byte(value))
	if err != nil {
		return "", ErrTemporary
	}
	return encrypted, nil
}
func (s *Service) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	decrypted, err := s.enc.Decrypt(value)
	if err != nil {
		return "", ErrConfiguration
	}
	return decrypted, nil
}
func usableRefresh(c delegationCredential, now time.Time) bool {
	return (c.status == "durable_credential_present" || c.status == "temporary_failure") && c.refresh != "" && (c.refreshExpiry.IsZero() || c.refreshExpiry.After(now)) && c.claim == uuid.Nil
}
func (s *Service) OfflineStatus(ctx context.Context, p Provider, humanID string) (OfflineStatus, error) {
	if p == nil || !validDelegationBinding(delegationBinding(p, humanID)) {
		return OfflineStatus{}, ErrConfiguration
	}
	hash := p.OfflineConfigurationHash()
	if hash == "" {
		return OfflineStatus{}, ErrConfiguration
	}
	b := delegationBinding(p, humanID)
	if err := s.store.clearExpired(ctx, b, s.now()); err != nil {
		return OfflineStatus{}, ErrTemporary
	}
	c, err := s.store.load(ctx, b)
	if errors.Is(err, pgx.ErrNoRows) {
		return OfflineStatus{UsableRefresh: false, Refused: false}, nil
	}
	if err != nil {
		return OfflineStatus{}, ErrTemporary
	}
	return OfflineStatus{UsableRefresh: c.config == p.DelegationConfigurationHash() && usableRefresh(c, s.now()), Refused: c.requestConfig == hash && !c.refusedAt.IsZero()}, nil
}

// RetainVerifiedLogin is only called synchronously from AIM-70's authorized
// handoff. Missing refresh credentials must never invalidate a successful login.
func (s *Service) RetainVerifiedLogin(ctx context.Context, p Provider, humanID string, identity Identity, offlineRequested bool) error {
	if p == nil || identity == nil || identity.IssuerURL() != p.IssuerURL() || identity.SubjectID() == "" || !validDelegationBinding(delegationBinding(p, humanID)) {
		return ErrConfiguration
	}
	b := delegationBinding(p, humanID)
	// Preserve the one-shot handoff callback’s stable sentinel errors unchanged.
	return identity.WithCredentials(func(tokens Credentials) error { //nolint:wrapcheck // This boundary forwards the credential consumer error contract.
		now := s.now()
		hash := p.DelegationConfigurationHash()
		requestHash := p.OfflineConfigurationHash()
		if hash == "" || requestHash == "" {
			return ErrConfiguration
		}
		assertion := ""
		var err error
		if identity.Expiration().After(now) {
			assertion, err = s.encrypt(tokens.IDToken())
			if err != nil {
				return err
			}
		}
		refresh, err := s.encrypt(tokens.RefreshToken())
		if err != nil {
			return err
		}
		subject, err := s.encrypt(identity.SubjectID())
		if err != nil {
			return err
		}
		nonce, err := s.encrypt(identity.NonceValue())
		if err != nil {
			return err
		}
		expiry := time.Time{}
		expiryKnown := false
		if reported := tokens.RefreshExpiresAt(); reported != nil {
			expiry = *reported
			expiryKnown = true
		} else if tokens.RefreshExpiresIn() > 0 && tokens.RefreshExpiresIn() <= int64((1<<63-1)/time.Second) {
			expiry = tokens.ReceivedAt().Add(time.Duration(tokens.RefreshExpiresIn()) * time.Second)
			expiryKnown = true
		}
		refreshExpired := expiryKnown && !expiry.After(now)
		if refreshExpired {
			refresh = ""
			expiry = time.Time{}
		}
		for range 3 {
			current, err := s.store.load(ctx, b)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ErrTemporary
			}
			next := current
			// A callback installs a new generation; a late refresh cannot overwrite it.
			next.claim = uuid.Nil
			next.retryAfter = time.Time{}
			sameIdentity := current.config == hash && current.subject == "" && current.refresh == ""
			if current.config == hash && current.subject != "" {
				plain, e := s.decrypt(current.subject)
				if e != nil {
					return e
				}
				if plain != identity.SubjectID() {
					return ErrConfiguration
				}
				sameIdentity = true
			}
			if !sameIdentity {
				var empty delegationCredential
				next = empty
			}
			next.assertion = assertion
			next.assertionExpiry = identity.Expiration()
			if assertion == "" {
				next.assertionExpiry = time.Time{}
			}
			next.subject = subject
			next.nonce = nonce
			next.config = hash
			next.observedAt = now
			// A preserved refresh family remains bound to its original login nonce.
			if refresh == "" && sameIdentity && current.claim == uuid.Nil {
				next.nonce = current.nonce
			}
			// Do not resurrect a possibly spent refresh token from an in-flight claim.
			if refresh != "" {
				next.refresh = refresh
				next.refreshExpiry = expiry
				next.obtainedAt = now
			} else if refreshExpired || !sameIdentity || current.claim != uuid.Nil {
				next.refresh = ""
				next.refreshExpiry = time.Time{}
			}
			if !next.refreshExpiry.IsZero() && !next.refreshExpiry.After(now) {
				next.refresh = ""
				next.refreshExpiry = time.Time{}
			}
			if next.assertion == "" && next.refresh == "" {
				next.subject = ""
				next.nonce = ""
			}
			next.status = "assertion_only"
			if !offlineRequested {
				if !p.OfflineRequested() {
					next.status = "offline_not_requested"
				} else if !p.OfflineSupported() {
					next.status = "offline_unsupported"
				}
			}
			if next.refresh != "" {
				next.status = "durable_credential_present"
				next.refusedAt = time.Time{}
				next.requestConfig = requestHash
			} else if offlineRequested {
				next.refusedAt = now
				next.requestConfig = requestHash
				next.status = "refused"
			}
			if !next.refusedAt.IsZero() && next.requestConfig == requestHash && next.refresh == "" {
				next.status = "refused"
			}
			next = eraseExpiredDelegationSecrets(next, s.now())
			ok, e := s.store.save(ctx, b, current.generation, next)
			if e != nil {
				return ErrTemporary
			}
			if ok {
				return nil
			}
		}
		return ErrTemporary
	})
}

func (s *Service) RecordOfflineRefusal(ctx context.Context, p Provider, humanID string) error {
	if p == nil || !validDelegationBinding(delegationBinding(p, humanID)) {
		return ErrConfiguration
	}
	hash, requestHash := p.DelegationConfigurationHash(), p.OfflineConfigurationHash()
	if hash == "" || requestHash == "" {
		return ErrConfiguration
	}
	b := delegationBinding(p, humanID)
	if err := s.store.clearExpired(ctx, b, s.now()); err != nil {
		return ErrTemporary
	}
	for range 3 {
		c, err := s.store.load(ctx, b)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrTemporary
		}
		// A concurrent callback may have obtained durable access already.
		if c.config == hash && (usableRefresh(c, s.now()) || c.claim != uuid.Nil) {
			return nil
		}
		if c.config != hash {
			c = clearDelegationSecrets(c)
		}
		c.refusedAt = s.now()
		c.requestConfig = requestHash
		c.config = hash
		c.status = "refused"
		c.observedAt = s.now()
		c = eraseExpiredDelegationSecrets(c, s.now())
		ok, e := s.store.save(ctx, b, c.generation, c)
		if e != nil {
			return ErrTemporary
		}
		if ok {
			return nil
		}
	}
	return ErrTemporary
}

// Resolve returns an assertion only after the caller proves current authority.
// It performs no proactive refresh and never falls back to another provider.
func (s *Service) Resolve(ctx context.Context, b Binding, authority Authorizer) (Assertion, error) {
	if authority == nil || !validDelegationBinding(b) {
		return Assertion{}, ErrConfiguration
	}
	if err := authority.AuthorizeDelegation(ctx, b); err != nil {
		if definitiveDelegationFailure(err) {
			return Assertion{}, ErrConfiguration
		}
		return Assertion{}, ErrTemporary
	}
	p, err := s.loadBinding(ctx, b.OrganizationID, b.IssuerID, b.ClientID)
	if err != nil {
		if definitiveDelegationFailure(err) {
			return Assertion{}, ErrConfiguration
		}
		return Assertion{}, ErrTemporary
	}
	if p == nil || p.DelegationConfigurationHash() == "" {
		return Assertion{}, ErrConfiguration
	}
	now := s.now()
	if err = s.store.clearExpired(ctx, b, now); err != nil {
		return Assertion{}, ErrTemporary
	}
	c, err := s.store.load(ctx, b)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assertion{}, ErrReauthentication
	}
	if err != nil {
		return Assertion{}, ErrTemporary
	}
	if c.config != p.DelegationConfigurationHash() {
		return Assertion{}, ErrConfiguration
	}
	if c.status == "configuration_failure" {
		return Assertion{}, ErrConfiguration
	}
	if c.claim != uuid.Nil {
		return Assertion{}, ErrTemporary
	}
	if c.assertion != "" && c.assertionExpiry.After(now.Add(time.Minute)) {
		return s.assertion(c)
	}
	// A successful refresh without an ID token cannot renew the portable path.
	// Keep its rotation, but do not spin through more grants on every tool call.
	if c.status == "reauthentication_required" {
		return Assertion{}, ErrReauthentication
	}
	if c.claim != uuid.Nil || c.retryAfter.After(now) {
		return Assertion{}, ErrTemporary
	}
	if !usableRefresh(c, now) {
		return Assertion{}, ErrReauthentication
	}
	claim := uuid.New()
	acquired, err := s.store.claim(ctx, b, c.generation, claim, now)
	if err != nil || !acquired {
		return Assertion{}, ErrTemporary
	}
	// Claim acquisition advances the generation. Keep its snapshot independent
	// of the reread, which may fail or return a superseding callback's row.
	claimed := c
	claimed.generation++
	// Until the POST starts, releasing our own claim cannot replay a spent token.
	// A callback or revocation that supersedes us is protected by release's CAS.
	refreshStarted := false
	defer func() {
		if !refreshStarted {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = s.store.release(releaseCtx, b, claimed.generation, claim)
		}
	}()
	// Re-read after durable ownership. A concurrent callback can supersede us.
	c, err = s.store.load(ctx, b)
	if err != nil || c.claim != claim {
		return Assertion{}, ErrTemporary
	}
	// Load full discovery and credentials only after obtaining durable ownership.
	p, err = s.loadProvider(ctx, b.OrganizationID, b.IssuerID, b.ClientID)
	if err != nil {
		if definitiveDelegationFailure(err) {
			return Assertion{}, ErrConfiguration
		}
		return Assertion{}, ErrTemporary
	}
	if p == nil || p.DelegationConfigurationHash() != c.config {
		return Assertion{}, ErrConfiguration
	}
	refresh, err := s.decrypt(c.refresh)
	if err != nil {
		return Assertion{}, err
	}
	subject, err := s.decrypt(c.subject)
	if err != nil {
		return Assertion{}, err
	}
	nonce, err := s.decrypt(c.nonce)
	if err != nil {
		return Assertion{}, err
	}
	if refresh == "" || subject == "" {
		return Assertion{}, ErrConfiguration
	}
	marked, err := s.store.markRefreshAttempt(ctx, b, c.generation, claim, s.now())
	if err != nil || !marked {
		return Assertion{}, ErrTemporary
	}
	refreshStarted = true
	result, refreshErr := s.refreshIdentity(ctx, p, refresh, subject, nonce)
	// The caller's timeout must not prevent recording rotation or quarantining
	// an ambiguous generation. This bounded write is still CAS protected.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if refreshErr != nil {
		return Assertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, refreshErr)
	}
	if result == nil || result.Credentials == nil {
		return Assertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, &RefreshError{Kind: RefreshAmbiguous})
	}
	next := c
	next.observedAt = s.now()
	next.refreshedAt = s.now()
	next.status = "reauthentication_required"
	next.claim = uuid.Nil
	next.retryAfter = time.Time{}
	next.assertion = ""
	next.assertionExpiry = time.Time{}
	if rotation := result.Credentials.RefreshToken(); rotation != "" {
		next.refresh, err = s.encrypt(rotation)
		if err != nil {
			return Assertion{}, err
		}
	}
	// Lifetime updates are independent of token rotation. An explicit zero
	// expires the retained token; omission cannot remove a previously known bound.
	if expiry := result.Credentials.RefreshExpiresAt(); expiry != nil {
		next.refreshExpiry = *expiry
		// A present zero is an expired bound, not the omitted/unknown sentinel.
		if !expiry.After(s.now()) {
			next.refresh = ""
		}
	}
	if !next.refreshExpiry.IsZero() && !next.refreshExpiry.After(s.now()) {
		next.refresh = ""
		next.refreshExpiry = time.Time{}
	}
	if identity := result.Identity; identity != nil {
		defer identity.DiscardCredentials()
		if identity.SubjectID() != subject || identity.IssuerURL() != p.IssuerURL() {
			return Assertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, &RefreshError{Kind: RefreshInvalidIdentity})
		}
		if identity.Expiration().After(s.now()) {
			next.assertion, err = s.encrypt(result.Credentials.IDToken())
			if err != nil {
				return Assertion{}, err
			}
			next.assertionExpiry = identity.Expiration()
			next.status = "assertion_only"
			if next.refresh != "" {
				next.status = "durable_credential_present"
			}
		}
	}
	// Persist even when no ID token was returned: losing a rotated refresh token
	// must not cause a replay of the spent one. Such a response is not an assertion.
	live, liveErr := s.loadBinding(persistCtx, b.OrganizationID, b.IssuerID, b.ClientID)
	authorityErr := authority.AuthorizeDelegation(persistCtx, b)
	definitive := definitiveDelegationFailure(liveErr) || definitiveDelegationFailure(authorityErr) || (liveErr == nil && (live == nil || live.DelegationConfigurationHash() != c.config))
	quarantined := !definitive && (liveErr != nil || authorityErr != nil)
	if definitive {
		next = clearDelegationSecrets(next)
		next.status = "configuration_failure"
	} else if quarantined {
		// Preserve rotation, but do not expose it or replay this generation when
		// verification dependencies cannot establish current authority.
		next.claim = claim
		next.status = "temporary_failure"
		next.retryAfter = s.now().Add(time.Minute)
	}
	next = eraseExpiredDelegationSecrets(next, s.now())
	if (next.assertion == "" || !next.assertionExpiry.After(s.now().Add(time.Minute))) && next.status != "configuration_failure" && !quarantined {
		next.status = "reauthentication_required"
	}
	ok, err := s.store.finish(persistCtx, b, c.generation, claim, next)
	if err != nil {
		return Assertion{}, ErrTemporary
	}
	if !ok {
		return Assertion{}, ErrTemporary
	}
	if quarantined {
		return Assertion{}, ErrTemporary
	}
	if next.status == "configuration_failure" {
		return Assertion{}, ErrConfiguration
	}
	if next.assertion == "" || !next.assertionExpiry.After(s.now().Add(time.Minute)) {
		return Assertion{}, ErrReauthentication
	}
	return s.assertion(next)
}

// Authorizers must use a stable configuration/reauthentication error for a
// definitive denial. Unclassified dependency failures must never erase secrets.
func definitiveDelegationFailure(err error) bool {
	return errors.Is(err, ErrConfiguration) || errors.Is(err, ErrReauthentication) || errors.Is(err, pgx.ErrNoRows)
}

func (s *Service) assertion(c delegationCredential) (Assertion, error) {
	if c.assertion == "" || !c.assertionExpiry.After(s.now().Add(time.Minute)) {
		return Assertion{}, ErrReauthentication
	}
	value, err := s.decrypt(c.assertion)
	if err != nil {
		return Assertion{}, err
	}
	return Assertion{value: value, expiresAt: c.assertionExpiry}, nil
}
func clearDelegationSecrets(c delegationCredential) delegationCredential {
	c.assertion = ""
	c.assertionExpiry = time.Time{}
	c.refresh = ""
	c.refreshExpiry = time.Time{}
	c.subject = ""
	c.nonce = ""
	c.claim = uuid.Nil
	return c
}
func (s *Service) recordRefreshFailure(ctx context.Context, b Binding, c delegationCredential, claim uuid.UUID, cause error) error {
	var failure *RefreshError
	if !errors.As(cause, &failure) {
		failure = &RefreshError{Kind: RefreshAmbiguous}
	}
	next := c
	next.observedAt = s.now()
	next.claim = uuid.Nil
	outcome := ErrTemporary
	switch failure.Kind {
	case RefreshInvalidGrant:
		next.refresh = ""
		next.refreshExpiry = time.Time{}
		next.status = "reauthentication_required"
		if !next.assertionExpiry.After(s.now()) {
			next.assertion = ""
			next.assertionExpiry = time.Time{}
			next.subject = ""
			next.nonce = ""
		}
		outcome = ErrReauthentication
	case RefreshConfiguration:
		next.status = "configuration_failure"
		outcome = ErrConfiguration
	case RefreshInvalidIdentity:
		next = clearDelegationSecrets(next)
		next.status = "configuration_failure"
		outcome = ErrConfiguration
	case RefreshRetryable:
		next.status = "temporary_failure"
		next.retryAfter = s.now().Add(time.Minute)
	default:
		// The upstream may have rotated its token. Never clear/expire this claim;
		// reauthentication, not automatic replay, is the safe recovery path.
		next.claim = claim
		next.status = "temporary_failure"
		next.retryAfter = s.now().Add(time.Minute)
	}
	next = eraseExpiredDelegationSecrets(next, s.now())
	ok, err := s.store.finish(ctx, b, c.generation, claim, next)
	if err != nil || !ok {
		return ErrTemporary
	}
	return outcome
}

// Revoke explicitly erases secrets. The caller must authorize administrative
// revocation or current same-human ownership before invoking this internal API.
func (s *Service) Revoke(ctx context.Context, b Binding, authority Authorizer) error {
	if authority == nil || !validDelegationBinding(b) {
		return ErrConfiguration
	}
	if err := authority.AuthorizeDelegation(ctx, b); err != nil {
		if definitiveDelegationFailure(err) {
			return ErrConfiguration
		}
		return ErrTemporary
	}
	if err := s.store.revoke(ctx, b); err != nil {
		return ErrTemporary
	}
	return nil
}

// Expiry is checked again immediately before every write, including after slow
// provider/configuration reads. Unknown refresh expiry remains unknown.
func eraseExpiredDelegationSecrets(c delegationCredential, now time.Time) delegationCredential {
	if !c.assertionExpiry.After(now) {
		c.assertion = ""
		c.assertionExpiry = time.Time{}
	}
	if !c.refreshExpiry.IsZero() && !c.refreshExpiry.After(now) {
		c.refresh = ""
		c.refreshExpiry = time.Time{}
	}
	if c.assertion == "" && c.refresh == "" {
		c.subject = ""
		c.nonce = ""
	}
	return c
}
