package protectedresource

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProbeOnUseSkipsPlainHTTP(t *testing.T) {
	t.Parallel()

	// A nil database and policy prove no probe is started.
	p := NewProber(nil, nil)
	started := make(chan struct{}, 1)
	p.beforeDetached = func() { started <- struct{}{} }

	p.ProbeOnUse(t.Context(), testenv.NewLogger(t), uuid.New(), "org", "http://rs.example.test/mcp")

	require.Empty(t, p.slots)
	p.checked.Range(func(key, _ any) bool {
		require.Fail(t, "unexpected debounce entry", "%v", key)
		return true
	})
	require.Empty(t, started)
}

func TestResolveForLogin_NilProberIsNotApplicable(t *testing.T) {
	t.Parallel()

	var p *Prober
	got := p.ResolveForLogin(t.Context(), testenv.NewLogger(t), uuid.New(), "org", "https://rs.example.test/mcp")
	require.Equal(t, LoginResolution{Row: nil, ScopesSupported: nil, Live: false, Outcome: ProbeOutcomeNotApplicable, ProbeDuration: 0}, got)
}

func stamp(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true}
}

func TestLoginSkip(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name string
		row  *repo.RemoteProtectedResource
		want ProbeOutcome
		skip bool
	}{
		{name: "no row", row: nil, want: "", skip: false},
		{name: "fetched within the hour", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-30 * time.Minute))}, want: ProbeOutcomeSkippedFresh, skip: true},
		{name: "fetched over an hour ago", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-2 * time.Hour))}, want: "", skip: false},
		{name: "failed within fifteen minutes", row: &repo.RemoteProtectedResource{MetadataLastErrorAt: stamp(now.Add(-5 * time.Minute))}, want: ProbeOutcomeSkippedRecentError, skip: true},
		{name: "failed over fifteen minutes ago", row: &repo.RemoteProtectedResource{MetadataLastErrorAt: stamp(now.Add(-20 * time.Minute))}, want: "", skip: false},
		{name: "fresh read outranks a later error", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-30 * time.Minute)), MetadataLastErrorAt: stamp(now.Add(-time.Minute))}, want: ProbeOutcomeSkippedFresh, skip: true},
		{name: "never read", row: &repo.RemoteProtectedResource{}, want: "", skip: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, skip := loginSkip(tc.row, now)
			require.Equal(t, tc.skip, skip)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRefreshDue(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name string
		row  *repo.RemoteProtectedResource
		want bool
	}{
		{name: "never visited", row: &repo.RemoteProtectedResource{}, want: true},
		{name: "fetched within a day", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-23 * time.Hour))}, want: false},
		{name: "fetched over a day ago", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-25 * time.Hour))}, want: true},
		{name: "error only, within the backoff", row: &repo.RemoteProtectedResource{MetadataLastErrorAt: stamp(now.Add(-5 * time.Minute))}, want: false},
		{name: "error only, past the backoff", row: &repo.RemoteProtectedResource{MetadataLastErrorAt: stamp(now.Add(-20 * time.Minute))}, want: true},
		{name: "error newer than the fetch, past the backoff", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-2 * time.Hour)), MetadataLastErrorAt: stamp(now.Add(-20 * time.Minute))}, want: true},
		{name: "error newer than the fetch, within the backoff", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-2 * time.Hour)), MetadataLastErrorAt: stamp(now.Add(-time.Minute))}, want: false},
		{name: "fetch newer than an old error stands for a day", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now.Add(-2 * time.Hour)), MetadataLastErrorAt: stamp(now.Add(-3 * time.Hour))}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, refreshDue(tc.row, now))
		})
	}
}

func TestLastGoodScopes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name string
		row  *repo.RemoteProtectedResource
		want []string
	}{
		{name: "no row", row: nil, want: nil},
		{name: "never fetched", row: &repo.RemoteProtectedResource{ScopesSupported: []string{"read"}}, want: nil},
		{name: "fetched without a list", row: &repo.RemoteProtectedResource{MetadataFetchedAt: stamp(now)}, want: nil},
		{name: "recent list", row: &repo.RemoteProtectedResource{ScopesSupported: []string{"read"}, MetadataFetchedAt: stamp(now.Add(-6 * 24 * time.Hour))}, want: []string{"read"}},
		{name: "recent empty list is a list", row: &repo.RemoteProtectedResource{ScopesSupported: []string{}, MetadataFetchedAt: stamp(now)}, want: []string{}},
		{name: "list older than a week", row: &repo.RemoteProtectedResource{ScopesSupported: []string{"read"}, MetadataFetchedAt: stamp(now.Add(-8 * 24 * time.Hour))}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, LastGoodScopes(tc.row, now))
		})
	}
}
