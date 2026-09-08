package activities

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// fakeIssuerMetadataRefresher serves canned due pages and lets a test observe or interrupt each visit.
type fakeIssuerMetadataRefresher struct {
	pages     [][]remotesessions.IssuerMetadataRefreshCandidate
	cursors   []*remotesessions.IssuerMetadataRefreshCursor
	listCalls int
	visited   []uuid.UUID
	onVisit   func()
}

func (f *fakeIssuerMetadataRefresher) ListDue(_ context.Context, _ time.Time, after *remotesessions.IssuerMetadataRefreshCursor, _ int32) ([]remotesessions.IssuerMetadataRefreshCandidate, *remotesessions.IssuerMetadataRefreshCursor, error) {
	f.cursors = append(f.cursors, after)
	i := f.listCalls
	f.listCalls++
	if i >= len(f.pages) {
		return nil, nil, nil
	}
	page := f.pages[i]
	if len(page) == 0 {
		return page, nil, nil
	}
	return page, &remotesessions.IssuerMetadataRefreshCursor{VisitedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.NegativeInfinity, Valid: true}, ID: page[len(page)-1].ID}, nil
}

func (f *fakeIssuerMetadataRefresher) ListReprojectable(context.Context, time.Time, int32) ([]remotesessions.IssuerMetadataRefreshCandidate, error) {
	return nil, nil
}

func (f *fakeIssuerMetadataRefresher) RecordSkipped(context.Context, remotesessions.IssuerMetadataRefreshCandidate, remotesessionmetrics.IssuerMetadataRefreshOutcome) {
}

func (f *fakeIssuerMetadataRefresher) Reproject(_ context.Context, candidate remotesessions.IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	f.visited = append(f.visited, candidate.ID)
	if f.onVisit != nil {
		f.onVisit()
	}
	return remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected, nil
}

func (f *fakeIssuerMetadataRefresher) Refresh(_ context.Context, candidate remotesessions.IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	f.visited = append(f.visited, candidate.ID)
	if f.onVisit != nil {
		f.onVisit()
	}
	return remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, nil
}

func candidateIDs(candidates []RemoteSessionIssuerMetadataRefreshCandidate) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func newFilterActivity(t *testing.T, routes route.Store) *RemoteSessionIssuerMetadataRefresh {
	t.Helper()
	refresher := remotesessions.NewIssuerMetadataRefresher(testenv.NewLogger(t), testenv.NewMeterProvider(t), nil, nil, nil)
	return NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), refresher, routes)
}

func filterCandidate(issuerURL string, tunnel uuid.NullUUID) remotesessions.IssuerMetadataRefreshCandidate {
	return remotesessions.IssuerMetadataRefreshCandidate{
		ID:                  uuid.New(),
		IssuerURL:           issuerURL,
		Host:                remotesessions.IssuerMetadataRefreshHost(issuerURL),
		ProjectID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:      pgtype.Text{String: "", Valid: false},
		TunneledMcpServerID: tunnel,
	}
}

func TestRemoteSessionIssuerMetadataRefresh_FilterFetchable_SkipsTunnelDown(t *testing.T) {
	t.Parallel()

	routes := route.NewRouteTable()
	liveTunnel := uuid.New()
	deadTunnel := uuid.New()
	require.NoError(t, routes.Publish(t.Context(), liveTunnel.String(), "gateway-1:9000", time.Minute))

	direct := filterCandidate("https://direct.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	live := filterCandidate("https://live.internal", uuid.NullUUID{UUID: liveTunnel, Valid: true})
	dead := filterCandidate("https://dead.internal", uuid.NullUUID{UUID: deadTunnel, Valid: true})

	kept := newFilterActivity(t, routes).filterFetchable(t.Context(), []remotesessions.IssuerMetadataRefreshCandidate{direct, live, dead})
	require.Equal(t, []remotesessions.IssuerMetadataRefreshCandidate{direct, live}, kept)
}

func TestRemoteSessionIssuerMetadataRefresh_FilterFetchable_UnknownLivenessKeepsAll(t *testing.T) {
	t.Parallel()

	tunneled := filterCandidate("https://tunneled.internal", uuid.NullUUID{UUID: uuid.New(), Valid: true})

	kept := newFilterActivity(t, nil).filterFetchable(t.Context(), []remotesessions.IssuerMetadataRefreshCandidate{tunneled})
	require.Equal(t, []remotesessions.IssuerMetadataRefreshCandidate{tunneled}, kept)
}

func TestRemoteSessionIssuerMetadataRefresh_FilterFetchable_DropsUnparseableIssuer(t *testing.T) {
	t.Parallel()

	routable := filterCandidate("https://IdP.Example.com:443/tenant", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	unparseable := filterCandidate("not a url", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.Empty(t, unparseable.Host)

	kept := newFilterActivity(t, nil).filterFetchable(t.Context(), []remotesessions.IssuerMetadataRefreshCandidate{unparseable, routable})
	require.Equal(t, []remotesessions.IssuerMetadataRefreshCandidate{routable}, kept)
	require.Equal(t, "idp.example.com", toRefreshCandidates(kept)[0].Host)
}

func TestRemoteSessionIssuerMetadataRefreshCandidate_RoundTripsScope(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	scoped := remotesessions.IssuerMetadataRefreshCandidate{
		ID:                  uuid.New(),
		IssuerURL:           "https://idp.example.com",
		Host:                "idp.example.com",
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:      pgtype.Text{String: "org_123", Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	global := filterCandidate("https://global.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	converted := toRefreshCandidates([]remotesessions.IssuerMetadataRefreshCandidate{scoped, global})
	require.Equal(t, scoped, toRefresherCandidate(converted[0]))
	require.Equal(t, global, toRefresherCandidate(converted[1]))
}

func TestJitteredHostSpacing_StaysWithinSpread(t *testing.T) {
	t.Parallel()

	for range 200 {
		d := jitteredHostSpacing()
		require.GreaterOrEqual(t, d, remoteSessionIssuerMetadataRefreshSpacing-remoteSessionIssuerMetadataRefreshJitter)
		require.Less(t, d, remoteSessionIssuerMetadataRefreshSpacing+remoteSessionIssuerMetadataRefreshJitter)
	}
}

func TestRemoteSessionIssuerMetadataRefresh_ListRefreshCandidates_PagesPastTunnelDown(t *testing.T) {
	t.Parallel()

	routes := route.NewRouteTable()
	deadA := filterCandidate("https://dead-a.internal", uuid.NullUUID{UUID: uuid.New(), Valid: true})
	deadB := filterCandidate("https://dead-b.internal", uuid.NullUUID{UUID: uuid.New(), Valid: true})
	healthyA := filterCandidate("https://healthy-a.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	healthyB := filterCandidate("https://healthy-b.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	fake := &fakeIssuerMetadataRefresher{pages: [][]remotesessions.IssuerMetadataRefreshCandidate{{deadA, deadB}, {healthyA, healthyB}}}

	got, err := NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), fake, routes).ListRefreshCandidates(t.Context(), ListRemoteSessionIssuerMetadataRefreshCandidatesInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{healthyA.ID, healthyB.ID}, candidateIDs(got), "a first page of tunnel-down rows does not hide the healthy rows behind it")
	require.Equal(t, 2, fake.listCalls)
	require.Nil(t, fake.cursors[0])
	require.Equal(t, deadB.ID, fake.cursors[1].ID, "the second page starts after the first page's last row")
}

func TestRemoteSessionIssuerMetadataRefresh_ListRefreshCandidates_StopsAtTheLimitAndPageCap(t *testing.T) {
	t.Parallel()

	routes := route.NewRouteTable()
	pages := make([][]remotesessions.IssuerMetadataRefreshCandidate, 0, remoteSessionIssuerMetadataRefreshMaxPages+2)
	for range remoteSessionIssuerMetadataRefreshMaxPages + 2 {
		pages = append(pages, []remotesessions.IssuerMetadataRefreshCandidate{filterCandidate("https://dead.internal", uuid.NullUUID{UUID: uuid.New(), Valid: true})})
	}
	fake := &fakeIssuerMetadataRefresher{pages: pages}
	got, err := NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), fake, routes).ListRefreshCandidates(t.Context(), ListRemoteSessionIssuerMetadataRefreshCandidatesInput{Limit: 1})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, remoteSessionIssuerMetadataRefreshMaxPages, fake.listCalls, "an endless run of unfetchable rows is bounded by the page cap")

	healthy := filterCandidate("https://healthy.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	extra := filterCandidate("https://extra.example.com", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	fake = &fakeIssuerMetadataRefresher{pages: [][]remotesessions.IssuerMetadataRefreshCandidate{{healthy, extra}, {extra}}}
	got, err = NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), fake, routes).ListRefreshCandidates(t.Context(), ListRemoteSessionIssuerMetadataRefreshCandidatesInput{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{healthy.ID}, candidateIDs(got), "never more than the requested number")
	require.Equal(t, 1, fake.listCalls, "a full first page needs no second read")
}

func TestRemoteSessionIssuerMetadataRefresh_RefreshHost_CancelledContextIsAnError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake := &fakeIssuerMetadataRefresher{onVisit: cancel}
	first := RemoteSessionIssuerMetadataRefreshCandidate{ID: uuid.New(), IssuerURL: "https://idp.example.com/a", Host: "idp.example.com", ProjectID: uuid.Nil, OrganizationID: ""}
	second := RemoteSessionIssuerMetadataRefreshCandidate{ID: uuid.New(), IssuerURL: "https://idp.example.com/b", Host: "idp.example.com", ProjectID: uuid.Nil, OrganizationID: ""}

	result, err := NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), fake, nil).RefreshHost(ctx, RefreshRemoteSessionIssuerMetadataHostInput{Host: "idp.example.com", Issuers: []RemoteSessionIssuerMetadataRefreshCandidate{first, second}})
	require.ErrorIs(t, err, context.Canceled, "a batch cut short is not a success")
	require.Equal(t, []uuid.UUID{first.ID}, fake.visited)
	require.Equal(t, 1, result.Outcomes[string(remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed)])
}

func TestRemoteSessionIssuerMetadataRefresh_Reproject_CancelledContextIsAnError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake := &fakeIssuerMetadataRefresher{onVisit: cancel}
	only := RemoteSessionIssuerMetadataRefreshCandidate{ID: uuid.New(), IssuerURL: "https://idp.example.com", Host: "idp.example.com", ProjectID: uuid.Nil, OrganizationID: ""}

	result, err := NewRemoteSessionIssuerMetadataRefresh(testenv.NewLogger(t), fake, nil).Reproject(ctx, ReprojectRemoteSessionIssuerMetadataInput{Issuers: []RemoteSessionIssuerMetadataRefreshCandidate{only}})
	require.ErrorIs(t, err, context.Canceled, "cancellation during the last issuer still fails the batch")
	require.Equal(t, []uuid.UUID{only.ID}, fake.visited)
	require.Equal(t, 1, result.Outcomes[string(remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected)])
}
