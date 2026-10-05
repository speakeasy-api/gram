package remotemcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCompareScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource []string
		issuer   []string
		want     scopeOutcome
	}{
		{name: "resource omits member", resource: nil, issuer: []string{"a"}, want: scopeOutcomeUnknown},
		{name: "resource omits member and issuer empty", resource: nil, issuer: nil, want: scopeOutcomeUnknown},
		{name: "both empty", resource: []string{}, issuer: []string{}, want: scopeOutcomeMatch},
		{name: "resource empty issuer not", resource: []string{}, issuer: []string{"a"}, want: scopeOutcomeResourceSubset},
		{name: "same set", resource: []string{"a", "b"}, issuer: []string{"b", "a"}, want: scopeOutcomeMatch},
		{name: "duplicates ignored", resource: []string{"a", "a"}, issuer: []string{"a"}, want: scopeOutcomeMatch},
		{name: "resource subset", resource: []string{"a"}, issuer: []string{"a", "b"}, want: scopeOutcomeResourceSubset},
		{name: "resource superset", resource: []string{"a", "b"}, issuer: []string{"a"}, want: scopeOutcomeResourceSuperset},
		{name: "resource superset of empty set", resource: []string{"a"}, issuer: []string{}, want: scopeOutcomeResourceSuperset},
		{name: "issuer omits member", resource: []string{"a"}, issuer: nil, want: scopeOutcomeUnknown},
		{name: "issuer omits member and resource empty", resource: []string{}, issuer: nil, want: scopeOutcomeUnknown},
		{name: "disjoint", resource: []string{"a"}, issuer: []string{"b"}, want: scopeOutcomeDisjoint},
		{name: "partial overlap", resource: []string{"a", "b"}, issuer: []string{"b", "c"}, want: scopeOutcomePartialOverlap},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, compareScopes(tc.resource, tc.issuer))
		})
	}
}

func TestChallengeScopesSweep(t *testing.T) {
	t.Parallel()

	now := time.Now()
	s := newChallengeScopesState()
	s.seen.Store("stale", &challengeObservation{scopes: []string{"a"}, at: now.Add(-challengeScopesRecheck)})
	s.seen.Store("fresh", &challengeObservation{scopes: []string{"a"}, at: now.Add(-challengeScopesRecheck + time.Second)})
	s.sweep(now)

	_, ok := s.seen.Load("stale")
	require.False(t, ok)
	_, ok = s.seen.Load("fresh")
	require.True(t, ok)
}

func TestParseChallengeScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers []string
		want    []string
	}{
		{name: "no header", headers: nil, want: nil},
		{name: "no scope param", headers: []string{`Bearer realm="mcp", resource_metadata="https://rs.example.test/.well-known/oauth-protected-resource"`}, want: nil},
		{name: "quoted scopes", headers: []string{`Bearer realm="mcp", scope="read write", error="insufficient_scope"`}, want: []string{"read", "write"}},
		{name: "single unquoted scope", headers: []string{`Bearer scope=read`}, want: []string{"read"}},
		{name: "empty scope", headers: []string{`Bearer scope=""`}, want: nil},
		{name: "extra whitespace", headers: []string{`Bearer scope="  read   write "`}, want: []string{"read", "write"}},
		{name: "case-insensitive name", headers: []string{`Bearer SCOPE="read"`}, want: []string{"read"}},
		{name: "scope inside another value is not a param", headers: []string{`Bearer realm="scope=\"x\"", scope="read"`}, want: []string{"read"}},
		{name: "second header carries it", headers: []string{`Basic realm="x"`, `Bearer scope="a b"`}, want: []string{"a", "b"}},
		{name: "first non-empty wins", headers: []string{`Bearer scope=""`, `Bearer scope="a"`}, want: []string{"a"}},
		{name: "unterminated quote", headers: []string{`Bearer scope="read`}, want: nil},
		{name: "basic scope ignored", headers: []string{`Basic realm="x", scope="admin"`}, want: nil},
		{name: "basic before bearer in one header", headers: []string{`Basic scope="admin", Bearer scope="read"`}, want: []string{"read"}},
		{name: "dpop accepted", headers: []string{`DPoP algs="ES256", scope="read"`}, want: []string{"read"}},
		{name: "no scheme", headers: []string{`scope="read"`}, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, parseChallengeScopes(tc.headers))
		})
	}
}

func TestRecordProtectedResourceRejectsMismatchedIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource string
		probed   string
	}{
		{name: "document adds trailing slash", resource: "https://rs.example.test/mcp/", probed: "https://rs.example.test/mcp"},
		{name: "document omits trailing slash", resource: "https://rs.example.test/mcp", probed: "https://rs.example.test/mcp/"},
		{name: "different resource", resource: "https://rs.example.test/other", probed: "https://rs.example.test/mcp"},
		{name: "missing resource", resource: "", probed: "https://rs.example.test/mcp"},
		{name: "both empty", resource: "", probed: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A nil database proves rejected documents never reach persistence.
			doc := wellknown.OAuthProtectedResourceMetadata{Resource: tc.resource}
			require.NoError(t, recordProtectedResource(context.Background(), nil, uuid.Nil, "", tc.probed, doc))
		})
	}
}

func TestProbeProtectedResourceOnUseSkipsPlainHTTP(t *testing.T) {
	t.Parallel()

	// A nil database and policy prove no probe is started.
	f := &ProxyManager{protectedResourceProbes: newProtectedResourceProbeState()}
	started := make(chan struct{}, 1)
	f.afterProtectedResourceProbe = func() { started <- struct{}{} }

	f.probeProtectedResourceOnUse(t.Context(), testenv.NewLogger(t), uuid.New(), "org", "http://rs.example.test/mcp")

	require.Empty(t, f.protectedResourceProbes.slots)
	f.protectedResourceProbes.checked.Range(func(key, _ any) bool {
		require.Fail(t, "unexpected debounce entry", "%v", key)
		return true
	})
	require.Empty(t, started)
}
