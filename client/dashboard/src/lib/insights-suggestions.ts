import {
  Blocks,
  Bot,
  ChartLine,
  CircleAlert,
  Coins,
  Gauge,
  History,
  KeyRound,
  MessageCircle,
  Puzzle,
  Rocket,
  Search,
  Server,
  Settings,
  ShieldAlert,
  Sparkles,
  Terminal,
  TrendingUp,
  Users,
  Wrench,
  Zap,
  type LucideIcon,
} from "lucide-react";

/**
 * Every Project Assistant suggestion in the dashboard, colocated in one
 * place and keyed by route. The docked composer and the chat panel's
 * welcome screen both read from here.
 *
 * Three kinds of entries live in `INSIGHTS_SUGGESTIONS`:
 *
 * 1. Route entries (static arrays) keyed by the project-relative path, e.g.
 *    `"logs/mcp"`. These are resolved automatically by `getRouteSuggestions`
 *    — pages don't need to mount anything to get them. A page-level
 *    `<InsightsConfig suggestions={...}>` still wins when mounted.
 * 2. Parameterized entries (functions) for pages whose prompts need page
 *    data (a user's name, the selected date range). Pages import these and
 *    pass the result to `<InsightsConfig suggestions={...}>`.
 * 3. Interaction entries (functions) for "Explore with AI" chart CTAs,
 *    keyed under the route with a `#` fragment, e.g. `"home#top-users"`.
 *
 * Conventions: `title` is the chip text — a short question; `icon` names the
 * subject glyph (see `INSIGHTS_SUGGESTION_ICONS`); `prompt` is the full
 * message sent to the assistant.
 *
 * Route resolution walks path segments longest-first, so `"insights/employees"`
 * matches `/…/insights/employees/jane` until the detail page mounts its own
 * parameterized config. The `""` key is the project home page; `"default"` is
 * the project-wide fallback used when no route entry matches.
 */

/** Subject glyphs available to suggestion chips. */
export const INSIGHTS_SUGGESTION_ICONS = {
  alert: CircleAlert,
  blocks: Blocks,
  bot: Bot,
  chart: ChartLine,
  chat: MessageCircle,
  coins: Coins,
  gauge: Gauge,
  history: History,
  key: KeyRound,
  puzzle: Puzzle,
  rocket: Rocket,
  search: Search,
  server: Server,
  settings: Settings,
  shield: ShieldAlert,
  sparkles: Sparkles,
  terminal: Terminal,
  trend: TrendingUp,
  users: Users,
  wrench: Wrench,
  zap: Zap,
} satisfies Record<string, LucideIcon>;

type InsightsSuggestionIcon = keyof typeof INSIGHTS_SUGGESTION_ICONS;

export interface InsightsSuggestion {
  /** Chip text — a short question. */
  title: string;
  label: string;
  prompt: string;
  /** Subject glyph; chips fall back to `sparkles` when omitted. */
  icon?: InsightsSuggestionIcon;
}

/**
 * Starter prompts for the full-page chat landing + home widget. Pitched at the
 * questions an enterprise platform, security, or FinOps lead actually asks of
 * their AI usage — reliability, governance, exposure, spend, and adoption.
 */
export const CHAT_LANDING_SUGGESTIONS: InsightsSuggestion[] = [
  {
    title: "Any secrets or PII in chats?",
    label: "Sensitive-data scan",
    icon: "shield",
    prompt:
      "Scan recent agent conversations for sensitive-data exposure — leaked secrets, PII, or prompt-injection attempts. Show me the findings, how serious each is, and which chats they came from.",
  },
  {
    title: "Any unapproved servers?",
    label: "Shadow MCP usage",
    icon: "server",
    prompt:
      "Are agents calling any MCP servers or tools that aren't on our approved list? List the shadow servers, who's calling them, and how often.",
  },
  {
    title: "Which tools are erroring?",
    label: "Failure hotspots",
    icon: "alert",
    prompt:
      "Which tools and MCP servers are failing the most right now? Group the errors by pattern and tell me which users or clients are hitting them hardest.",
  },
  {
    title: "What's driving our spend?",
    label: "Token & cost drivers",
    icon: "coins",
    prompt:
      "Break down token spend over the last 30 days by tool, model, and client, and call out the biggest cost drivers and any sudden jumps.",
  },
  {
    title: "Is adoption growing?",
    label: "Week-over-week adoption",
    icon: "trend",
    prompt:
      "How is adoption trending week over week — active end users, agent sessions, and tool calls — and which teams or clients are driving the growth?",
  },
  {
    title: "Who are the heaviest users this week?",
    label: "Top users by volume",
    icon: "users",
    prompt:
      "Who are the heaviest end users this week by tool calls and agent sessions, and what is each of them mainly doing?",
  },
  {
    title: "What's getting slower?",
    label: "Latency regressions",
    icon: "gauge",
    prompt:
      "Which tools have the worst p95 latency, and which ones have regressed the most this week compared to last? Flag anything that looks like it's degrading.",
  },
];

type SuggestionEntry =
  | InsightsSuggestion[]
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- per-entry signatures vary; call sites get exact types from the object literal
  | ((...args: any[]) => InsightsSuggestion[]);

export const INSIGHTS_SUGGESTIONS = {
  /** Project-wide fallback (used when no route entry matches). */
  default: [
    {
      title: "What's erroring?",
      label: "Recent error trends",
      icon: "alert",
      prompt: "Summarize the most common error patterns in the last 24 hours.",
    },
    {
      title: "Most-called tools?",
      label: "Most-called tools",
      icon: "wrench",
      prompt: "Which tools have been called most often this week?",
    },
    {
      title: "Slowest tools?",
      label: "Latency outliers",
      icon: "gauge",
      prompt: "Find tools with the slowest p95 latency in the last day.",
    },
  ],

  /** Project home (overview dashboard). */
  "": [
    {
      title: "What's happening?",
      label: "Summarize activity",
      icon: "trend",
      prompt:
        "Give me an overview of activity in this project: tool calls, agent sessions, and errors over the last 7 days.",
    },
    {
      title: "What changed this week?",
      label: "Week-over-week trends",
      icon: "history",
      prompt:
        "How did usage patterns change this week compared to last week? Call out anything notable.",
    },
    {
      title: "Anything broken?",
      label: "Anything to look into?",
      icon: "alert",
      prompt:
        "Are there any error spikes, failing tools, or unusual activity I should look into right now?",
    },
  ],

  // "Explore with AI" chart CTAs on the home dashboard.
  "home#top-users": (rangeLabel: string) => [
    {
      title: "Who are my top users?",
      label: rangeLabel,
      icon: "users" as const,
      prompt: `Who are my top 5 end users in the ${rangeLabel}, and what is each user's main usage pattern — tool calls, skill invocations, agent sessions, or a mix?`,
    },
  ],
  "home#top-servers": (rangeLabel: string) => [
    {
      title: "Which servers are hot?",
      label: rangeLabel,
      icon: "server" as const,
      prompt: `Which servers received the most tool calls in the ${rangeLabel}, and which specific tools on each server are driving that volume? Lets look at data from all logs including hooks telemetry.`,
    },
  ],
  "home#agent-sessions": (rangeLabel: string) => [
    {
      title: "How do power users work?",
      label: rangeLabel,
      icon: "bot" as const,
      prompt: `For the users with the most agent sessions in the ${rangeLabel}, what are the common prompts they send and which tools get invoked most often?`,
    },
  ],
  "home#llm-clients": (rangeLabel: string) => [
    {
      title: "Which clients are reliable?",
      label: rangeLabel,
      icon: "gauge" as const,
      prompt: `Break down tool-call activity by LLM client in the ${rangeLabel} and highlight any clients with unusually high error rates or latency.`,
    },
  ],

  playground: [
    {
      title: "Which tools should I test?",
      label: "Recently failing tools",
      icon: "wrench",
      prompt:
        "Which tools in this project have been failing recently? I want to test them in the playground.",
    },
    {
      title: "Any recent errors?",
      label: "Recent tool errors",
      icon: "alert",
      prompt:
        "Show me recent tool call errors with their inputs so I can reproduce them here.",
    },
    {
      title: "Which tools are slow?",
      label: "Latency outliers",
      icon: "gauge",
      prompt:
        "Which tools have the slowest latency? Help me figure out what to test.",
    },
  ],

  elements: [
    {
      title: "How many chat sessions?",
      label: "Embedded chat sessions",
      icon: "chat",
      prompt:
        "How many chat sessions have come through embedded clients recently, and how is that trending?",
    },
    {
      title: "What do users ask?",
      label: "What users ask",
      icon: "search",
      prompt: "What are end users asking most often in chat sessions?",
    },
    {
      title: "Which chats failed?",
      label: "Chats that errored",
      icon: "alert",
      prompt:
        "Find recent chat sessions that ended in errors and summarize what went wrong.",
    },
  ],

  integrations: [
    {
      title: "Which integrations are used?",
      label: "What's actually used",
      icon: "puzzle",
      prompt:
        "Which integrations are actively being used, based on recent tool calls?",
    },
    {
      title: "Any integration errors?",
      label: "Errors by integration",
      icon: "alert",
      prompt:
        "Summarize errors coming from integration tools over the last week.",
    },
    {
      title: "Anything unused?",
      label: "No traffic in 30 days",
      icon: "search",
      prompt:
        "Are there integrations or tool sources with no usage in the last 30 days?",
    },
  ],

  "custom-tools": [
    {
      title: "How used are my tools?",
      label: "Calls by client",
      icon: "wrench",
      prompt:
        "How often are my custom tools being called, and by which clients?",
    },
    {
      title: "Any failing tools?",
      label: "Recent errors",
      icon: "alert",
      prompt:
        "Are any custom tools failing? Show me their recent errors and likely causes.",
    },
    {
      title: "Which are slowest?",
      label: "Latency outliers",
      icon: "gauge",
      prompt: "Which custom tools have the slowest latency?",
    },
  ],

  prompts: [
    {
      title: "Most-used prompts?",
      label: "Most-used prompts",
      icon: "trend",
      prompt: "Which prompts are used most often in recent agent sessions?",
    },
    {
      title: "Do prompts work?",
      label: "Do they work?",
      icon: "alert",
      prompt:
        "For sessions that used project prompts, how often did the conversation end in an error or retry?",
    },
    {
      title: "Any stale prompts?",
      label: "Unused prompts",
      icon: "search",
      prompt: "Are there prompts with no usage in the last 30 days?",
    },
  ],

  sources: [
    {
      title: "Which sources are busiest?",
      label: "Calls by source",
      icon: "server",
      prompt: "Which tool sources generate the most tool calls?",
    },
    {
      title: "Errors by source?",
      label: "Failing sources",
      icon: "alert",
      prompt: "Break down recent tool errors by source. Which source is worst?",
    },
    {
      title: "Any stale sources?",
      label: "No traffic in 30 days",
      icon: "search",
      prompt: "Are any sources unused in the last 30 days?",
    },
  ],

  catalog: [
    {
      title: "What's configured?",
      label: "What's configured",
      icon: "server",
      prompt:
        "Summarize the MCP servers and tools already configured in this project.",
    },
    {
      title: "What should I add?",
      label: "Based on usage",
      icon: "sparkles",
      prompt:
        "What kinds of tools does my team call most? Help me decide what to add next from the catalog.",
    },
    {
      title: "Any tool gaps?",
      label: "Where agents struggle",
      icon: "search",
      prompt:
        "Do recent agent sessions show failures or dead ends that a missing tool could explain?",
    },
  ],

  assistants: [
    {
      title: "What are assistants asked?",
      label: "Recent conversations",
      icon: "bot",
      prompt:
        "Summarize recent conversations with project assistants. What are people asking about?",
    },
    {
      title: "Which tools do they use?",
      label: "Tools assistants call",
      icon: "wrench",
      prompt: "Which tools do assistants invoke most often?",
    },
    {
      title: "Any failed conversations?",
      label: "Conversations that errored",
      icon: "alert",
      prompt:
        "Find assistant conversations that hit errors and summarize the failure patterns.",
    },
  ],

  clis: [
    {
      title: "Which skills are used?",
      label: "Most-invoked skills",
      icon: "terminal",
      prompt: "Which skills are being invoked most often, and by whom?",
    },
    {
      title: "Any skill failures?",
      label: "Recent skill errors",
      icon: "alert",
      prompt: "Have any skill invocations failed recently? Summarize why.",
    },
    {
      title: "Is adoption growing?",
      label: "Skill adoption trend",
      icon: "trend",
      prompt: "How is skill adoption trending across the team?",
    },
  ],

  mcp: [
    {
      title: "Which servers are busiest?",
      label: "Calls by server",
      icon: "server",
      prompt: "Which MCP servers get the most traffic, and from which clients?",
    },
    {
      title: "Any server errors?",
      label: "Errors by server",
      icon: "alert",
      prompt: "Summarize recent errors across my MCP servers.",
    },
    {
      title: "Token usage by server?",
      label: "Tokens by server",
      icon: "coins",
      prompt: "Which MCP servers consume the most tokens?",
    },
  ],

  environments: [
    {
      title: "Which environments are used?",
      label: "Calls by environment",
      icon: "blocks",
      prompt: "Which environments are tool calls running against recently?",
    },
    {
      title: "Any config issues?",
      label: "Missing variables?",
      icon: "alert",
      prompt:
        "Are there tool failures that look like missing or invalid environment variables?",
    },
    {
      title: "How do they compare?",
      label: "Error rates side by side",
      icon: "gauge",
      prompt: "Compare recent error rates between environments.",
    },
  ],

  triggers: [
    {
      title: "Did any triggers fail?",
      label: "Recent executions",
      icon: "zap",
      prompt: "Summarize recent trigger executions. Did any fail?",
    },
    {
      title: "Which fire most?",
      label: "Most frequent triggers",
      icon: "trend",
      prompt: "Which triggers fire most often, and is that volume expected?",
    },
    {
      title: "Any slow runs?",
      label: "Slow trigger runs",
      icon: "gauge",
      prompt: "Are any triggers running slower than usual?",
    },
  ],

  "insights/costs": [
    {
      title: "What are we spending?",
      label: "Summarize costs",
      icon: "coins",
      prompt:
        "Summarize AI agent costs across all users, broken down by client type and model.",
    },
    {
      title: "Who spends most?",
      label: "Who spends most?",
      icon: "users",
      prompt:
        "Which users have the highest token usage and cost? Show a breakdown.",
    },
    {
      title: "Cost by model?",
      label: "Cost by model",
      icon: "trend",
      prompt:
        "Break down token usage and cost by model. Which models are most expensive?",
    },
    {
      title: "Which clients are popular?",
      label: "Compare clients",
      icon: "bot",
      prompt:
        "Compare usage across different AI coding clients (Claude Code, Cursor, etc). Which is most popular?",
    },
  ],

  "insights/tools": [
    {
      title: "How is usage trending?",
      label: "Top tools & users",
      icon: "trend",
      prompt:
        "Summarize tool usage for this period: top tools, top users, and how the volume is trending.",
    },
    {
      title: "Which tools fail most?",
      label: "Highest failure rates",
      icon: "alert",
      prompt:
        "Which tools have the highest failure rates right now, and what do the errors look like?",
    },
    {
      title: "Slowest tools?",
      label: "Slowest tools",
      icon: "gauge",
      prompt: "Find tools with the slowest p95 latency in this period.",
    },
  ],

  "insights/mcp": [
    {
      title: "Which tools fail most?",
      label: "Analyze failing tools",
      icon: "alert",
      prompt:
        "Which tools have the highest failure rates? What might be causing the failures?",
    },
    {
      title: "How are metrics trending?",
      label: "Analyze trends",
      icon: "trend",
      prompt:
        "What trends do you see in the metrics? Are things improving or declining?",
    },
  ],

  "insights/employees": [
    {
      title: "Who is enrolled?",
      label: "Who is enrolled?",
      icon: "users",
      prompt:
        "Using the Employees tab context, summarize who is enrolled in this project based on whether they have any platform token usage.",
    },
    {
      title: "Who isn't enrolled?",
      label: "Who is not enrolled?",
      icon: "alert",
      prompt:
        "Which employees are not enrolled because they have no platform token usage in this project?",
    },
    {
      title: "How is enrollment going?",
      label: "Summarize enrollment",
      icon: "trend",
      prompt:
        "Summarize project employee enrollment based on whether each employee has platform token usage.",
    },
    {
      title: "What's each user's usage?",
      label: "Show user usage",
      icon: "coins",
      prompt:
        "Show me a table of organization users' platform usage for the last 30 days, including token counts, last activity, and hook source breakdowns.",
    },
  ],

  "insights/employees/:userSlug": (
    displayName: string,
    displayEmail: string,
    rangeLabel: string,
  ) => [
    {
      title: "What's their usage?",
      label: "Summarize usage",
      icon: "coins" as const,
      prompt: `Summarize the token and tool usage for ${displayName} (${displayEmail}) over ${rangeLabel}.`,
    },
    {
      title: "Which platforms do they use?",
      label: "Show platforms",
      icon: "bot" as const,
      prompt: `What platforms has ${displayName} been using?`,
    },
  ],

  "logs/tools": [
    {
      title: "What failed recently?",
      label: "Group failed tool calls",
      icon: "alert",
      prompt:
        "Show me failed tool calls from the current period and group them by error type.",
    },
    {
      title: "Trace a failing call?",
      label: "Follow one call end to end",
      icon: "search",
      prompt:
        "Help me trace a failing tool call end to end — find a recent failure and walk through what happened.",
    },
    {
      title: "Was there a spike?",
      label: "When did it start?",
      icon: "trend",
      prompt:
        "Was there an error spike in this period? When did it start and which tools were involved?",
    },
  ],

  "logs/mcp": [
    {
      title: "Which calls are failing?",
      label: "Summarize failing tool calls",
      icon: "alert",
      prompt: "Summarize failing tool calls",
    },
    {
      title: "Chart top tool calls?",
      label: "Plot tool call counts",
      icon: "chart",
      prompt: "Plot a chart of the top tool calls and their counts",
    },
    {
      title: "Any recent errors?",
      label: "Find recent errors",
      icon: "search",
      prompt: "Search for recent error logs and summarize what's happening",
    },
  ],

  "logs/risk-events": [
    {
      title: "What risks were found?",
      label: "Findings by category",
      icon: "shield",
      prompt:
        "Summarize recent risk events grouped by category and severity. Never quote redacted match content.",
    },
    {
      title: "Who triggers most events?",
      label: "Users & sessions",
      icon: "users",
      prompt:
        "Which users or agent sessions generated the most risk events recently?",
    },
    {
      title: "Any new patterns?",
      label: "Anything new this week?",
      icon: "search",
      prompt:
        "Are there kinds of risk findings appearing this week that weren't present before?",
    },
  ],

  "agent-sessions": [
    {
      title: "Which chats failed?",
      label: "Analyze failed chats",
      icon: "alert",
      prompt:
        "Show me recent agent sessions that failed. What patterns do you see in the failures?",
    },
    {
      title: "Search the logs?",
      label: "Search raw logs",
      icon: "search",
      prompt:
        "Search the raw telemetry logs for errors or warnings in the current period",
    },
    {
      title: "Debug a session?",
      label: "Debug a specific chat",
      icon: "chat",
      prompt:
        "Help me debug an agent session. Search both the chat data and raw logs to understand what happened.",
    },
  ],

  "risk-overview": [
    {
      title: "Top firing rules?",
      label: "which rule_ids fired most",
      icon: "shield",
      prompt:
        "What are the top 5 rules by finding count over the last 7 days? Report by source family and rule only, and never quote any redacted match content.",
    },
    {
      title: "Any shadow MCPs?",
      label: "unapproved MCPs in use",
      icon: "server",
      prompt:
        "List all shadow_mcp findings across the project. For each, name the MCP server identifier (match), the chat_id, and when it was first observed. These match values are server URLs/commands and are safe to name.",
    },
    {
      title: "How many leaked secrets?",
      label: "dedupe by fingerprint",
      icon: "key",
      prompt:
        "How many distinct leaked secrets are there? Identical secrets share a redacted fingerprint, so dedupe by that. Group the counts by rule, and do not print any redacted match content back to me.",
    },
    {
      title: "Any analysis backlog?",
      label: "pending messages per policy",
      icon: "history",
      prompt:
        "Is there an analysis backlog? For each active policy, report pending vs analyzed message counts and workflow state, and flag any policy whose pending count is non-zero.",
    },
  ],

  "detection-rules": [
    {
      title: "Which rules fire most?",
      label: "Which rules fire most",
      icon: "shield",
      prompt:
        "Which detection rules fire most often, and are any rules completely silent?",
    },
    {
      title: "Any noisy rules?",
      label: "Possible false positives",
      icon: "alert",
      prompt:
        "Are any rules generating excessive findings that look like false positives?",
    },
    {
      title: "Any coverage gaps?",
      label: "What's not covered",
      icon: "search",
      prompt:
        "Based on recent risk findings, where might detection coverage be missing?",
    },
  ],

  "approval-requests": [
    {
      title: "What's pending?",
      label: "What's waiting",
      icon: "history",
      prompt: "Summarize pending approval requests. Which are oldest?",
    },
    {
      title: "What triggers approvals?",
      label: "What triggers approvals",
      icon: "search",
      prompt: "What kinds of tool calls trigger approval requests most often?",
    },
    {
      title: "Are approvals slow?",
      label: "How long requests wait",
      icon: "gauge",
      prompt:
        "Are approvals slowing anyone down? Look at how long requests wait before a decision.",
    },
  ],

  "risk-policies": [
    {
      title: "Are policies healthy?",
      label: "what's running and what's stuck",
      icon: "shield",
      prompt:
        "Are the risk policies healthy? For each one, report whether it's enabled, its action (flag vs block), total and pending message counts, and workflow state. Flag any policy with non-zero pending messages.",
    },
    {
      title: "Any quiet policies?",
      label: "policies with no recent findings",
      icon: "search",
      prompt:
        "Which policies have not produced any findings in the last 30 days? Report them by name with their last-seen finding date.",
    },
    {
      title: "What's each source catching?",
      label: "what's each source catching",
      icon: "trend",
      prompt:
        "What is each detection source catching? Group findings by source (gitleaks, presidio, prompt_injection, shadow_mcp, destructive_tool) over the last 7 days, and report counts with the top rule per source family.",
    },
    {
      title: "What detectors exist?",
      label: "what detectors are available",
      icon: "sparkles",
      prompt:
        "Which detection backends are configured on this server (e.g. the prompt-injection ML classifier)?",
    },
  ],

  sdks: [
    {
      title: "What's in the SDK?",
      label: "What's exposed",
      icon: "server",
      prompt:
        "Summarize the toolsets and tools exposed to SDK consumers in this project.",
    },
    {
      title: "SDK vs MCP traffic?",
      label: "Calls by client",
      icon: "trend",
      prompt:
        "How much traffic comes from SDK or API clients versus MCP clients?",
    },
    {
      title: "Any SDK errors?",
      label: "Failing SDK calls",
      icon: "alert",
      prompt: "Summarize recent errors from SDK or API tool calls.",
    },
  ],

  deployments: [
    {
      title: "What changed last deploy?",
      label: "What changed",
      icon: "rocket",
      prompt:
        "Summarize the most recent deployment: which tools were added, removed, or changed?",
    },
    {
      title: "Did errors increase?",
      label: "Errors before vs after",
      icon: "alert",
      prompt:
        "Compare error rates before and after the latest deployment. Did anything regress?",
    },
    {
      title: "Recent deployments?",
      label: "Recent deployments",
      icon: "history",
      prompt: "Summarize recent deployments and whether each succeeded.",
    },
  ],

  plugins: [
    {
      title: "Which plugins are active?",
      label: "Calls by plugin",
      icon: "puzzle",
      prompt: "Which plugins are active and how often are their tools called?",
    },
    {
      title: "Any plugin errors?",
      label: "Failing plugins",
      icon: "alert",
      prompt: "Summarize recent errors from plugin tools.",
    },
    {
      title: "Any unused plugins?",
      label: "No traffic in 30 days",
      icon: "search",
      prompt: "Are any plugins unused in the last 30 days?",
    },
  ],

  settings: [
    {
      title: "How is this set up?",
      label: "Configuration & activity",
      icon: "settings",
      prompt:
        "Give me an overview of this project's configuration and recent activity.",
    },
    {
      title: "Anything unused?",
      label: "What's unused",
      icon: "search",
      prompt:
        "Is there anything unused in this project — toolsets, environments, or tools with no traffic?",
    },
    {
      title: "Anything broken?",
      label: "Anything to look into?",
      icon: "alert",
      prompt:
        "Are there any error spikes or failing tools I should look into before changing settings?",
    },
  ],

  /** Org-level audit logs page (own InsightsProvider, outside project routes). */
  "org/audit-logs": [
    {
      title: "What changed recently?",
      label: "What changed recently?",
      icon: "history",
      prompt:
        "Summarize the most significant recent changes across the organization based on the audit logs.",
    },
    {
      title: "Any security events?",
      label: "Security-relevant events",
      icon: "shield",
      prompt:
        "What security-relevant events have occurred recently? Look for API key changes, permission modifications, or unusual patterns.",
    },
    {
      title: "Who's most active?",
      label: "Most active team members",
      icon: "users",
      prompt:
        "Who have been the most active users recently and what kinds of changes have they been making?",
    },
  ],
} satisfies Record<string, SuggestionEntry>;

/**
 * Resolve the route-level suggestions for a project-scoped pathname
 * (`/:orgSlug/projects/:projectSlug/...`). Walks segments longest-first so
 * deeper entries (e.g. `"logs/risk-events"`) beat their parents, and detail
 * pages (`/mcp/x/foo/overview`) inherit from their section (`"mcp"`).
 * Function-valued (parameterized) entries are skipped — those need page data
 * and reach the provider via <InsightsConfig> instead. Returns undefined for
 * non-project paths and unknown sections.
 */
export function getRouteSuggestions(
  pathname: string,
): InsightsSuggestion[] | undefined {
  const segments = pathname.split("/").filter(Boolean);
  if (segments[1] !== "projects") return undefined;
  const rest = segments.slice(3);

  if (rest.length === 0) {
    return INSIGHTS_SUGGESTIONS[""];
  }
  for (let depth = rest.length; depth >= 1; depth--) {
    const key = rest.slice(0, depth).join("/");
    const entry =
      INSIGHTS_SUGGESTIONS[key as keyof typeof INSIGHTS_SUGGESTIONS];
    if (Array.isArray(entry)) return entry;
  }
  return undefined;
}
