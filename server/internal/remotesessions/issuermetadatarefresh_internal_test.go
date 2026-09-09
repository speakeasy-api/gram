package remotesessions

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
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
		{name: "reproject and fetch a stale row", use: IssuerMetadataUse{MetadataFetchedAt: at(-30 * time.Hour), MetadataLastErrorAt: none, MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: true, fetch: true},
		{name: "reproject survives a transient failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-time.Minute), MetadataLastErrorUrl: retry, NeedsReprojection: true}, reproject: true, fetch: false},
		{name: "no reproject after a definitive failure", use: IssuerMetadataUse{MetadataFetchedAt: at(-time.Hour), MetadataLastErrorAt: at(-time.Minute), MetadataLastErrorUrl: noURL, NeedsReprojection: true}, reproject: false, fetch: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := planIssuerMetadataRefresh(tc.use, now)
			require.Equal(t, tc.reproject, plan.reproject, "reproject")
			require.Equal(t, tc.fetch, plan.fetch, "fetch")
		})
	}
}
