package remotesessions

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

func TestPlanIssuerMetadataRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) pgtype.Timestamptz { return conv.ToPGTimestamptz(now.Add(d)) }
	none := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	retry := conv.ToPGText("https://idp.example.com/.well-known/openid-configuration")
	noURL := pgtype.Text{String: "", Valid: false}

	cases := []struct {
		name      string
		use       IssuerMetadataUse
		reproject bool
		fetch     bool
	}{
		{name: "never visited", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, reproject: false, fetch: true},
		{name: "fresh fetch", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, reproject: false, fetch: false},
		{name: "stale fetch", use: IssuerMetadataUse{MetadataFetchedAt: at(-25 * time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, reproject: false, fetch: true},
		{name: "recent transient failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-30 * time.Hour), MetadataLastErrorAt: at(-30 * time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: false}, reproject: false, fetch: false},
		{name: "transient failure past the retry window", use: IssuerMetadataUse{MetadataFetchedAt: at(-30 * time.Hour), MetadataLastErrorAt: at(-61 * time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: false}, reproject: false, fetch: true},
		{name: "definitive failure counts as a visit", use: IssuerMetadataUse{MetadataFetchedAt: at(-30 * time.Hour), MetadataLastErrorAt: at(-2 * time.Hour), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, reproject: false, fetch: false},
		{name: "definitive failure past the daily cutoff", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: at(-25 * time.Hour), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, reproject: false, fetch: true},
		{name: "reproject a fresh row with NULL capability columns", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: true, fetch: false},
		{name: "a due fetch skips reprojection", use: IssuerMetadataUse{MetadataFetchedAt: at(-30 * time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: false, fetch: true},
		{name: "a never-fetched row fetches rather than reprojects", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: false, fetch: true},
		{name: "partial read an hour ago keeps the daily cadence", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-time.Hour), MetadataLastErrorUrl: retry, NeedsReprojection: false}, reproject: false, fetch: false},
		{name: "partial read a day ago is due", use: IssuerMetadataUse{MetadataFetchedAt: at(-25 * time.Hour), MetadataLastErrorAt: at(-25 * time.Hour), MetadataLastErrorUrl: retry, NeedsReprojection: false}, reproject: false, fetch: true},
		{name: "retry URL older than the last fetch is not a standing failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-2 * time.Hour), MetadataLastErrorUrl: retry, NeedsReprojection: false}, reproject: false, fetch: false},
		{name: "reproject survives a transient failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: true}, reproject: true, fetch: false},
		{name: "no reproject after a definitive failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: false, fetch: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := planIssuerMetadataRefresh(tc.use, now)
			require.Equal(t, tc.reproject, plan.reproject, "reproject")
			require.Equal(t, tc.fetch, plan.fetch, "fetch")
			require.Empty(t, plan.skipped, "the on-use plan is silent when nothing is due")
		})
	}
}

func TestPlanReactiveIssuerMetadataRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) pgtype.Timestamptz { return conv.ToPGTimestamptz(now.Add(d)) }
	none := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	retry := conv.ToPGText("https://idp.example.com/.well-known/openid-configuration")
	noURL := pgtype.Text{String: "", Valid: false}

	cases := []struct {
		name  string
		use   IssuerMetadataUse
		fetch bool
	}{
		{name: "never visited", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: true},
		{name: "fetched inside the daily cadence", use: IssuerMetadataUse{MetadataFetchedAt: at(-2 * time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: true},
		{name: "fetched inside the reactive interval", use: IssuerMetadataUse{MetadataFetchedAt: at(-5 * time.Minute), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: false},
		{name: "fetched exactly at the interval", use: IssuerMetadataUse{MetadataFetchedAt: at(-issuerMetadataReactiveInterval), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: true},
		{name: "transient failure inside the reactive interval", use: IssuerMetadataUse{MetadataFetchedAt: at(-2 * time.Hour), MetadataLastErrorAt: at(-time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: false}, fetch: false},
		{name: "definitive failure inside the reactive interval", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: at(-9 * time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: false},
		{name: "standing definitive failure past the reactive interval waits for the retry window", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: at(-11 * time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: false},
		{name: "standing definitive failure inside the retry window", use: IssuerMetadataUse{MetadataFetchedAt: at(-2 * time.Hour), MetadataLastErrorAt: at(-59 * time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: false},
		{name: "standing definitive failure past the retry window", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: at(-issuerMetadataRetryAfter), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: true},
		{name: "definitive failure superseded by a later fetch", use: IssuerMetadataUse{MetadataFetchedAt: at(-11 * time.Minute), MetadataLastErrorAt: at(-30 * time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: false}, fetch: true},
		{name: "transient failure past the reactive interval", use: IssuerMetadataUse{MetadataFetchedAt: none, MetadataLastErrorAt: at(-11 * time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: false}, fetch: true},
		{name: "reprojection is never the answer", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: true}, fetch: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := planReactiveIssuerMetadataRefresh(tc.use, now)
			require.Equal(t, tc.fetch, plan.fetch, "fetch")
			require.False(t, plan.reproject, "reproject")
			if tc.fetch {
				require.Empty(t, plan.skipped)
			} else {
				require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedRecent, plan.skipped)
			}
		})
	}
}

func TestNewTokenEndpointError_KeepsTheStatusText(t *testing.T) {
	t.Parallel()

	require.EqualError(t, newTokenEndpointError(404, "404 Not Found", []byte(`{"error":"invalid_grant","error_description":"revoked"}`)), "token endpoint 404 Not Found: invalid_grant: revoked")
	require.EqualError(t, newTokenEndpointError(410, "410 Gone", []byte("moved")), "token endpoint 410 Gone: moved")
}

func TestTokenEndpointMissing(t *testing.T) {
	t.Parallel()

	require.True(t, tokenEndpointMissing(404, false))
	require.True(t, tokenEndpointMissing(410, false))
	require.False(t, tokenEndpointMissing(404, true), "an OAuth error body is the endpoint answering, not drift")
	require.False(t, tokenEndpointMissing(400, false))
	require.False(t, tokenEndpointMissing(200, true), "a 2xx body error never triggers")
}
