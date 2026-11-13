package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"
)

const (
	// stateStoreTimeout bounds each state read or write, as the original
	// Postgres store does.
	stateStoreTimeout = 10 * time.Second

	// certificateLeaseDuration is how long a lease lasts if its holder never
	// releases it. It outlasts certificateFetchTimeout plus the release's
	// stateStoreTimeout, so a live request never loses its lease.
	certificateLeaseDuration = 3 * time.Minute
)

// errStateNotExist stands in for ipn.ErrStateNotExist.
var errStateNotExist = errors.New("no state")

// certificateIssuer is implemented by state stores that share a certificate
// across a connection's slots: a slot reads the stored certificate, and
// claims the issuance lease before requesting a missing one or renewing it.
type certificateIssuer interface {
	ReadTLSCertAndKey(domain string) ([]byte, []byte, error)
	ClaimCertificateIssuance(ctx context.Context) (func(), bool, error)
}

// certificateSource is where a node gets its certificate.
type certificateSource interface {
	// Stored returns the connection's stored certificate, or nil when none
	// is stored or the store does not share certificates.
	Stored(ctx context.Context) (*tls.Certificate, error)

	// Obtain returns a certificate for a node holding none: the stored one
	// when it is valid, otherwise a new one, ordered under the lease.
	Obtain(ctx context.Context, now time.Time) (*tls.Certificate, error)

	// Renew replaces held under the lease, or returns the certificate
	// another slot already renewed it into.
	Renew(ctx context.Context, held *tls.Certificate, now time.Time) (*tls.Certificate, error)
}

// certPairFunc asks tsnet for the certificate for domain, ordering one if
// none is stored.
type certPairFunc func(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error)

// certPairWithValidityFunc asks tsnet for a certificate for domain valid for
// at least minValidity, renewing the stored one at once if it is not.
type certPairWithValidityFunc func(ctx context.Context, domain string, minValidity time.Duration) (certPEM, keyPEM []byte, err error)

// sharedSource is the original tsnetCertificateSource: it gets a node's
// certificate from the shared store, and from tsnet only under the issuance
// lease. store is nil, or not a certificateIssuer, for a store that does not
// share certificates.
type sharedSource struct {
	store                any
	fqdn                 string
	certPair             certPairFunc
	certPairWithValidity certPairWithValidityFunc
}

var _ certificateSource = (*sharedSource)(nil)

func (s *sharedSource) Stored(context.Context) (*tls.Certificate, error) {
	issuer, ok := s.store.(certificateIssuer)
	if !ok {
		return nil, nil
	}
	certPEM, keyPEM, err := issuer.ReadTLSCertAndKey(s.fqdn)
	if errors.Is(err, errStateNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read stored tailnet service certificate: %w", err)
	}
	return parseCertificatePair(certPEM, keyPEM)
}

// Obtain reuses a valid stored certificate without the lease. A store it
// cannot read fails the request rather than ordering a replacement.
func (s *sharedSource) Obtain(ctx context.Context, now time.Time) (*tls.Certificate, error) {
	stored, err := s.Stored(ctx)
	if err != nil {
		return nil, err
	}
	if stored != nil && now.Before(stored.Leaf.NotAfter) {
		return stored, nil
	}
	release, err := s.claim(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	certPEM, keyPEM, err := s.certPair(ctx, s.fqdn)
	if err != nil {
		return nil, fmt.Errorf("obtain tailnet service certificate: %w", err)
	}
	return parseCertificatePair(certPEM, keyPEM)
}

// Renew re-reads the store under the lease, since another slot may have just
// renewed, and otherwise asks tsnet for a certificate valid for longer than
// held has left.
func (s *sharedSource) Renew(ctx context.Context, held *tls.Certificate, now time.Time) (*tls.Certificate, error) {
	release, err := s.claim(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	stored, err := s.Stored(ctx)
	if err != nil {
		return nil, err
	}
	if replaces(stored, held) {
		return stored, nil
	}
	certPEM, keyPEM, err := s.certPairWithValidity(ctx, s.fqdn, held.Leaf.NotAfter.Sub(now)+time.Hour)
	if err != nil {
		return nil, fmt.Errorf("ask tsnet for a renewal: %w", err)
	}
	return parseCertificatePair(certPEM, keyPEM)
}

// claim takes the connection's issuance lease when the store shares
// certificates, failing with errCertificateIssuing while another slot holds
// it.
func (s *sharedSource) claim(ctx context.Context) (func(), error) {
	issuer, ok := s.store.(certificateIssuer)
	if !ok {
		return func() {}, nil
	}
	release, claimed, err := issuer.ClaimCertificateIssuance(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim tailnet service certificate lease: %w", err)
	}
	if !claimed {
		return nil, errCertificateIssuing
	}
	return release, nil
}

func parseCertificatePair(certPEM, keyPEM []byte) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse tailnet service certificate: %w", err)
	}
	return &pair, nil
}
