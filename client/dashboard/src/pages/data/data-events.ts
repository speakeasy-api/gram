/**
 * Prototype data model for the Data page.
 *
 * Mirrors the canonical telemetry event identity stamped on telemetry_logs
 * rows (urn:telemetry:<origin>:<kind>:<type>): origin is the observation
 * channel, kind is the OTel signal shape, and type is the producer's own
 * event type. The page is a UI prototype, so everything here is a static
 * fixture — no backend queries yet.
 */

export const EVENT_ORIGINS = [
  "provider_otel",
  "provider_api",
  "agent_hook",
  "gram_service",
  "unknown",
] as const;

export type DataEventOrigin = (typeof EVENT_ORIGINS)[number];

export const EVENT_KINDS = ["log", "metric"] as const;

export type DataEventKind = (typeof EVENT_KINDS)[number];

export interface DataEvent {
  id: string;
  timestamp: Date;
  /**
   * Project the event landed in. The page is org-scoped: the feed spans
   * every project in the organization.
   */
  project: string;
  origin: DataEventOrigin;
  kind: DataEventKind;
  /** URN type segment: the producer's event type, sanitized and lowercased. */
  type: string;
  /** Producing agent or service (gram.hook.source / service.name). */
  producer: string;
  /** Log body or a human summary of the record. */
  body: string;
  attributes: Record<string, string | number | boolean>;
}

export function eventUrn(event: DataEvent): string {
  return `urn:telemetry:${event.origin}:${event.kind}:${event.type}`;
}

export const ORIGIN_LABELS: Record<DataEventOrigin, string> = {
  provider_otel: "Provider OTel",
  provider_api: "Provider API",
  agent_hook: "Agent Hook",
  gram_service: "Gram Service",
  unknown: "Unknown",
};

/** Projects the mock feed spans; the real page queries every org project. */
export const MOCK_PROJECTS = ["default", "internal-tools", "platform"] as const;

// ---------------------------------------------------------------------------
// Data quality
// ---------------------------------------------------------------------------

export interface QualityCheck {
  /** Attribute key the event class is expected to carry. */
  key: string;
  label: string;
  present: boolean;
}

export type QualityGrade = "complete" | "partial" | "unclassified";

export interface EventQuality {
  grade: QualityGrade;
  checks: QualityCheck[];
  missing: QualityCheck[];
}

interface ExpectedAttribute {
  key: string;
  label: string;
}

/**
 * The attributes each event class is expected to arrive with. Keyed by
 * `<origin>:<kind>` — the physical layout of the event, which is exactly what
 * this page lets you narrow by. In the real implementation this contract
 * comes from the capability registry; here it is a fixture.
 */
const EXPECTED_ATTRIBUTES: Record<string, ExpectedAttribute[]> = {
  "provider_otel:log": [
    { key: "event.name", label: "Producer event name" },
    { key: "session.id", label: "Session / conversation ID" },
    { key: "service.version", label: "Producer version" },
  ],
  "provider_otel:metric": [
    { key: "gen_ai.usage.input_tokens", label: "Input tokens" },
    { key: "gen_ai.usage.output_tokens", label: "Output tokens" },
    { key: "gen_ai.usage.cost_usd", label: "Cost (USD)" },
    { key: "gen_ai.request.model", label: "Model" },
    { key: "session.id", label: "Session / conversation ID" },
  ],
  "provider_api:metric": [
    { key: "gen_ai.usage.input_tokens", label: "Input tokens" },
    { key: "gen_ai.usage.output_tokens", label: "Output tokens" },
    { key: "gen_ai.request.model", label: "Model" },
    { key: "user.email", label: "User identity" },
  ],
  "agent_hook:log": [
    { key: "gram.hook.event", label: "Hook event name" },
    { key: "gram.hook.source", label: "Hook source agent" },
    { key: "conversation.id", label: "Conversation ID" },
  ],
  "agent_hook:metric": [
    { key: "gen_ai.usage.input_tokens", label: "Input tokens" },
    { key: "gen_ai.usage.output_tokens", label: "Output tokens" },
    { key: "gen_ai.request.model", label: "Model" },
    { key: "conversation.id", label: "Conversation ID" },
  ],
  "gram_service:log": [
    { key: "gram.event.source", label: "Gram event source" },
    { key: "trace.id", label: "Trace ID" },
  ],
};

export function evaluateQuality(event: DataEvent): EventQuality {
  const expected = EXPECTED_ATTRIBUTES[`${event.origin}:${event.kind}`] ?? [];
  const checks = expected.map((attr) => ({
    key: attr.key,
    label: attr.label,
    present: event.attributes[attr.key] !== undefined,
  }));
  const missing = checks.filter((check) => !check.present);

  // A row nothing could classify (unknown origin, or a producer that sent no
  // usable event type) is a data-quality problem regardless of which
  // attributes it carries — surface it as its own grade.
  if (event.origin === "unknown" || event.type === "unknown") {
    return { grade: "unclassified", checks, missing };
  }

  return {
    grade: missing.length === 0 ? "complete" : "partial",
    checks,
    missing,
  };
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

interface FixtureSpec extends Omit<DataEvent, "id" | "timestamp"> {
  /** Minutes before "now" the event was observed. */
  minutesAgo: number;
}

const FIXTURES: FixtureSpec[] = [
  {
    minutesAgo: 1,
    project: "default",
    origin: "gram_service",
    kind: "log",
    type: "tool_call",
    producer: "gram-server",
    body: "POST tools:http:petstore:list_pets 200",
    attributes: {
      "gram.event.source": "tool_call",
      "gram.tool.urn": "tools:http:petstore:list_pets",
      "http.request.method": "POST",
      "http.response.status_code": 200,
      "trace.id": "6f2a9c01d4e88b3a51c0aa8f9e2d7b41",
      "gram.duration_ms": 412,
    },
  },
  {
    minutesAgo: 2,
    project: "default",
    origin: "provider_otel",
    kind: "log",
    type: "api_request",
    producer: "claude-code",
    body: "claude_code.api_request",
    attributes: {
      "event.name": "api_request",
      "session.id": "0aa7c3f1-83b4-4e6e-9a2f-6a51c22e8d10",
      request_id: "req_9Zk3mWfT",
      "gen_ai.request.model": "claude-sonnet-4-5",
      "service.version": "2.1.19",
    },
  },
  {
    minutesAgo: 2,
    project: "default",
    origin: "provider_otel",
    kind: "metric",
    type: "usage",
    producer: "claude-code",
    body: "claude_code.token.usage",
    attributes: {
      "gen_ai.usage.input_tokens": 1843,
      "gen_ai.usage.output_tokens": 592,
      "gen_ai.usage.cache_read_tokens": 12480,
      "gen_ai.usage.cost_usd": 0.0231,
      "gen_ai.request.model": "claude-sonnet-4-5",
      "session.id": "0aa7c3f1-83b4-4e6e-9a2f-6a51c22e8d10",
      "service.version": "2.1.19",
    },
  },
  {
    minutesAgo: 4,
    project: "internal-tools",
    origin: "agent_hook",
    kind: "log",
    type: "pretooluse",
    producer: "cursor",
    body: "preToolUse: shell (allowed)",
    attributes: {
      "gram.hook.event": "preToolUse",
      "gram.hook.source": "cursor",
      "gram.hook.decision": "allow",
      "gram.tool.name": "shell",
      "conversation.id": "c7e6c1a2-0d31-45c2-8a75-3f0d19b3a144",
      cursor_version: "2.4.0",
    },
  },
  {
    minutesAgo: 6,
    project: "internal-tools",
    origin: "agent_hook",
    kind: "metric",
    type: "usage",
    producer: "cursor",
    body: "afterAgentResponse token usage",
    attributes: {
      "gram.hook.source": "cursor",
      "gen_ai.usage.input_tokens": 9210,
      "gen_ai.usage.output_tokens": 1310,
      "conversation.id": "c7e6c1a2-0d31-45c2-8a75-3f0d19b3a144",
      // Missing gen_ai.request.model: Cursor's afterAgentResponse hook does
      // not always report which model produced the response.
    },
  },
  {
    minutesAgo: 9,
    project: "platform",
    origin: "unknown",
    kind: "log",
    type: "agent_heartbeat",
    producer: "unknown",
    body: '{"kind":"heartbeat","agent":"acme-internal-agent","v":"0.3"}',
    attributes: {
      "log.severity": "INFO",
      // No gram.event.source, no hook attributes: nothing could classify
      // this row, so it landed with origin unknown. Kept visible on purpose.
    },
  },
  {
    minutesAgo: 11,
    project: "platform",
    origin: "gram_service",
    kind: "log",
    type: "chat_completion",
    producer: "gram-server",
    body: "chat completion via openrouter",
    attributes: {
      "gram.event.source": "chat_completion",
      "gen_ai.request.model": "openai/gpt-5.2",
      "gen_ai.usage.input_tokens": 2380,
      "gen_ai.usage.output_tokens": 411,
      "gen_ai.response.finish_reason": "stop",
      "gen_ai.conversation.id": "f19b2c7d-5566-4a01-8d9e-b4a20cd0f234",
      "trace.id": "b81f3ce09a2d47f6a1e5dd0c2b9e8a63",
    },
  },
  {
    minutesAgo: 13,
    project: "default",
    origin: "agent_hook",
    kind: "log",
    type: "pretooluse",
    producer: "claude-code",
    body: "PreToolUse: Bash (allowed)",
    attributes: {
      "gram.hook.event": "PreToolUse",
      "gram.hook.source": "claude-code",
      "gram.hook.decision": "allow",
      "gram.tool.name": "Bash",
      // Missing conversation.id: older plugin versions did not forward the
      // session identifier on hook events.
    },
  },
  {
    minutesAgo: 16,
    project: "internal-tools",
    origin: "provider_api",
    kind: "metric",
    type: "usage",
    producer: "cursor-admin-api",
    body: "cursor usage event",
    attributes: {
      "gen_ai.usage.input_tokens": 51240,
      "gen_ai.usage.output_tokens": 8113,
      "gen_ai.request.model": "composer-2.5",
      "user.email": "ada@example.com",
      "gram.billing.total_cents": 84,
      "gram.billing.charged_cents": 101,
    },
  },
  {
    minutesAgo: 21,
    project: "default",
    origin: "provider_otel",
    kind: "log",
    type: "tool_result",
    producer: "claude-code",
    body: "claude_code.tool_result",
    attributes: {
      "event.name": "tool_result",
      "session.id": "0aa7c3f1-83b4-4e6e-9a2f-6a51c22e8d10",
      tool_use_id: "toolu_01WqXk8mNv",
      "gram.tool.name": "Read",
      "service.version": "2.1.19",
    },
  },
  {
    minutesAgo: 26,
    project: "default",
    origin: "provider_otel",
    kind: "log",
    type: "unknown",
    producer: "claude-code",
    body: "api request",
    attributes: {
      "session.id": "77b1d0c9-4f2e-4b7c-b7aa-90c1e22d5f88",
      // No event.name and no recognizable body prefix: the producer's type
      // could not be determined, so the URN type segment is "unknown".
    },
  },
  {
    minutesAgo: 31,
    project: "platform",
    origin: "gram_service",
    kind: "log",
    type: "chat_resolution",
    producer: "gram-worker",
    body: "chat resolution evaluation",
    attributes: {
      "gram.event.source": "evaluation",
      "gen_ai.evaluation.name": "chat_resolution",
      "gen_ai.evaluation.score.value": 0.86,
      "gen_ai.evaluation.score.label": "resolved",
      "gen_ai.conversation.id": "f19b2c7d-5566-4a01-8d9e-b4a20cd0f234",
      // Missing trace.id: evaluations run detached from the original
      // request trace.
    },
  },
  {
    minutesAgo: 35,
    project: "default",
    origin: "agent_hook",
    kind: "log",
    type: "skill.activated",
    producer: "claude-code",
    body: "skill activated: postgresql",
    attributes: {
      "gram.hook.event": "skill.activated",
      "gram.hook.source": "claude-code",
      "gram.skill.name": "postgresql",
      "conversation.id": "5cd90ab1-77e3-4f19-8c30-2e88f1d0a9b2",
    },
  },
  {
    minutesAgo: 42,
    project: "internal-tools",
    origin: "provider_otel",
    kind: "metric",
    type: "usage",
    producer: "codex",
    body: "codex.sse_event response.completed",
    attributes: {
      "gen_ai.usage.input_tokens": 6120,
      "gen_ai.usage.output_tokens": 902,
      "gen_ai.usage.reasoning_tokens": 344,
      "gen_ai.request.model": "gpt-5.3-codex",
      "session.id": "conv_88f1a0c2b3d4",
      // Missing gen_ai.usage.cost_usd: Codex OTel reports tokens but never
      // USD cost, so cost stays an estimate downstream.
    },
  },
  {
    minutesAgo: 47,
    project: "default",
    origin: "gram_service",
    kind: "log",
    type: "resource_read",
    producer: "gram-server",
    body: "GET resources:function:docs:readme 200",
    attributes: {
      "gram.event.source": "resource_read",
      "gram.resource.urn": "resources:function:docs:readme",
      "http.response.status_code": 200,
      "trace.id": "3d4e88b3a51c0aa8f9e2d7b416f2a9c0",
      "gram.duration_ms": 58,
    },
  },
  {
    minutesAgo: 53,
    project: "default",
    origin: "provider_otel",
    kind: "log",
    type: "user_prompt",
    producer: "claude-code",
    body: "claude_code.user_prompt",
    attributes: {
      "event.name": "user_prompt",
      "session.id": "77b1d0c9-4f2e-4b7c-b7aa-90c1e22d5f88",
      "prompt.id": "prompt_4XkQ92",
      "service.version": "2.0.44",
    },
  },
  {
    minutesAgo: 58,
    project: "internal-tools",
    origin: "agent_hook",
    kind: "log",
    type: "afteragentresponse",
    producer: "cursor",
    body: "afterAgentResponse",
    attributes: {
      "gram.hook.event": "afterAgentResponse",
      "gram.hook.source": "cursor",
      "conversation.id": "c7e6c1a2-0d31-45c2-8a75-3f0d19b3a144",
      cursor_version: "2.4.0",
    },
  },
];

/**
 * Builds the fixture feed with timestamps relative to now so the page always
 * looks freshly ingested. Sorted reverse-chronologically, newest first.
 */
export function buildMockEvents(now: Date = new Date()): DataEvent[] {
  return FIXTURES.map((fixture, index) => {
    const { minutesAgo, ...event } = fixture;
    return {
      ...event,
      id: `evt_${String(index + 1).padStart(3, "0")}`,
      timestamp: new Date(now.getTime() - minutesAgo * 60_000),
    };
  }).sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime());
}
