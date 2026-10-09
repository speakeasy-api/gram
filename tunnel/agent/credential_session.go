package agent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/tunnel/identity"
)

var (
	errCredentialRejected = errors.New("upstream credential rejected")
	errCredentialExpired  = errors.New("upstream credential expired")
)

// credentialContext identifies the upstream grant a session's tokens come
// from. Refreshing a token keeps it; authorizing the grant again, switching
// upstream account, or a different client changes it.
type credentialContext struct {
	clientID        string
	grantID         string
	grantGeneration int64
}

// admittedCredential is a request's bearer, proven by the assertion to be
// the subject's own grant.
type admittedCredential struct {
	token   string
	context credentialContext
	// expiresAt is the token's expiry; zero when the upstream stated none.
	expiresAt time.Time
	// assertionExpiresAt bounds how long the admission itself stays valid.
	assertionExpiresAt time.Time
}

// admitCredential checks that the request carries exactly the bearer the
// assertion vouches for, from a grant the asserted subject owns.
func admitCredential(header http.Header, assertion callerAssertion, now time.Time) (admittedCredential, error) {
	var none admittedCredential
	token, ok := bearerToken(header)
	if !ok {
		return none, fmt.Errorf("%w: missing or malformed bearer", errCredentialRejected)
	}
	raw := bytes.TrimSpace(assertion.credential)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return none, fmt.Errorf("%w: assertion vouches for no upstream credential", errCredentialRejected)
	}

	var claim struct {
		Owner           *string         `json:"owner"`
		ClientID        *string         `json:"client_id"`
		GrantID         *string         `json:"grant_id"`
		GrantGeneration *json.Number    `json:"grant_generation"`
		TokenSHA256     *string         `json:"token_sha256"`
		TokenExpiresAt  json.RawMessage `json:"token_expires_at"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&claim); err != nil {
		return none, fmt.Errorf("%w: malformed credential claim", errCredentialRejected)
	}
	if claim.Owner == nil || *claim.Owner != identity.OwnerSubject {
		return none, fmt.Errorf("%w: credential is not the subject's own", errCredentialRejected)
	}
	clientID, okClient := canonicalUUID(claim.ClientID)
	grantID, okGrant := canonicalUUID(claim.GrantID)
	if !okClient || !okGrant || claim.GrantGeneration == nil {
		return none, fmt.Errorf("%w: incomplete grant identity", errCredentialRejected)
	}
	generation, err := claim.GrantGeneration.Int64()
	if err != nil || generation < 1 {
		return none, fmt.Errorf("%w: invalid grant generation", errCredentialRejected)
	}
	if claim.TokenSHA256 == nil || !isLowerHexSHA256(*claim.TokenSHA256) {
		return none, fmt.Errorf("%w: invalid token digest", errCredentialRejected)
	}
	if subtle.ConstantTimeCompare([]byte(identity.TokenSHA256(token)), []byte(*claim.TokenSHA256)) != 1 {
		return none, fmt.Errorf("%w: bearer does not match the vouched token", errCredentialRejected)
	}

	admitted := admittedCredential{
		token:              token,
		context:            credentialContext{clientID: clientID, grantID: grantID, grantGeneration: generation},
		expiresAt:          time.Time{},
		assertionExpiresAt: assertion.expiresAt,
	}
	// A RawMessage field keeps an explicit null, which is invalid: an unknown
	// expiry is stated by omitting the field.
	if claim.TokenExpiresAt != nil {
		var seconds json.Number
		expDec := json.NewDecoder(bytes.NewReader(claim.TokenExpiresAt))
		expDec.UseNumber()
		if expDec.Decode(&seconds) != nil {
			return none, fmt.Errorf("%w: invalid token expiry", errCredentialRejected)
		}
		unix, err := seconds.Int64()
		if err != nil {
			return none, fmt.Errorf("%w: invalid token expiry", errCredentialRejected)
		}
		admitted.expiresAt = time.Unix(unix, 0)
		if !now.Before(admitted.expiresAt) {
			// The credential is otherwise valid: callers still compare its
			// grant context, so return it with the error.
			return admitted, errCredentialExpired
		}
	}
	return admitted, nil
}

// expired reports whether the token, or the assertion that admitted it, has
// expired. An expired token is never written.
func (c admittedCredential) expired(now time.Time) bool {
	return (!c.expiresAt.IsZero() && !now.Before(c.expiresAt)) || !now.Before(c.assertionExpiresAt.Add(identity.ClockSkew))
}

func canonicalUUID(raw *string) (string, bool) {
	if raw == nil {
		return "", false
	}
	id, err := uuid.Parse(*raw)
	if err != nil || id == uuid.Nil || id.String() != *raw {
		return "", false
	}
	return *raw, true
}

func isLowerHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && s == string(bytes.ToLower([]byte(s)))
}

// sessionCredentials is a credentials-mode session's binding and token state.
//
// One admission gate serializes every change to it: publishing a token,
// arming its expiry, and writing the admitted request to the server's stdin
// all happen while holding the gate, and request-driven termination takes the
// gate too. A termination therefore orders after any write already admitted,
// and once the session is closing nothing new is published or forwarded.
type sessionCredentials struct {
	principal principal
	context   credentialContext
	dir       credentialDir
	maxAge    time.Duration
	// expiryGrace keeps the session past its token's expiry, waiting for a
	// refreshed token.
	expiryGrace time.Duration
	now         func() time.Time

	// afterPublish, when set, runs between publishing a token and forwarding
	// the request, so tests can interleave termination there.
	afterPublish func()

	// gate is the admission gate; holding it means a send on the channel.
	gate chan struct{}

	// generation and timer are guarded by gate. generation counts token
	// writes so a stale expiry callback can tell it was superseded.
	generation uint64
	timer      *time.Timer
}

func newSessionCredentials(p principal, c credentialContext, dir credentialDir, maxAge, expiryGrace time.Duration, now func() time.Time) *sessionCredentials {
	return &sessionCredentials{
		principal:    p,
		context:      c,
		dir:          dir,
		maxAge:       maxAge,
		expiryGrace:  expiryGrace,
		now:          now,
		afterPublish: nil,
		gate:         make(chan struct{}, 1),
		generation:   0,
		timer:        nil,
	}
}

// deadline is how long after a write the session keeps the token: until its
// expiry plus a grace period, and never past the maximum credential age.
// Timers run on the monotonic clock, so moving the wall clock back cannot
// extend the maximum age.
func (c *sessionCredentials) deadline(cred admittedCredential, now time.Time) time.Duration {
	d := c.maxAge
	if !cred.expiresAt.IsZero() {
		d = min(d, cred.expiresAt.Sub(now)+c.expiryGrace)
	}
	return d
}

// enterGate takes the admission gate unless ctx ends or the session is done.
func (s *stdioSession) enterGate(ctx context.Context) bool {
	select {
	case s.cred.gate <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-s.done:
		return false
	}
}

func (s *stdioSession) leaveGate() { <-s.cred.gate }

// publishLocked writes the token and arms its expiry. The caller holds the
// gate and has checked the session is not closing.
func (s *stdioSession) publishLocked(cred admittedCredential) error {
	now := s.cred.now()
	if cred.expired(now) {
		return errCredentialExpired
	}
	if err := s.cred.dir.writeToken(cred.token); err != nil {
		return fmt.Errorf("write session token: %w", err)
	}
	s.cred.generation++
	generation := s.cred.generation
	if s.cred.timer != nil {
		s.cred.timer.Stop()
	}
	s.cred.timer = time.AfterFunc(s.cred.deadline(cred, now), func() { s.expireCredential(generation) })
	return nil
}

// sendCredentialed publishes the request's token and forwards msgs while
// holding the gate. A token or assertion that expired while the request
// waited is refused without ending the session: a stale request says nothing
// about the session's newest token, and the deadline still bounds it. Any
// other failure after admission ends the session.
func (s *stdioSession) sendCredentialed(ctx context.Context, msgs []rpcMessage, cred admittedCredential) error {
	if !s.enterGate(ctx) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errStdioSessionClosed
	}
	defer s.leaveGate()
	if s.closing.Load() {
		return errStdioSessionClosed
	}
	if err := s.publishLocked(cred); err != nil {
		if errors.Is(err, errCredentialExpired) {
			return err
		}
		s.logger.Warn("tunnel stdio session token could not be written; stopping server", slog.Any("error", err))
		s.close()
		return err
	}
	if s.cred.afterPublish != nil {
		s.cred.afterPublish()
	}
	// A request admitted with a published token that cannot be forwarded,
	// whether stdin is closed, a write failed part way, or the caller gave up
	// waiting, ends the session before the gate is released.
	if err := s.send(ctx, msgs); err != nil {
		s.close()
		return err
	}
	return nil
}

// expireCredential ends the session unless a newer token superseded the one
// whose deadline fired.
func (s *stdioSession) expireCredential(generation uint64) {
	if !s.enterGate(context.Background()) {
		return
	}
	defer s.leaveGate()
	if s.closing.Load() || generation != s.cred.generation {
		return
	}
	s.logger.Info("tunnel stdio session credential expired without a newer one; stopping server")
	s.close()
}

// beginTerminate stops admitting requests at once, then ends the session in
// the background after any write already admitted finishes.
func (s *stdioSession) beginTerminate() {
	s.closing.Store(true)
	go s.terminate()
}

// terminate ends the session after any admitted write finishes.
func (s *stdioSession) terminate() {
	if s.cred == nil {
		s.close()
		return
	}
	if s.enterGate(context.Background()) {
		defer s.leaveGate()
	}
	s.close()
}

// removeCredentials deletes the session's storage once nothing can publish
// to it any more: the session is closing and the gate is free. It waits for
// an admitted publisher, whose token write and stdin write must finish before
// the gate is free, rather than pull its directory out from under it.
func (s *stdioSession) removeCredentials() {
	s.cred.gate <- struct{}{}
	defer s.leaveGate()
	if s.cred.timer != nil {
		s.cred.timer.Stop()
	}
	if err := s.cred.dir.remove(); err != nil {
		s.logger.Warn("tunnel stdio session credentials could not be removed", slog.Any("error", err))
	}
}
