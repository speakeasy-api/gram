package gcpauth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

// countingResolver records how many times the ambient principal was resolved, so
// tests can assert the memo actually memoizes.
type countingResolver struct {
	*gcpauth.StubResolver

	ambient atomic.Int64
}

func newCountingResolver() *countingResolver {
	r := &countingResolver{StubResolver: gcpauth.NewStubResolver(), ambient: atomic.Int64{}}
	r.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		if cred.ImpersonateServiceAccount == "" {
			r.ambient.Add(1)
			return gcpauth.Principal{Email: gcpauth.StubResolverPrincipal, Source: gcpauth.SourceMetadataServer}, nil
		}
		return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
	})
	return r
}

func TestIdentity_GramPrincipalMemoizes(t *testing.T) {
	t.Parallel()

	resolver := newCountingResolver()
	identity := gcpauth.NewIdentity(resolver)

	for range 5 {
		got, err := identity.GramPrincipal(t.Context())
		require.NoError(t, err)
		require.Equal(t, gcpauth.StubResolverPrincipal, got)
	}

	require.Equal(t, int64(1), resolver.ambient.Load(), "gram's own identity is fixed for the process, so it must resolve once")
}

// A failure must not be cached: an environment that has not been granted
// credentials yet has to start working once it is, without a restart.
func TestIdentity_GramPrincipalDoesNotCacheFailure(t *testing.T) {
	t.Parallel()

	resolver := gcpauth.NewStubResolver()
	var fail atomic.Bool
	fail.Store(true)
	resolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		if fail.Load() {
			return gcpauth.Principal{Email: "", Source: ""}, errors.New("no credentials configured")
		}
		return gcpauth.Principal{Email: gcpauth.StubResolverPrincipal, Source: gcpauth.SourceMetadataServer}, nil
	})
	identity := gcpauth.NewIdentity(resolver)

	_, err := identity.GramPrincipal(t.Context())
	require.Error(t, err)

	fail.Store(false)
	got, err := identity.GramPrincipal(t.Context())
	require.NoError(t, err)
	require.Equal(t, gcpauth.StubResolverPrincipal, got)
}

// Concurrent callers racing to resolve is explicitly allowed; they must all get
// the same answer and none may observe a torn read.
func TestIdentity_GramPrincipalConcurrent(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(newCountingResolver())

	var wg sync.WaitGroup
	results := make([]string, 16)
	for i := range results {
		wg.Go(func() {
			got, err := identity.GramPrincipal(t.Context())
			if err == nil {
				results[i] = got
			}
		})
	}
	wg.Wait()

	for i, got := range results {
		require.Equal(t, gcpauth.StubResolverPrincipal, got, "goroutine %d", i)
	}
}

func TestIdentity_ImpersonationTargetProblemAccepts(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(newCountingResolver())

	kind, reason, err := identity.ImpersonationTargetProblem(t.Context(), testenv.NewLogger(t), "signer@customer-project.iam.gserviceaccount.com")
	require.NoError(t, err)
	require.Equal(t, gcpauth.TargetOK, kind, "a user-managed service account in someone else's project is acceptable")
	require.Empty(t, reason)
}

// The screening exists because Gram publishes its own service account, so a
// target inside Gram's own project must be refused rather than probed.
func TestIdentity_ImpersonationTargetProblemRefusesGramProject(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(newCountingResolver())

	kind, reason, err := identity.ImpersonationTargetProblem(t.Context(), testenv.NewLogger(t), "internal@gram-stub.iam.gserviceaccount.com")
	require.NoError(t, err)
	require.Equal(t, gcpauth.TargetOwnProject, kind)
	require.Contains(t, reason, "your own GCP project")
}

// Anything that cannot be placed in a project is refused rather than compared
// unreliably: default compute and App Engine accounts name their project by
// number or by an id that cannot be compared against one.
func TestIdentity_ImpersonationTargetProblemRefusesUnplaceable(t *testing.T) {
	t.Parallel()

	identity := gcpauth.NewIdentity(newCountingResolver())

	for _, target := range []string{
		"123456789012-compute@developer.gserviceaccount.com",
		"my-project@appspot.gserviceaccount.com",
		"person@example.com",
		"not-an-email",
		"",
		// Google-managed service agents live under *.iam.gserviceaccount.com too,
		// but their domain names a Google namespace rather than a project. Left
		// unrefused, one belonging to Gram's own project would never match Gram's
		// project id and would slip past the same-project comparison below.
		"service-123456789012@gcp-sa-cloudkms.iam.gserviceaccount.com",
		"service-123456789012@compute-system.iam.gserviceaccount.com",
		"123456789012@cloudservices.iam.gserviceaccount.com",
	} {
		kind, reason, err := identity.ImpersonationTargetProblem(t.Context(), testenv.NewLogger(t), target)
		require.NoError(t, err, "%q", target)
		require.Equal(t, gcpauth.TargetMalformed, kind, "%q must be refused", target)
		require.Contains(t, reason, "user-managed service account", "%q must be refused", target)
	}
}

// A screening the server cannot evaluate is an error, never an acceptance. The
// kind returned alongside it carries no meaning, so nothing here asserts one:
// the error is the whole of what a caller may act on.
func TestIdentity_ImpersonationTargetProblemErrorsWhenUnevaluatable(t *testing.T) {
	t.Parallel()

	resolver := gcpauth.NewStubResolver()
	resolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("metadata server unreachable")
	})
	identity := gcpauth.NewIdentity(resolver)

	_, reason, err := identity.ImpersonationTargetProblem(t.Context(), testenv.NewLogger(t), "signer@customer-project.iam.gserviceaccount.com")
	require.Error(t, err)
	require.Empty(t, reason)
}

// Gram running as something that cannot be placed in a project fails closed:
// comparing against nothing would silently accept every target.
func TestIdentity_ImpersonationTargetProblemErrorsWhenGramIsUnplaceable(t *testing.T) {
	t.Parallel()

	resolver := gcpauth.NewStubResolver()
	resolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "123456789012-compute@developer.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	})
	identity := gcpauth.NewIdentity(resolver)

	_, reason, err := identity.ImpersonationTargetProblem(t.Context(), testenv.NewLogger(t), "signer@customer-project.iam.gserviceaccount.com")
	require.Error(t, err)
	require.Empty(t, reason)
}

// ResolvePrincipal must stay a live probe. Serving it from the GramPrincipal
// memo would report success for an impersonation right that has been revoked.
func TestIdentity_ResolvePrincipalIsNotMemoized(t *testing.T) {
	t.Parallel()

	resolver := newCountingResolver()
	identity := gcpauth.NewIdentity(resolver)

	_, err := identity.GramPrincipal(t.Context())
	require.NoError(t, err)

	var calls atomic.Int64
	resolver.SetResolve(func(_ context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
		calls.Add(1)
		return gcpauth.Principal{Email: cred.ImpersonateServiceAccount, Source: gcpauth.SourceImpersonation}, nil
	})

	for range 3 {
		_, err := identity.ResolvePrincipal(t.Context(), gcpauth.Credential{
			ImpersonateServiceAccount: "signer@customer-project.iam.gserviceaccount.com",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		})
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), calls.Load())
}

// Wrapping must preserve the sentinel: callers branch on ErrUnsupportedMode to
// tell "not supported" apart from "did not work".
func TestIdentity_ResolvePrincipalPreservesSentinel(t *testing.T) {
	t.Parallel()

	resolver := gcpauth.NewStubResolver()
	resolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: ""}, gcpauth.ErrUnsupportedMode
	})
	identity := gcpauth.NewIdentity(resolver)

	_, err := identity.ResolvePrincipal(t.Context(), gcpauth.Credential{
		ImpersonateServiceAccount: "",
		WifPoolID:                 "pool",
		WifProviderID:             "provider",
		WifProjectNumber:          "123",
	})
	require.ErrorIs(t, err, gcpauth.ErrUnsupportedMode)
}

var _ gcpauth.Resolver = (*gcpauth.Identity)(nil)
