package metamcp

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Instructions is the built-in server-instructions block answered by
// initialize and server/discover when a gateway has no operator-authored
// instructions of its own. It stays static so the dashboard can ship a copy;
// the caller's servers are prepended separately by WithMemberSummary.
const Instructions = `This endpoint fronts a set of MCP servers as one, exposing a handful of tools instead of every member's full catalog.

Work from the outside in:

1. list_servers — what is reachable, what each system is for, and each member's status: "available", "unavailable" when the member's tunnel is down, or "unknown" when it was not checked (this list does not contact remote servers; call connection_status to check sign-in).
2. describe_server — that server's tools, as qualified names (server--tool) with descriptions but no input schemas.
3. describe_tools — input schemas for the specific tools you intend to call.
4. execute_tool — run one: the qualified name goes in "name", its JSON payload in "arguments".

Never execute a name you have not described: arguments guessed from a tool name will fail validation.

If a call fails, re-check list_servers before retrying. "unknown" means not checked, not broken: call connection_status to check sign-in. Such members usually answer describe and execute calls normally. Only "unavailable" means the member cannot currently be reached.

If a member says it needs authentication, call connection_status. A connection_status of "unavailable" means a temporary problem: retry shortly. Live connections show current sign-in status and, when this gateway has a sign-in page, link to it. Cached connections report verification at consent without a new probe; tool calls check current authorization. If new consent is required, reconnect this gateway in your AI app.`

// ResolveInstructions returns custom instructions verbatim when nonblank,
// otherwise falling back to the built-in gateway instructions.
func ResolveInstructions(custom *string) string {
	if custom == nil || strings.TrimSpace(*custom) == "" {
		return Instructions
	}
	return *custom
}

// ResolveDiscoveryInstructions describes the active catalog unless the owner
// supplies custom instructions.
func ResolveDiscoveryInstructions(custom *string, mode DiscoveryMode) string {
	if custom != nil && strings.TrimSpace(*custom) != "" {
		return *custom
	}
	if mode == DiscoveryModeDirect {
		return "This gateway exposes the tools from its MCP servers directly. Use tools/list to discover tool definitions, then call each tool by its qualified server--tool name."
	}
	if mode == DiscoveryModeCode {
		return "Code Mode exposes one execute tool for Python. Inside execute, tools is prebound and top-level await works. Discover with tools.search, inspect tools.describe, call tools.call; await each. Use described input schemas and exact server--tool paths. Fresh state per run. Limits, results and retry rules are in the execute description."
	}
	return Instructions
}

// memberSummaryLimit fits the summary in the one line Codex keeps for a
// deferred MCP server: the first line, cut at 250 characters.
const memberSummaryLimit = 250

// MemberLabel names one server for the instructions summary.
type MemberLabel struct {
	Slug string
	Name string
}

// WithMemberSummary prepends a one-line list of the caller's servers to
// built-in instructions, so agents that see only the first line still learn
// what the gateway reaches. Custom instructions and an empty member set are
// returned unchanged.
func WithMemberSummary(instructions string, custom *string, members []MemberLabel) string {
	if len(members) == 0 || (custom != nil && strings.TrimSpace(*custom) != "") {
		return instructions
	}
	return MemberSummary(members) + "\n\n" + instructions
}

// MemberSummary lists server names in at most memberSummaryLimit characters,
// counting the ones that do not fit.
func MemberSummary(members []MemberLabel) string {
	const prefix = "Tools from these servers: "
	summary := prefix
	for i, member := range members {
		label := member.Name
		if label == "" {
			label = member.Slug
		}
		// Quoting keeps operator labels from injecting new instruction lines.
		next := summary
		if i > 0 {
			next += ", "
		}
		next += strconv.Quote(label)
		rest := ""
		if left := len(members) - i - 1; left > 0 {
			rest = fmt.Sprintf(" and %d more", left)
		}
		if utf8.RuneCountInString(next+rest+".") > memberSummaryLimit {
			if i == 0 {
				return fmt.Sprintf("Tools from %d servers.", len(members))
			}
			return fmt.Sprintf("%s and %d more.", summary, len(members)-i)
		}
		summary = next
	}
	return summary + "."
}
