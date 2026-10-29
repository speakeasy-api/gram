package telemetry

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	// metaMCPPhaseDailyWindow is the shortest window whose discovery and
	// execution series is bucketed by day: a week or more reads by day.
	metaMCPPhaseDailyWindow = 7 * 24 * time.Hour

	// metaMCPPhaseHourlyWindow is the shortest window bucketed by hour. Up to
	// six hours, five-minute buckets keep a short window's shape visible.
	metaMCPPhaseHourlyWindow = 6 * time.Hour

	// maxMetaMCPPhaseBuckets bounds the series at a year of daily buckets.
	// Telemetry is kept for 90 days, so a wider window only widens buckets.
	maxMetaMCPPhaseBuckets = 366
)

// metaMCPPhaseModes orders discovery modes from the most discovery to the
// least, then rows that recorded no mode.
var metaMCPPhaseModes = []telem_gen.MetaMcpDiscoveryMode{
	telem_gen.MetaMcpDiscoveryMode(metamcp.DiscoveryModeProgressive),
	telem_gen.MetaMcpDiscoveryMode(metamcp.DiscoveryModeCode),
	telem_gen.MetaMcpDiscoveryMode(metamcp.DiscoveryModeDirect),
	telem_gen.MetaMcpDiscoveryMode(repo.MetaMCPModeUnrecorded),
}

// metaMCPCallPositions orders the position ranges a session's calls fall in.
var metaMCPCallPositions = []telem_gen.MetaMcpCallPosition{"1", "2", "3-5", "6-10", "11-20", "21+"}

// metaMCPPhaseInterval chooses the series bucket width for a window, in
// seconds.
func metaMCPPhaseInterval(timeStart, timeEnd int64) int64 {
	window := time.Duration(timeEnd - timeStart)
	interval := 5 * time.Minute
	switch {
	case window >= metaMCPPhaseDailyWindow:
		interval = 24 * time.Hour
	case window > metaMCPPhaseHourlyWindow:
		interval = time.Hour
	}
	windowSeconds := int64(window / time.Second)
	return max(int64(interval/time.Second), (windowSeconds+maxMetaMCPPhaseBuckets-1)/maxMetaMCPPhaseBuckets)
}

// metaMCPPhaseMode is a row's mode as the API reports it. A value this build
// does not know, such as one a newer release recorded, reports as unrecorded.
func metaMCPPhaseMode(value string) telem_gen.MetaMcpDiscoveryMode {
	mode := telem_gen.MetaMcpDiscoveryMode(value)
	if !slices.Contains(metaMCPPhaseModes, mode) {
		return telem_gen.MetaMcpDiscoveryMode(repo.MetaMCPModeUnrecorded)
	}
	return mode
}

// buildMetaMCPCallPhases shapes a gateway's phase rows for the API: totals per
// mode with the previous window beside them, positions in session order, and
// the series in time order.
func buildMetaMCPCallPhases(phases repo.MetaMCPPhases, intervalSeconds int64) *telem_gen.MetaMcpCallPhases {
	totals := map[telem_gen.MetaMcpDiscoveryMode]*telem_gen.MetaMcpModePhases{}
	total := func(mode telem_gen.MetaMcpDiscoveryMode) *telem_gen.MetaMcpModePhases {
		entry, ok := totals[mode]
		if !ok {
			entry = &telem_gen.MetaMcpModePhases{
				Mode:                   mode,
				DiscoveryCalls:         0,
				ExecutionCalls:         0,
				ExecuteRuns:            0,
				PreviousDiscoveryCalls: 0,
				PreviousExecutionCalls: 0,
				PreviousExecuteRuns:    0,
			}
			totals[mode] = entry
		}
		return entry
	}

	type seriesKey struct {
		bucket int64
		mode   telem_gen.MetaMcpDiscoveryMode
	}
	var callsWithoutSession uint64
	bySeries := map[seriesKey]*telem_gen.MetaMcpPhaseBucket{}
	seriesKeys := make([]seriesKey, 0, len(phases.Series))
	for _, row := range phases.Series {
		mode := metaMCPPhaseMode(row.Mode)
		entry := total(mode)
		entry.DiscoveryCalls += uint64ToInt64(row.DiscoveryCalls)
		entry.ExecutionCalls += uint64ToInt64(row.ExecutionCalls)
		entry.ExecuteRuns += uint64ToInt64(row.ExecuteRuns)
		callsWithoutSession += row.CallsWithoutSession

		// Unknown modes fold into unrecorded, so a key can repeat.
		key := seriesKey{bucket: row.BucketTimeUnixNano, mode: mode}
		bucket, ok := bySeries[key]
		if !ok {
			bucket = &telem_gen.MetaMcpPhaseBucket{
				BucketTimeUnixNano: strconv.FormatInt(row.BucketTimeUnixNano, 10),
				Mode:               mode,
				DiscoveryCalls:     0,
				ExecutionCalls:     0,
				ExecuteRuns:        0,
			}
			bySeries[key] = bucket
			seriesKeys = append(seriesKeys, key)
		}
		bucket.DiscoveryCalls += uint64ToInt64(row.DiscoveryCalls)
		bucket.ExecutionCalls += uint64ToInt64(row.ExecutionCalls)
		bucket.ExecuteRuns += uint64ToInt64(row.ExecuteRuns)
	}
	slices.SortFunc(seriesKeys, func(a, b seriesKey) int {
		if a.bucket != b.bucket {
			return cmp.Compare(a.bucket, b.bucket)
		}
		return cmp.Compare(slices.Index(metaMCPPhaseModes, a.mode), slices.Index(metaMCPPhaseModes, b.mode))
	})
	series := make([]*telem_gen.MetaMcpPhaseBucket, 0, len(seriesKeys))
	for _, key := range seriesKeys {
		series = append(series, bySeries[key])
	}
	for _, row := range phases.Previous {
		entry := total(metaMCPPhaseMode(row.Mode))
		entry.PreviousDiscoveryCalls += uint64ToInt64(row.DiscoveryCalls)
		entry.PreviousExecutionCalls += uint64ToInt64(row.ExecutionCalls)
		entry.PreviousExecuteRuns += uint64ToInt64(row.ExecuteRuns)
	}

	modes := make([]*telem_gen.MetaMcpModePhases, 0, len(totals))
	for _, mode := range metaMCPPhaseModes {
		if entry, ok := totals[mode]; ok {
			modes = append(modes, entry)
		}
	}

	type positionKey struct {
		mode     telem_gen.MetaMcpDiscoveryMode
		position telem_gen.MetaMcpCallPosition
	}
	byPosition := map[positionKey]*telem_gen.MetaMcpPositionPhases{}
	for _, row := range phases.Positions {
		key := positionKey{mode: metaMCPPhaseMode(row.Mode), position: telem_gen.MetaMcpCallPosition(row.Position)}
		entry, ok := byPosition[key]
		if !ok {
			entry = &telem_gen.MetaMcpPositionPhases{Mode: key.mode, Position: key.position, DiscoveryCalls: 0, ExecutionCalls: 0}
			byPosition[key] = entry
		}
		entry.DiscoveryCalls += uint64ToInt64(row.DiscoveryCalls)
		entry.ExecutionCalls += uint64ToInt64(row.ExecutionCalls)
	}
	positions := make([]*telem_gen.MetaMcpPositionPhases, 0, len(byPosition))
	for _, mode := range metaMCPPhaseModes {
		for _, position := range metaMCPCallPositions {
			if entry, ok := byPosition[positionKey{mode: mode, position: position}]; ok {
				positions = append(positions, entry)
			}
		}
	}

	return &telem_gen.MetaMcpCallPhases{
		Modes:               modes,
		Positions:           positions,
		IntervalSeconds:     intervalSeconds,
		Series:              series,
		CallsWithoutSession: uint64ToInt64(callsWithoutSession),
	}
}
