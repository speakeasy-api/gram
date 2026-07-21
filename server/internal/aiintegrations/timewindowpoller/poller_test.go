package timewindowpoller

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const testSchedule = "cursor"

// fakeWindowSource serves empty single-page windows and records every fetch's
// bounds so tests can assert exactly which ranges the poller requested.
type fakeWindowSource struct {
	// upperBound overrides the source finalization watermark; zero means the
	// data is immediately final (upper bound = endTime), like Cursor.
	upperBound time.Time
	fetches    []fetchBounds
}

type fetchBounds struct {
	start     time.Time
	end       time.Time
	pageToken string
}

func (s *fakeWindowSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	if s.upperBound.IsZero() {
		return endTime, nil
	}
	return s.upperBound, nil
}

func (s *fakeWindowSource) FetchPage(_ context.Context, start, end time.Time, pageToken string) (Page[int], error) {
	s.fetches = append(s.fetches, fetchBounds{start: start, end: end, pageToken: pageToken})
	return Page[int]{Payload: 0, NextPage: "", HasMore: false}, nil
}

func (s *fakeWindowSource) ProcessPage(context.Context, int) error {
	return nil
}

func (s *fakeWindowSource) RetryAfter(error) (time.Duration, bool) {
	return 0, false
}

type fakePagedWindowSource struct {
	pages   []Page[int]
	fetches []fetchBounds
}

func (s *fakePagedWindowSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	return endTime, nil
}

func (s *fakePagedWindowSource) FetchPage(_ context.Context, start, end time.Time, pageToken string) (Page[int], error) {
	s.fetches = append(s.fetches, fetchBounds{start: start, end: end, pageToken: pageToken})
	return s.pages[len(s.fetches)-1], nil
}

func (s *fakePagedWindowSource) ProcessPage(context.Context, int) error {
	return nil
}

func (s *fakePagedWindowSource) RetryAfter(error) (time.Duration, bool) {
	return 0, false
}

// fakeCheckpointStore records watermark advancements in order.
type fakeCheckpointStore struct {
	checkpoints []PollCheckpoint
}

func (s *fakeCheckpointStore) AdvanceWatermark(_ context.Context, _ uuid.UUID, checkpoint PollCheckpoint) error {
	s.checkpoints = append(s.checkpoints, checkpoint)
	return nil
}

func (s *fakeCheckpointStore) watermarks() []time.Time {
	watermarks := make([]time.Time, 0, len(s.checkpoints))
	for _, checkpoint := range s.checkpoints {
		watermarks = append(watermarks, checkpoint.Watermark)
	}
	return watermarks
}

func newWindowTestPoller(store Store, state SyncState, src Source[int], endTime time.Time, initialLookback, maxWindow, granularity, resumeOffset time.Duration) *Poller[int] {
	return &Poller[int]{
		Store:           store,
		Schedule:        testSchedule,
		State:           state,
		Source:          src,
		EndTime:         endTime,
		Heartbeat:       func(context.Context, int) {},
		InitialLookback: initialLookback,
		MaxWindow:       maxWindow,
		Granularity:     granularity,
		ResumeOffset:    resumeOffset,
	}
}

func newSyncState(syncID uuid.UUID, watermark time.Time, checkpoint PollCheckpoint) SyncState {
	return SyncState{
		SyncID:      syncID,
		WatermarkAt: watermark,
		Checkpoint:  checkpoint,
	}
}

// A schedule that has never synced (zero watermark) must fetch from the
// lookback boundary itself: the resume offset would permanently skip an event
// timestamped exactly on it.
func TestPollerInitialLookbackFetchIncludesBoundary(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{upperBound: time.Time{}, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), time.Time{}, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{{start: endTime.Add(-24 * time.Hour), end: endTime, pageToken: ""}}, src.fetches)
	require.Equal(t, []time.Time{endTime}, store.watermarks())
}

// A resumed schedule's watermark is the inclusive end of already-ingested
// data, so the fetch starts one resume offset past it.
func TestPollerResumedFetchSkipsWatermark(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	watermark := endTime.Add(-time.Hour)
	src := &fakeWindowSource{upperBound: time.Time{}, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), watermark, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{{start: watermark.Add(time.Millisecond), end: endTime, pageToken: ""}}, src.fetches)
	require.Equal(t, []time.Time{endTime}, store.watermarks())
}

func TestPollerPersistsCheckpointAfterEachPage(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	windowStart := endTime.Add(-24 * time.Hour)
	src := &fakePagedWindowSource{
		pages: []Page[int]{
			{Payload: 1, NextPage: "2", HasMore: true},
			{Payload: 2, NextPage: "", HasMore: false},
		},
		fetches: nil,
	}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), time.Time{}, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{
		{start: windowStart, end: endTime, pageToken: ""},
		{start: windowStart, end: endTime, pageToken: "2"},
	}, src.fetches)
	require.Equal(t, []PollCheckpoint{
		PartialCheckpoint(windowStart, windowStart, endTime, "2"),
		CompletedCheckpoint(endTime),
	}, store.checkpoints)
}

func TestPollerResumesPartialCheckpointWithStoredWindow(t *testing.T) {
	t.Parallel()

	windowEnd := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	watermark := windowEnd.Add(-time.Hour)
	windowStart := watermark.Add(time.Millisecond)
	src := &fakePagedWindowSource{
		pages: []Page[int]{
			{Payload: 2, NextPage: "", HasMore: false},
		},
		fetches: nil,
	}
	store := &fakeCheckpointStore{checkpoints: nil}
	state := newSyncState(uuid.New(), time.Time{}, PartialCheckpoint(watermark, windowStart, windowEnd, "2"))
	p := newWindowTestPoller(store, state, src, windowEnd, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{{start: windowStart, end: windowEnd, pageToken: "2"}}, src.fetches)
	require.Equal(t, []PollCheckpoint{CompletedCheckpoint(windowEnd)}, store.checkpoints)
}

// When an initial sync spans multiple windows, only the first window starts
// at the untouched lookback boundary; every later window resumes from the
// previous window's fetched end and gets the offset.
func TestPollerMultiWindowOffsetsOnlyResumedSeams(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{upperBound: time.Time{}, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), time.Time{}, emptyCheckpoint()), src, endTime, 3*time.Hour, time.Hour, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{
		{start: endTime.Add(-3 * time.Hour), end: endTime.Add(-2 * time.Hour), pageToken: ""},
		{start: endTime.Add(-2 * time.Hour).Add(time.Millisecond), end: endTime.Add(-time.Hour), pageToken: ""},
		{start: endTime.Add(-time.Hour).Add(time.Millisecond), end: endTime, pageToken: ""},
	}, src.fetches)
	require.Equal(t, []time.Time{
		endTime.Add(-2 * time.Hour),
		endTime.Add(-time.Hour),
		endTime,
	}, store.watermarks())
}

// Exclusive-end bucketed sources set no resume offset: the watermark is
// re-used verbatim as the next window's inclusive start.
func TestPollerZeroResumeOffsetReusesWatermark(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 30, 0, time.UTC)
	watermark := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{upperBound: time.Time{}, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), watermark, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, time.Minute, 0)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{{start: watermark, end: endTime.Truncate(time.Minute), pageToken: ""}}, src.fetches)
	require.Equal(t, []time.Time{endTime.Truncate(time.Minute)}, store.watermarks())
}

// The provider finalization watermark caps the range: nothing at or beyond
// the source's upper bound is fetched, and the stored watermark stops there.
func TestPollerCapsRangeAtSourceUpperBound(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	upperBound := endTime.Add(-30 * time.Minute)
	watermark := endTime.Add(-time.Hour)
	src := &fakeWindowSource{upperBound: upperBound, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), watermark, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Equal(t, []fetchBounds{{start: watermark.Add(time.Millisecond), end: upperBound, pageToken: ""}}, src.fetches)
	require.Equal(t, []time.Time{upperBound}, store.watermarks())
}

// An up-to-date schedule (watermark at the range end) fetches nothing and
// leaves the stored watermark untouched.
func TestPollerNoFetchWhenUpToDate(t *testing.T) {
	t.Parallel()

	endTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	src := &fakeWindowSource{upperBound: time.Time{}, fetches: nil}
	store := &fakeCheckpointStore{checkpoints: nil}
	p := newWindowTestPoller(store, newSyncState(uuid.New(), endTime, emptyCheckpoint()), src, endTime, 24*time.Hour, 0, 0, time.Millisecond)

	require.NoError(t, p.Do(t.Context()))

	require.Empty(t, src.fetches)
	require.Empty(t, store.checkpoints)
}
