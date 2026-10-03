package dialect

import "time"

// The canonical metrics agent_metrics carries for usage. The names match the
// agent_events usage columns, so a measure means the same thing in both
// tables.
const (
	CanonicalMetricInputTokens      = "input_tokens"
	CanonicalMetricOutputTokens     = "output_tokens"
	CanonicalMetricCacheReadTokens  = "cache_read_tokens"
	CanonicalMetricCacheWriteTokens = "cache_write_tokens" //nolint:gosec // G101: a metric name, not a credential
	CanonicalMetricCostUSD          = "cost_usd"
)

// Reporting grains, as agent_metrics.grain holds them.
const (
	GrainMinute = "minute"
	GrainHour   = "hour"
	GrainPoint  = "point"
)

// Temporalities, as agent_metrics.temporality holds them.
const (
	TemporalityDelta = "delta"
)

const (
	unitTokens = "{token}"
	unitUSD    = "USD"
)

// UsageMeasure is one usage value a poller row can state: the attribute it
// is stated under, the canonical metric it is, and its unit.
type UsageMeasure struct {
	// Key is the telemetry attribute that carries the value.
	Key string

	// CanonicalMetric is the agent_metrics canonical name for the value.
	CanonicalMetric string

	// Unit is the value's unit.
	Unit string
}

// UsageMeasures are the usage values the provider pollers state, in the
// order rows are written. The pollers normalize every provider onto these
// keys: input tokens exclude cache reads, and cost is in US dollars.
var UsageMeasures = []UsageMeasure{
	{Key: "gen_ai.usage.input_tokens", CanonicalMetric: CanonicalMetricInputTokens, Unit: unitTokens},
	{Key: "gen_ai.usage.output_tokens", CanonicalMetric: CanonicalMetricOutputTokens, Unit: unitTokens},
	{Key: "gen_ai.usage.cache_read.input_tokens", CanonicalMetric: CanonicalMetricCacheReadTokens, Unit: unitTokens},
	{Key: "gen_ai.usage.cache_creation.input_tokens", CanonicalMetric: CanonicalMetricCacheWriteTokens, Unit: unitTokens},
	{Key: "gen_ai.usage.cost", CanonicalMetric: CanonicalMetricCostUSD, Unit: unitUSD},
}

// UsageFeed is what Gram knows about one provider API usage feed: how its
// rows are grained and what identifies one measurement across re-polls.
type UsageFeed struct {
	// Grain is the feed's native reporting grain.
	Grain string

	// Window is the interval one row covers, starting at the row's time.
	// Zero when a row is a point observation or its interval is not known.
	Window time.Duration

	// IdentityKey is the attribute carrying the feed's own hash of a
	// measurement. The pollers do not deduplicate every feed on insert, so a
	// re-polled window writes the same measurement again under a new row id;
	// keying on the hash lets agent_metrics collapse it. Empty when the feed
	// states none.
	IdentityKey string
}

// usageFeeds maps each poller's gram_urn to its feed.
var usageFeeds = map[string]UsageFeed{
	// Anthropic Admin Analytics reports one row per user, model and one-minute
	// bucket, timed at the bucket start.
	"claude_chat:usage:metrics": {Grain: GrainMinute, Window: time.Minute, IdentityKey: "claude_chat.event_hash"},
	"claude_chat:cost:metrics":  {Grain: GrainMinute, Window: time.Minute, IdentityKey: "claude_chat.event_hash"},

	// The OpenAI compliance COSTS feed reports hourly per-user aggregates, but
	// a row is timed at the event when the event states a time, so its
	// interval is not known from the row.
	"codex:usage:metrics":   {Grain: GrainHour, Window: 0, IdentityKey: "codex.compliance.event_hash"},
	"chatgpt:usage:metrics": {Grain: GrainHour, Window: 0, IdentityKey: "codex.compliance.event_hash"},

	// The Cursor Admin API reports one row per request.
	"cursor:usage:metrics": {Grain: GrainPoint, Window: 0, IdentityKey: "cursor.event_hash"},
}

// UsageFeedFor returns the feed a poller row belongs to, by its gram_urn.
func UsageFeedFor(gramURN string) (UsageFeed, bool) {
	feed, ok := usageFeeds[gramURN]
	return feed, ok
}
