package telemetry

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// maxMCPServerSeriesPoints bounds the series one health read may return. The
// widest window the admin service asks for is 90 days in daily buckets, plus
// the partial bucket at each end.
const maxMCPServerSeriesPoints = 100

// MCPServerTelemetryTarget identifies one MCP server in telemetry. It is built
// from the admin service's own rows, not from Platform MCP's resolver.
type MCPServerTelemetryTarget struct {
	ProjectID string
	// MCPServerID is empty for toolset-only servers.
	MCPServerID string
	// ToolsetSlug is empty when the server has no toolset or several live
	// servers share it, because the slug would then count their calls too.
	ToolsetSlug string
	// URLSlug is the <slug> in /mcp/<slug>, matched against hook-observed calls.
	URLSlug string
}

// MCPServerOutcomes counts one server's tool calls by outcome class.
type MCPServerOutcomes struct {
	Success      int64
	Unauthorized int64
	ClientError  int64
	ServerError  int64
	Blocked      int64
	Failed       int64
	Unknown      int64
	// Watermark is the newest event observed in the project. Zero when the
	// project holds no telemetry.
	Watermark time.Time
}

// MCPServerSeriesPoint is one bucket of a server's tool call series.
type MCPServerSeriesPoint struct {
	BucketStart time.Time
	Total       int64
	Failed      int64
}

// MCPServerHealth reads one server's tool call telemetry for the admin health
// report. Like SupportCoverage, it is constructed apart from Service so the
// admin binary carries only the ClickHouse repository.
type MCPServerHealth struct {
	chRepo *repo.Queries
}

// NewMCPServerHealth builds a reader over ClickHouse. db is accepted for
// parity with NewSupportCoverage; the reader holds no Postgres state.
func NewMCPServerHealth(_ *pgxpool.Pool, chConn clickhouse.Conn) *MCPServerHealth {
	return &MCPServerHealth{chRepo: repo.New(chConn)}
}

// Outcomes counts the server's calls in [from, to] across both lanes: calls
// that reached Gram directly and calls an agent hook observed.
func (h *MCPServerHealth) Outcomes(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time) (*MCPServerOutcomes, error) {
	if target.ProjectID == "" {
		return nil, fmt.Errorf("mcp server health: project id is required")
	}

	params := repo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{target.ProjectID},
		ToolsetSlugs:         nonEmpty(target.ToolsetSlug),
		MCPServerURLSuffixes: nil,
		MCPServerIDs:         nonEmpty(target.MCPServerID),
		ToolSources:          nil,
		CanonicalIdentityOrg: "",
		TimeStart:            from.UnixNano(),
		TimeEnd:              to.UnixNano(),
		Limit:                0,
	}
	if target.URLSlug != "" {
		params.MCPServerURLSuffixes = []string{"/mcp/" + target.URLSlug}
	}
	// With no identity the breakdown falls back to every server in the
	// project, which would attribute other servers' calls to this one.
	if len(params.ToolsetSlugs) == 0 && len(params.MCPServerIDs) == 0 && len(params.MCPServerURLSuffixes) == 0 {
		return nil, fmt.Errorf("mcp server health: target names no server identity")
	}

	rows, err := h.chRepo.GetMCPOutcomeBreakdown(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("read mcp outcome breakdown: %w", err)
	}

	out := &MCPServerOutcomes{
		Success:      0,
		Unauthorized: 0,
		ClientError:  0,
		ServerError:  0,
		Blocked:      0,
		Failed:       0,
		Unknown:      0,
		Watermark:    time.Time{},
	}
	for _, row := range rows {
		count, err := toCount(row.CallCount)
		if err != nil {
			return nil, err
		}
		var slot *int64
		switch row.Outcome {
		case repo.MCPOutcomeSuccess:
			slot = &out.Success
		case repo.MCPOutcomeUnauthorized:
			slot = &out.Unauthorized
		case repo.MCPOutcomeClientError:
			slot = &out.ClientError
		case repo.MCPOutcomeServerError:
			slot = &out.ServerError
		case repo.MCPOutcomeBlocked:
			slot = &out.Blocked
		case repo.MCPOutcomeFailed:
			slot = &out.Failed
		case repo.MCPOutcomeUnknown:
			slot = &out.Unknown
		default:
			return nil, fmt.Errorf("mcp server health: unrecognized outcome class %q", row.Outcome)
		}
		if *slot > math.MaxInt64-count {
			return nil, fmt.Errorf("mcp server health: outcome count overflows")
		}
		*slot += count
	}

	watermark, err := h.chRepo.GetTelemetryWatermark(ctx, repo.GetTelemetryWatermarkParams{GramProjectIDs: []string{target.ProjectID}})
	if err != nil {
		return nil, fmt.Errorf("read telemetry watermark: %w", err)
	}
	if watermark > 0 {
		out.Watermark = time.Unix(0, watermark).UTC()
	}

	return out, nil
}

// Series buckets the server's tool calls in [from, to] from the direct lane,
// matching a row by the server id or the toolset slug as the outcome breakdown
// does. The time series filters on one identity at a time, so when the target
// has both the buckets are counted by inclusion-exclusion: calls by id, plus
// calls by slug, minus calls carrying both.
func (h *MCPServerHealth) Series(ctx context.Context, target MCPServerTelemetryTarget, from, to time.Time, bucket time.Duration) ([]MCPServerSeriesPoint, error) {
	if target.ProjectID == "" {
		return nil, fmt.Errorf("mcp server health: project id is required")
	}
	intervalSeconds := int64(bucket / time.Second)
	if intervalSeconds <= 0 {
		return nil, fmt.Errorf("mcp server health: bucket must be at least one second")
	}

	switch {
	case target.MCPServerID != "" && target.ToolsetSlug != "":
		byID, err := h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, "", from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		bySlug, err := h.seriesBuckets(ctx, target.ProjectID, "", target.ToolsetSlug, from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		byBoth, err := h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, target.ToolsetSlug, from, to, intervalSeconds)
		if err != nil {
			return nil, err
		}
		return mergeSeries(byID, bySlug, byBoth)
	case target.MCPServerID != "":
		return h.seriesBuckets(ctx, target.ProjectID, target.MCPServerID, "", from, to, intervalSeconds)
	case target.ToolsetSlug != "":
		return h.seriesBuckets(ctx, target.ProjectID, "", target.ToolsetSlug, from, to, intervalSeconds)
	default:
		return nil, fmt.Errorf("mcp server health: target names no direct-lane identity")
	}
}

// seriesBuckets reads one filtered time series. Every call over the same
// window and interval returns the same gap-filled bucket starts.
func (h *MCPServerHealth) seriesBuckets(ctx context.Context, projectID, mcpServerID, toolsetSlug string, from, to time.Time, intervalSeconds int64) ([]MCPServerSeriesPoint, error) {
	buckets, err := h.chRepo.GetTimeSeriesMetrics(ctx, repo.GetTimeSeriesMetricsParams{
		ExcludedHookSources: nil,
		GramProjectID:       projectID,
		TimeStart:           from.UnixNano(),
		TimeEnd:             to.UnixNano(),
		IntervalSeconds:     intervalSeconds,
		User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         toolsetSlug,
		RemoteMCPServerID:   "",
		MCPServerID:         mcpServerID,
		MetaMCPServerID:     "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	})
	if err != nil {
		return nil, fmt.Errorf("read mcp server time series: %w", err)
	}
	if len(buckets) > maxMCPServerSeriesPoints {
		return nil, fmt.Errorf("mcp server health: series has %d buckets, more than %d", len(buckets), maxMCPServerSeriesPoints)
	}

	points := make([]MCPServerSeriesPoint, 0, len(buckets))
	for _, b := range buckets {
		total, err := toCount(b.TotalToolCalls)
		if err != nil {
			return nil, err
		}
		failed, err := toCount(b.FailedToolCalls)
		if err != nil {
			return nil, err
		}
		if failed > total {
			return nil, fmt.Errorf("mcp server health: bucket has more failed calls than calls")
		}
		points = append(points, MCPServerSeriesPoint{
			BucketStart: time.Unix(0, b.BucketTimeUnixNano).UTC(),
			Total:       total,
			Failed:      failed,
		})
	}
	return points, nil
}

// mergeSeries returns byID + bySlug - byBoth per bucket. The three reads share
// a window and interval, so their buckets must line up; anything else is a
// malformed read rather than something to reconcile.
func mergeSeries(byID, bySlug, byBoth []MCPServerSeriesPoint) ([]MCPServerSeriesPoint, error) {
	if len(bySlug) != len(byID) || len(byBoth) != len(byID) {
		return nil, fmt.Errorf("mcp server health: series reads returned %d, %d and %d buckets", len(byID), len(bySlug), len(byBoth))
	}
	merged := make([]MCPServerSeriesPoint, len(byID))
	for i := range byID {
		if !bySlug[i].BucketStart.Equal(byID[i].BucketStart) || !byBoth[i].BucketStart.Equal(byID[i].BucketStart) {
			return nil, fmt.Errorf("mcp server health: series reads disagree on bucket %d", i)
		}
		total := byID[i].Total + bySlug[i].Total - byBoth[i].Total
		failed := byID[i].Failed + bySlug[i].Failed - byBoth[i].Failed
		if total < 0 || failed < 0 || failed > total {
			return nil, fmt.Errorf("mcp server health: series bucket %d does not reconcile", i)
		}
		merged[i] = MCPServerSeriesPoint{BucketStart: byID[i].BucketStart, Total: total, Failed: failed}
	}
	return merged, nil
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func toCount(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("mcp server health: count %d overflows", value)
	}
	return int64(value), nil
}
