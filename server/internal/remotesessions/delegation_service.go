package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Delegation errors are deliberately stable and contain no provider payloads.
var (
	ErrDelegationTemporary        = errors.New("delegation temporarily unavailable")
	ErrDelegationReauthentication = errors.New("delegation reauthentication required")
	ErrDelegationConfiguration    = errors.New("delegation configuration requires remediation")
)

// DelegationAssertion is an internal-only result, never a management API model.
type DelegationAssertion struct {
	value     string
	expiresAt time.Time
}

func (a DelegationAssertion) Value() string                { return a.value }
func (a DelegationAssertion) ExpiresAt() time.Time         { return a.expiresAt }
func (a DelegationAssertion) String() string               { return "[redacted delegation assertion]" }
func (a DelegationAssertion) GoString() string             { return a.String() }
func (a DelegationAssertion) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (a DelegationAssertion) LogValue() slog.Value         { return slog.StringValue(a.String()) }

// DelegationBinding describes the human whose authority the caller must prove.
// Client identifiers alone never authorize use of a retained credential.
type DelegationBinding struct {
	OrganizationID string
	IssuerID       uuid.UUID
	ClientID       uuid.UUID
	HumanID        string
}

// DelegationAuthorizer must check current issuer/client binding, organization
// access and human-delegation authority. Agent/workload authority is separate;
// retaining a human's credential never grants it to an autonomous agent.
type DelegationAuthorizer interface {
	AuthorizeDelegation(context.Context, DelegationBinding) error
}

type DelegationOfflineStatus struct {
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
	revoke(context.Context, DelegationBinding) error
	markRefreshAttempt(context.Context, DelegationBinding, int64, uuid.UUID, time.Time) (bool, error)
	load(context.Context, DelegationBinding) (delegationCredential, error)
	save(context.Context, DelegationBinding, int64, delegationCredential) (bool, error)
	claim(context.Context, DelegationBinding, int64, uuid.UUID, time.Time) (bool, error)
	finish(context.Context, DelegationBinding, int64, uuid.UUID, delegationCredential) (bool, error)
	clearExpired(context.Context, DelegationBinding, time.Time) error
}

// DelegationService lazily renews upstream credentials. A durable claim is never
// stolen after a lease timeout: an ambiguous rotating POST cannot be replayed.
// A new validated login is the recovery path for an abandoned claim.
type DelegationService struct {
	store           delegationStore
	enc             *encryption.Client
	manager         *ChallengeManager
	now             func() time.Time
	loadBinding     func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error)
	loadProvider    func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error)
	refreshIdentity func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error)
}

func NewDelegationService(db *pgxpool.Pool, enc *encryption.Client, manager *ChallengeManager) *DelegationService {
	return &DelegationService{store: &delegationRepository{db: db}, enc: enc, manager: manager, now: time.Now, loadBinding: manager.LoadFederatedDelegationProvider, loadProvider: manager.LoadFederatedProvider, refreshIdentity: manager.RefreshFederatedIdentity}
}
func delegationBinding(p *FederatedProvider, humanID string) DelegationBinding {
	return DelegationBinding{OrganizationID: p.organizationID, IssuerID: p.issuer.ID, ClientID: p.client.ID, HumanID: humanID}
}
func validDelegationBinding(b DelegationBinding) bool {
	if b.OrganizationID == "" || b.IssuerID == uuid.Nil || b.ClientID == uuid.Nil || b.HumanID == "" {
		return false
	}
	subject, err := urn.ParseSessionSubject(urn.NewUserSubject(b.HumanID).String())
	return err == nil && subject.String() == urn.NewUserSubject(b.HumanID).String()
}
func (s *DelegationService) encrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	encrypted, err := s.enc.Encrypt([]byte(value))
	if err != nil {
		return "", ErrDelegationTemporary
	}
	return encrypted, nil
}
func (s *DelegationService) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	decrypted, err := s.enc.Decrypt(value)
	if err != nil {
		return "", ErrDelegationConfiguration
	}
	return decrypted, nil
}
func usableRefresh(c delegationCredential, now time.Time) bool {
	return (c.status == "durable_credential_present" || c.status == "temporary_failure") && c.refresh != "" && (c.refreshExpiry.IsZero() || c.refreshExpiry.After(now)) && c.claim == uuid.Nil
}
func (s *DelegationService) OfflineStatus(ctx context.Context, p *FederatedProvider, humanID string) (DelegationOfflineStatus, error) {
	if p == nil || !validDelegationBinding(delegationBinding(p, humanID)) {
		return DelegationOfflineStatus{}, ErrDelegationConfiguration
	}
	hash := p.OfflineConfigurationHash()
	if hash == "" {
		return DelegationOfflineStatus{}, ErrDelegationConfiguration
	}
	b := delegationBinding(p, humanID)
	if err := s.store.clearExpired(ctx, b, s.now()); err != nil {
		return DelegationOfflineStatus{}, ErrDelegationTemporary
	}
	c, err := s.store.load(ctx, b)
	if errors.Is(err, pgx.ErrNoRows) {
		return DelegationOfflineStatus{UsableRefresh: false, Refused: false}, nil
	}
	if err != nil {
		return DelegationOfflineStatus{}, ErrDelegationTemporary
	}
	return DelegationOfflineStatus{UsableRefresh: c.config == p.DelegationConfigurationHash() && usableRefresh(c, s.now()), Refused: c.requestConfig == hash && !c.refusedAt.IsZero()}, nil
}

// RetainVerifiedLogin is only called synchronously from AIM-70's authorized
// handoff. Missing refresh credentials must never invalidate a successful login.
func (s *DelegationService) RetainVerifiedLogin(ctx context.Context, p *FederatedProvider, humanID string, identity *FederatedIdentity, offlineRequested bool) error {
	if p == nil || identity == nil || identity.Issuer != p.issuer.Issuer || identity.Subject == "" || !validDelegationBinding(delegationBinding(p, humanID)) {
		return ErrDelegationConfiguration
	}
	b := delegationBinding(p, humanID)
	return identity.WithCredentials(func(tokens EphemeralFederatedCredentials) error {
		now := s.now()
		hash := p.DelegationConfigurationHash()
		requestHash := p.OfflineConfigurationHash()
		if hash == "" || requestHash == "" {
			return ErrDelegationConfiguration
		}
		assertion := ""
		var err error
		if identity.ExpiresAt.After(now) {
			assertion, err = s.encrypt(tokens.IDToken())
			if err != nil {
				return err
			}
		}
		refresh, err := s.encrypt(tokens.RefreshToken())
		if err != nil {
			return err
		}
		subject, err := s.encrypt(identity.Subject)
		if err != nil {
			return err
		}
		nonce, err := s.encrypt(identity.Nonce)
		if err != nil {
			return err
		}
		expiry := time.Time{}
		if reported := tokens.RefreshExpiresAt(); reported != nil {
			expiry = *reported
		} else if tokens.RefreshExpiresIn() > 0 && tokens.RefreshExpiresIn() <= int64((1<<63-1)/time.Second) {
			expiry = tokens.ReceivedAt().Add(time.Duration(tokens.RefreshExpiresIn()) * time.Second)
		}
		if !expiry.IsZero() && !expiry.After(now) {
			refresh = ""
			expiry = time.Time{}
		}
		for range 3 {
			current, err := s.store.load(ctx, b)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ErrDelegationTemporary
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
				if plain != identity.Subject {
					return ErrDelegationConfiguration
				}
				sameIdentity = true
			}
			if !sameIdentity {
				var empty delegationCredential
				next = empty
			}
			next.assertion = assertion
			next.assertionExpiry = identity.ExpiresAt
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
			} else if !sameIdentity || current.claim != uuid.Nil {
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
				if !slices.Contains(p.client.Scope, "offline_access") {
					next.status = "offline_not_requested"
				} else if policy, err := p.OfflinePolicy(); err == nil && !policy.Enabled {
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
				return ErrDelegationTemporary
			}
			if ok {
				return nil
			}
		}
		return ErrDelegationTemporary
	})
}

func (s *DelegationService) RecordOfflineRefusal(ctx context.Context, p *FederatedProvider, humanID string) error {
	if p == nil || !validDelegationBinding(delegationBinding(p, humanID)) {
		return ErrDelegationConfiguration
	}
	b := delegationBinding(p, humanID)
	if err := s.store.clearExpired(ctx, b, s.now()); err != nil {
		return ErrDelegationTemporary
	}
	for range 3 {
		c, err := s.store.load(ctx, b)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrDelegationTemporary
		}
		// A concurrent callback may have obtained durable access already.
		if c.config == p.DelegationConfigurationHash() && (usableRefresh(c, s.now()) || c.claim != uuid.Nil) {
			return nil
		}
		if c.config != p.DelegationConfigurationHash() {
			c = clearDelegationSecrets(c)
		}
		c.refusedAt = s.now()
		c.requestConfig = p.OfflineConfigurationHash()
		c.config = p.DelegationConfigurationHash()
		c.status = "refused"
		c.observedAt = s.now()
		c = eraseExpiredDelegationSecrets(c, s.now())
		ok, e := s.store.save(ctx, b, c.generation, c)
		if e != nil {
			return ErrDelegationTemporary
		}
		if ok {
			return nil
		}
	}
	return ErrDelegationTemporary
}

// Resolve returns an assertion only after the caller proves current authority.
// It performs no proactive refresh and never falls back to another provider.
func (s *DelegationService) Resolve(ctx context.Context, b DelegationBinding, authority DelegationAuthorizer) (DelegationAssertion, error) {
	if authority == nil || !validDelegationBinding(b) {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	if err := authority.AuthorizeDelegation(ctx, b); err != nil {
		if definitiveDelegationFailure(err) {
			return DelegationAssertion{}, ErrDelegationConfiguration
		}
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	p, err := s.loadBinding(ctx, b.OrganizationID, b.IssuerID, b.ClientID)
	if err != nil {
		if definitiveDelegationFailure(err) {
			return DelegationAssertion{}, ErrDelegationConfiguration
		}
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if p == nil || p.DelegationConfigurationHash() == "" {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	now := s.now()
	if err = s.store.clearExpired(ctx, b, now); err != nil {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	c, err := s.store.load(ctx, b)
	if errors.Is(err, pgx.ErrNoRows) {
		return DelegationAssertion{}, ErrDelegationReauthentication
	}
	if err != nil {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if c.config != p.DelegationConfigurationHash() {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	if c.status == "configuration_failure" {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	if c.claim != uuid.Nil {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if c.assertion != "" && c.assertionExpiry.After(now.Add(time.Minute)) {
		return s.assertion(c)
	}
	// A successful refresh without an ID token cannot renew the portable path.
	// Keep its rotation, but do not spin through more grants on every tool call.
	if c.status == "reauthentication_required" {
		return DelegationAssertion{}, ErrDelegationReauthentication
	}
	if c.claim != uuid.Nil || c.retryAfter.After(now) {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if !usableRefresh(c, now) {
		return DelegationAssertion{}, ErrDelegationReauthentication
	}
	claim := uuid.New()
	acquired, err := s.store.claim(ctx, b, c.generation, claim, now)
	if err != nil || !acquired {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	// Re-read after durable ownership. A concurrent callback can supersede us.
	c, err = s.store.load(ctx, b)
	if err != nil || c.claim != claim {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	// Until the POST starts, releasing our own claim cannot replay a spent token.
	// A callback or revocation that supersedes us is protected by finish's CAS.
	refreshStarted := false
	defer func() {
		if !refreshStarted {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			next := eraseExpiredDelegationSecrets(c, s.now())
			next.claim = uuid.Nil
			_, _ = s.store.finish(releaseCtx, b, c.generation, claim, next)
		}
	}()
	// Load full discovery and credentials only after obtaining durable ownership.
	p, err = s.loadProvider(ctx, b.OrganizationID, b.IssuerID, b.ClientID)
	if err != nil {
		if definitiveDelegationFailure(err) {
			return DelegationAssertion{}, ErrDelegationConfiguration
		}
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if p == nil || p.DelegationConfigurationHash() != c.config {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	refresh, err := s.decrypt(c.refresh)
	if err != nil {
		return DelegationAssertion{}, err
	}
	subject, err := s.decrypt(c.subject)
	if err != nil {
		return DelegationAssertion{}, err
	}
	nonce, err := s.decrypt(c.nonce)
	if err != nil {
		return DelegationAssertion{}, err
	}
	if refresh == "" || subject == "" {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	marked, err := s.store.markRefreshAttempt(ctx, b, c.generation, claim, s.now())
	if err != nil || !marked {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	refreshStarted = true
	result, refreshErr := s.refreshIdentity(ctx, p, refresh, subject, nonce)
	// The caller's timeout must not prevent recording rotation or quarantining
	// an ambiguous generation. This bounded write is still CAS protected.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if refreshErr != nil {
		return DelegationAssertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, refreshErr)
	}
	if result == nil {
		return DelegationAssertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous})
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
			return DelegationAssertion{}, err
		}
	}
	// Lifetime updates are independent of token rotation. An explicit zero
	// expires the retained token; omission cannot remove a previously known bound.
	if expiry := result.Credentials.RefreshExpiresAt(); expiry != nil {
		next.refreshExpiry = *expiry
	}
	if !next.refreshExpiry.IsZero() && !next.refreshExpiry.After(s.now()) {
		next.refresh = ""
		next.refreshExpiry = time.Time{}
	}
	if identity := result.Identity; identity != nil {
		defer identity.DiscardCredentials()
		if identity.Subject != subject || identity.Issuer != p.issuer.Issuer {
			return DelegationAssertion{}, s.recordRefreshFailure(persistCtx, b, c, claim, &FederatedRefreshError{Kind: FederatedRefreshInvalidIdentity})
		}
		if identity.ExpiresAt.After(s.now()) {
			next.assertion, err = s.encrypt(result.Credentials.IDToken())
			if err != nil {
				return DelegationAssertion{}, err
			}
			next.assertionExpiry = identity.ExpiresAt
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
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if !ok {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if quarantined {
		return DelegationAssertion{}, ErrDelegationTemporary
	}
	if next.status == "configuration_failure" {
		return DelegationAssertion{}, ErrDelegationConfiguration
	}
	if next.assertion == "" || !next.assertionExpiry.After(s.now().Add(time.Minute)) {
		return DelegationAssertion{}, ErrDelegationReauthentication
	}
	return s.assertion(next)
}

// Authorizers must use a stable configuration/reauthentication error for a
// definitive denial. Unclassified dependency failures must never erase secrets.
func definitiveDelegationFailure(err error) bool {
	return errors.Is(err, ErrDelegationConfiguration) || errors.Is(err, ErrDelegationReauthentication) || errors.Is(err, ErrFederatedConfiguration) || errors.Is(err, pgx.ErrNoRows)
}

func (s *DelegationService) assertion(c delegationCredential) (DelegationAssertion, error) {
	if c.assertion == "" || !c.assertionExpiry.After(s.now().Add(time.Minute)) {
		return DelegationAssertion{}, ErrDelegationReauthentication
	}
	value, err := s.decrypt(c.assertion)
	if err != nil {
		return DelegationAssertion{}, err
	}
	return DelegationAssertion{value: value, expiresAt: c.assertionExpiry}, nil
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
func (s *DelegationService) recordRefreshFailure(ctx context.Context, b DelegationBinding, c delegationCredential, claim uuid.UUID, cause error) error {
	var failure *FederatedRefreshError
	if !errors.As(cause, &failure) {
		failure = &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	next := c
	next.observedAt = s.now()
	next.claim = uuid.Nil
	outcome := ErrDelegationTemporary
	switch failure.Kind {
	case FederatedRefreshInvalidGrant:
		next.refresh = ""
		next.refreshExpiry = time.Time{}
		next.status = "reauthentication_required"
		if !next.assertionExpiry.After(s.now()) {
			next.assertion = ""
			next.assertionExpiry = time.Time{}
			next.subject = ""
			next.nonce = ""
		}
		outcome = ErrDelegationReauthentication
	case FederatedRefreshConfiguration:
		next.status = "configuration_failure"
		outcome = ErrDelegationConfiguration
	case FederatedRefreshInvalidIdentity:
		next = clearDelegationSecrets(next)
		next.status = "configuration_failure"
		outcome = ErrDelegationConfiguration
	case FederatedRefreshRetryable:
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
		return ErrDelegationTemporary
	}
	return outcome
}

// Revoke explicitly erases secrets. The caller must authorize administrative
// revocation or current same-human ownership before invoking this internal API.
func (s *DelegationService) Revoke(ctx context.Context, b DelegationBinding, authority DelegationAuthorizer) error {
	if authority == nil || !validDelegationBinding(b) || authority.AuthorizeDelegation(ctx, b) != nil {
		return ErrDelegationConfiguration
	}
	if err := s.store.revoke(ctx, b); err != nil {
		return ErrDelegationTemporary
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
