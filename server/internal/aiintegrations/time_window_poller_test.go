package aiintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeWindowSource serves empty single-page windows and records every fetch's
// bounds so tests can assert exactly which ranges the poller requested.
type fakeWindowSource struct {
	// upperBound overrides the source finalization watermark; zero means the
	// data is immediately final (upper bound = endTime), like Cursor.
	upperBound time.Time
	fetches    []fetchBounds
}

type fetchBounds struct {
	start time.Time
	end   time.Time
}

func (s *fakeWindowSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	if s.upperBound.IsZero() {
		return endTime, nil
	}
	return s.upperBound, nil
}

func (s *fakeWindowSource) FetchPage(_ context.Context, start, end time.Time, _ string) (page[int], error) {
	s.fetches = append(s.fetches, fetchBounds{start: start, end: end})
	return page[int]{Payload: 0, NextPage: "", HasMore: false}, nil
}

func (s *fakeWindowSource) RetryAfter(error) (time.Duration, bool) {
	return 0, false
}

// fakeCheckpointStore records watermark advancements in order.
type fakeCheckpointStore struct {
	watermarks []time.Time
}

func (s *fakeCheckpointStore) AdvanceSchedulePollWatermark(_ context.Context, _ uuid.UUID, _ string, watermark time.Time) error {
	s.watermarks = append(s.watermarks, watermark)
	return nil
}

func newWindowTestPoller(store checkpointStore, initialLookback, maxWindow, granularity, resumeOffset time.Duration) *poller[int] {
	return &poller[int]{
		store:           store,
		schedule:        ScheduleCursor,
		heartbeat:       func(context.Context, int) {},
		processPage:     func(context.Context, int) error { return nil },
		initialLookback: initialLookback,
		maxWindow:       maxWindow,
		granularity:     granularity,
		resumeOffset:    resumeOffset,
	}
}

// A schedule that has never synced (zero watermark) must fetch from the
// lookback boundary itself: the resume offset would permanently skip an event
// timestamped exactly on it.
func TestPollerInitialLookbackFetchIncludesBoundary(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, time.Time{}, src, endTime))

	require.Equal(t, []fetchBounds{{start: endTime.Add(-24 * time.Hour), end: endTime}}, src.fetches)
	require.Equal(t, []time.Time{endTime}, store.watermarks)
}

// A resumed schedule's watermark is the inclusive end of already-ingested
// data, so the fetch starts one resume offset past it.
func TestPollerResumedFetchSkipsWatermark(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	watermark := endTime.Add(-time.Hour)
	src := &fakeWindowSource{}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, watermark, src, endTime))

	require.Equal(t, []fetchBounds{{start: watermark.Add(time.Millisecond), end: endTime}}, src.fetches)
	require.Equal(t, []time.Time{endTime}, store.watermarks)
}

// When an initial sync spans multiple windows, only the first window starts
// at the untouched lookback boundary; every later window resumes from the
// previous window's fetched end and gets the offset.
func TestPollerMultiWindowOffsetsOnlyResumedSeams(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 3*time.Hour, time.Hour, 0, time.Millisecond)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, time.Time{}, src, endTime))

	require.Equal(t, []fetchBounds{
		{start: endTime.Add(-3 * time.Hour), end: endTime.Add(-2 * time.Hour)},
		{start: endTime.Add(-2 * time.Hour).Add(time.Millisecond), end: endTime.Add(-time.Hour)},
		{start: endTime.Add(-time.Hour).Add(time.Millisecond), end: endTime},
	}, src.fetches)
	require.Equal(t, []time.Time{
		endTime.Add(-2 * time.Hour),
		endTime.Add(-time.Hour),
		endTime,
	}, store.watermarks)
}

// Exclusive-end bucketed sources set no resume offset: the watermark is
// re-used verbatim as the next window's inclusive start.
func TestPollerZeroResumeOffsetReusesWatermark(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 30, 0, time.UTC)
	watermark := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 24*time.Hour, 0, time.Minute, 0)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, watermark, src, endTime))

	require.Equal(t, []fetchBounds{{start: watermark, end: endTime.Truncate(time.Minute)}}, src.fetches)
	require.Equal(t, []time.Time{endTime.Truncate(time.Minute)}, store.watermarks)
}

// The provider finalization watermark caps the range: nothing at or beyond
// the source's upper bound is fetched, and the stored watermark stops there.
func TestPollerCapsRangeAtSourceUpperBound(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	upperBound := endTime.Add(-30 * time.Minute)
	watermark := endTime.Add(-time.Hour)
	src := &fakeWindowSource{upperBound: upperBound}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, watermark, src, endTime))

	require.Equal(t, []fetchBounds{{start: watermark.Add(time.Millisecond), end: upperBound}}, src.fetches)
	require.Equal(t, []time.Time{upperBound}, store.watermarks)
}

// An up-to-date schedule (watermark at the range end) fetches nothing and
// leaves the stored watermark untouched.
func TestPollerNoFetchWhenUpToDate(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{}
	store := &fakeCheckpointStore{}
	p := newWindowTestPoller(store, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.sync(t.Context(), Config{ID: uuid.New()}, endTime, src, endTime))

	require.Empty(t, src.fetches)
	require.Empty(t, store.watermarks)
}
