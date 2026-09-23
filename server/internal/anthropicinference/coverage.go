package anthropicinference

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// meterUnscannedBlocks counts content blocks a delivery carried that produced
// no policy input. It is a coverage signal for the enforcement dashboard: a
// block type with sustained volume here is content policies never see.
const meterUnscannedBlocks = "risk.enforcement.inference.unscanned_content_blocks"

// Reasons a content block produced no policy input, recorded as a metric
// dimension alongside the block type.
const (
	// reasonUnsupportedType is a block whose type this package does not
	// decode, such as an image or document block or a type Anthropic added
	// after this code was written.
	reasonUnsupportedType = "unsupported_type"

	// reasonNoScannableText is a decoded block that carried neither text nor
	// a tool name, such as an empty text block or a tool result with no
	// content.
	reasonNoScannableText = "no_scannable_text"
)

// otherBlockType replaces a block type outside reportedBlockTypes on the
// metric. Block types reach this package inside a customer transcript, so an
// unfiltered dimension would let transcript content decide how many time
// series the counter creates. The exact type stays on the accompanying log
// line, so volume on this bucket can still be traced back to a name and that
// name added to reportedBlockTypes.
const otherBlockType = "other"

// reportedBlockTypes are the content block types allowed on the counter: the
// four this package decodes, plus the documented Anthropic types it does not.
var reportedBlockTypes = map[string]bool{
	"text":                       true,
	"attachment":                 true,
	"tool_use":                   true,
	"tool_result":                true,
	"image":                      true,
	"document":                   true,
	"search_result":              true,
	"thinking":                   true,
	"redacted_thinking":          true,
	"server_tool_use":            true,
	"web_search_tool_result":     true,
	"web_fetch_tool_result":      true,
	"code_execution_tool_result": true,
	"mcp_tool_use":               true,
	"mcp_tool_result":            true,
	"container_upload":           true,
}

// maxLoggedBlockType bounds the provider-supplied type on the log line. A type
// name is a short identifier; anything longer is malformed and is truncated
// rather than written whole into the log field.
const maxLoggedBlockType = 64

// maxTrackedTypes bounds how many distinct block types one delivery reports by
// name. A transcript is customer content and can name far more types than
// Anthropic documents; past this many, further types are counted under
// otherBlockType so a single delivery cannot flood the logs. A tally therefore
// holds at most this many named entries per skip reason plus the aggregate.
const maxTrackedTypes = 24

// unscannedBlock identifies a content block that produced no policy input.
// Only the block's type and why it was skipped are retained; its content stays
// in the transcript.
type unscannedBlock struct {
	// blockType is the type reported by the provider, empty when the block
	// carried no type at all.
	blockType string

	// reason is why the block produced no scannable input.
	reason string
}

// coverage tallies unscanned blocks by type and reason over one delivery.
type coverage map[unscannedBlock]int

func (c coverage) add(blockType string, reason string) {
	entry := unscannedBlock{blockType: blockType, reason: reason}
	if _, tracked := c[entry]; !tracked && len(c) >= maxTrackedTypes {
		entry = unscannedBlock{blockType: otherBlockType, reason: reason}
	}
	c[entry]++
}

// coverageMetrics holds this package's instruments. The zero value records
// nothing, so a Service built without a meter provider still runs.
type coverageMetrics struct {
	unscannedBlocks metric.Int64Counter
}

func newCoverageMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) coverageMetrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/anthropicinference")

	unscannedBlocks, err := meter.Int64Counter(
		meterUnscannedBlocks,
		metric.WithDescription("Content blocks an inference hook delivery did not extract any scannable text from, by block type and reason"),
		metric.WithUnit("{block}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUnscannedBlocks), attr.SlogError(err))
	}

	return coverageMetrics{unscannedBlocks: unscannedBlocks}
}

// reportCoverage records, per block type and reason, the blocks this delivery
// scanned nothing from. The log line carries the provider's own type name, so
// a type the counter buckets as "other" can still be identified; neither
// surface carries block content.
func (s *Service) reportCoverage(ctx context.Context, config Config, tally coverage) {
	entries := make([]unscannedBlock, 0, len(tally))
	for entry := range tally {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b unscannedBlock) int {
		return cmp.Or(cmp.Compare(a.blockType, b.blockType), cmp.Compare(a.reason, b.reason))
	})
	for _, entry := range entries {
		count := tally[entry]
		s.logger.InfoContext(ctx, "inference content block not scanned",
			attr.SlogOrganizationID(config.OrganizationID), attr.SlogProjectID(config.ProjectID.String()),
			attr.SlogInferenceContentBlockType(loggedBlockType(entry.blockType)),
			attr.SlogInferenceContentBlockSkipReason(entry.reason),
			attr.SlogInferenceUnscannedBlockCount(count))
		if s.metrics.unscannedBlocks == nil {
			continue
		}
		s.metrics.unscannedBlocks.Add(ctx, int64(count), metric.WithAttributes(
			attr.InferenceContentBlockType(reportedBlockType(entry.blockType)),
			attr.InferenceContentBlockSkipReason(entry.reason),
		))
	}
}

func reportedBlockType(blockType string) string {
	if reportedBlockTypes[blockType] {
		return blockType
	}
	return otherBlockType
}

func loggedBlockType(blockType string) string {
	runes := []rune(blockType)
	if len(runes) > maxLoggedBlockType {
		return string(runes[:maxLoggedBlockType])
	}
	return blockType
}
