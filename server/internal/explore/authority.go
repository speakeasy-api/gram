package explore

import (
	"fmt"
	"strings"
)

type sourceAuthority struct {
	Channel string
	Rank    int
}

// authorityRegistry ranks observation channels independently for every
// canonical field inside an already-scoped semantic dataset. NULL means
// absent, so an explicit zero or empty value can supersede a lower-authority
// value without allowing another measurement grain to participate.
var authorityRegistry = map[string][]sourceAuthority{
	"occurred_at": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"event_name": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"provider": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"surface": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"account_type": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"user_key": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"session_id": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"turn_id": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"query_source": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"request_model": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"response_model": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"tool_name": {
		{Channel: "agent_hook", Rank: 200},
		{Channel: "provider_otel", Rank: 100},
	},
	"mcp_server": {
		{Channel: "agent_hook", Rank: 200},
		{Channel: "provider_otel", Rank: 100},
	},
	"skill_name": {
		{Channel: "agent_hook", Rank: 200},
		{Channel: "provider_otel", Rank: 100},
	},
	"status": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"terminal": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"duration_ms": {
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"cost_usd": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"input_tokens": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"output_tokens": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"cache_read_tokens": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
	"cache_write_tokens": {
		{Channel: "provider_api", Rank: 300},
		{Channel: "provider_otel", Rank: 200},
		{Channel: "agent_hook", Rank: 100},
	},
}

func authorityRankExpr(member string) string {
	ranks, ok := authorityRegistry[member]
	if !ok {
		return "toUInt16(0)"
	}

	args := make([]string, 0, len(ranks)*2+1)
	for _, authority := range ranks {
		args = append(
			args,
			fmt.Sprintf("source_channel = '%s'", authority.Channel),
			fmt.Sprintf("toUInt16(%d)", authority.Rank),
		)
	}
	args = append(args, "toUInt16(0)")
	return "multiIf(" + strings.Join(args, ", ") + ")"
}

func authorityWeightExpr(member string) string {
	return fmt.Sprintf("tuple(%s, observed_at, src_event_id)", authorityRankExpr(member))
}

func candidateAuthorityWeightExpr(member string) string {
	return fmt.Sprintf(
		"tuple(%s, %s, %s)",
		authorityRankExpr(member),
		candidateColumn("observed_at"),
		candidateColumn("src_event_id"),
	)
}

func observationWeightExpr() string {
	return "tuple(observed_at, src_event_id)"
}

func componentWeightExpr() string {
	return fmt.Sprintf(
		"tuple(%s, %s)",
		componentColumn("observed_at"),
		componentColumn("src_event_id"),
	)
}

func canonicalColumn(column string) string {
	return "c_" + column
}

func componentColumn(column string) string {
	return "component_" + column
}

func candidateColumn(column string) string {
	return "candidate_" + column
}

func standardCanonicalFieldExpr(field Field) string {
	return fmt.Sprintf(
		"argMaxIf(%s, %s, isNotNull(%s)) AS %s",
		field.Expr,
		authorityWeightExpr(field.Name),
		field.Expr,
		canonicalColumn(field.Name),
	)
}

func componentCanonicalFieldExpr(field Field) string {
	return fmt.Sprintf(
		"argMaxIf(%s, %s, isNotNull(%s)) AS %s",
		field.Expr,
		observationWeightExpr(),
		field.Expr,
		componentColumn(field.Name),
	)
}
