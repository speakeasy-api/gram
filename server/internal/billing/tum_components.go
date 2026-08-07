package billing

import "slices"

// TumComponent is one token measure summed into tokens under management.
type TumComponent struct {
	// Key is the component's stable snake_case identifier.
	Key string
	// Column is the attribute_metrics_summaries aggregate column holding the
	// component's token count (read back with sumIfMerge).
	Column string
}

// tumComponents is the definition of the tokens-under-management measure:
// input + output + cache WRITES. Cache reads are deliberately excluded — a
// cache read re-observes prompt content that was already counted when it
// entered the cache (and dwarfs everything else: agent sessions re-read
// their whole cached prefix on every turn) — while a cache write is new
// prompt content being observed for the first time, so it counts.
//
// This registry is the single source of truth for what TUM is made of: the
// billing queries and customer-facing reports (the weekly usage summary
// email's total) build their measure expression from it, so adding or
// removing a component here changes what is billed and what is reported in
// lockstep. The population side of the definition — WHICH rows
// these columns are summed over — is GramHostedHookSourceStrings, the
// observed-traffic exclusion list.
var tumComponents = []TumComponent{
	{Key: "input_tokens", Column: "total_input_tokens"},
	{Key: "output_tokens", Column: "total_output_tokens"},
	{Key: "cache_write_tokens", Column: "cache_creation_input_tokens"},
}

// TumComponents lists the token measures that make up tokens under
// management.
func TumComponents() []TumComponent {
	return slices.Clone(tumComponents)
}
