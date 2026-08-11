package gcpauth

import (
	"context"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// StubResolverPrincipal is the ambient principal NewStubResolver reports when no
// impersonation target is named. It is a service account in a project of its
// own, distinct from any target a test is likely to use, so callers that screen
// impersonation targets against Gram's own project behave as they would in
// production rather than accidentally matching.
const StubResolverPrincipal = "gram@gram-stub.iam.gserviceaccount.com"

// stubAccessValue is what StubResolver's token sources hand out. It is
// deliberately not a plausible credential: anything that reaches a real Google
// endpoint with it fails loudly rather than appearing to work.
const stubAccessValue = "stub-gcpauth-value-not-valid"

// StubResolver is a Resolver that answers from memory instead of reaching a
// cloud identity. Use it anywhere a real resolve would make behavior depend on
// ambient credentials — tests, and local development off GCP.
//
// The zero value is not usable; construct one with NewStubResolver.
type StubResolver struct {
	mu      sync.Mutex
	resolve func(ctx context.Context, cred Credential) (Principal, error)
}

var _ Resolver = (*StubResolver)(nil)

// NewStubResolver returns a resolver that succeeds for both modes it can answer
// honestly offline: an impersonation credential resolves to the target it names
// (mirroring the real resolver, where a successful impersonation reports the
// target as the effective principal), and an ambient one resolves to
// StubResolverPrincipal. Workload Identity Federation returns ErrUnsupportedMode,
// as the real resolver does.
//
// Call SetResolve to script a different outcome.
func NewStubResolver() *StubResolver {
	return &StubResolver{mu: sync.Mutex{}, resolve: defaultStubResolve}
}

// SetResolve replaces the resolve behavior, so a caller can script a failure, a
// principal with no email, or a specific project. Safe to call between probes.
func (s *StubResolver) SetResolve(fn func(ctx context.Context, cred Credential) (Principal, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolve = fn
}

// ResolvePrincipal implements Resolver.
func (s *StubResolver) ResolvePrincipal(ctx context.Context, cred Credential) (Principal, error) {
	s.mu.Lock()
	resolve := s.resolve
	s.mu.Unlock()

	return resolve(ctx, cred)
}

// TokenSource implements Resolver. It hands out a non-functional token for any
// mode the stub can resolve, and mirrors the real resolver in rejecting
// Workload Identity Federation.
//
// The token is deliberately unusable: a stub exists so callers can exercise the
// plumbing that obtains and passes a token source around, not so they can make
// real Google API calls. Anything that does try one fails at the endpoint rather
// than silently succeeding against an unintended identity. It defers to
// ResolvePrincipal so a scripted failure covers this path too, which keeps the
// two halves of the interface from disagreeing about whether a credential works.
func (s *StubResolver) TokenSource(ctx context.Context, cred Credential) (oauth2.TokenSource, error) {
	if _, err := s.ResolvePrincipal(ctx, cred); err != nil {
		return nil, err
	}

	return oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken:  stubAccessValue,
		TokenType:    "Bearer",
		RefreshToken: "",
		Expiry:       time.Time{},
		ExpiresIn:    0,
	}), nil
}

func defaultStubResolve(_ context.Context, cred Credential) (Principal, error) {
	switch cred.mode() {
	case modeImpersonation:
		return Principal{Email: cred.ImpersonateServiceAccount, Source: SourceImpersonation}, nil
	case modeAmbient:
		return Principal{Email: StubResolverPrincipal, Source: SourceMetadataServer}, nil
	case modeWIF:
		return Principal{}, ErrUnsupportedMode
	default:
		return Principal{}, ErrUnsupportedMode
	}
}
