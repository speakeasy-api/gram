import { formatChartLabel, unixNanoToMs } from "@/components/chart/chartUtils";
import {
  ACCENT_RED,
  GOOD_GREEN,
  OTHER_SERIES,
  SERIES,
  SEVERITY,
} from "@/components/chart/palette";
import type { TimeSeriesDataset } from "@/components/observe/toolUsageTimeSeriesChartData";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import type { LogFilter } from "@gram/client/models/components/logfilter.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionKey } from "@gram/client/models/components/gatewayconnectionkey.js";
import type { GatewayConnectionOutcomeCount } from "@gram/client/models/components/gatewayconnectionoutcomecount.js";
import type { ListGatewayConnectionHotspotsPayloadStage } from "@gram/client/models/components/listgatewayconnectionhotspotspayload.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import { withIncludedValues } from "./sessionFilters";

export type HealthStage = ListGatewayConnectionHotspotsPayloadStage;
export type HealthWindow = "24h" | "7d" | "30d";

export const HEALTH_WINDOWS: { value: HealthWindow; label: string }[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
];

export const WINDOW_PHRASE: Record<HealthWindow, string> = {
  "24h": "in the last 24 hours",
  "7d": "in the last 7 days",
  "30d": "in the last 30 days",
};

const WINDOW_MS: Record<HealthWindow, number> = {
  "24h": 24 * 3_600_000,
  "7d": 7 * 24 * 3_600_000,
  "30d": 30 * 24 * 3_600_000,
};

// In the order a connection meets them; Listings is the default.
export const STAGE_OPTIONS: { value: HealthStage; label: string }[] = [
  { value: "inbound_authorize", label: "Sign-in requests" },
  { value: "inbound_token", label: "Token requests" },
  { value: "discovery_listing", label: "Listings" },
  { value: "discovery_member", label: "Member checks" },
];

const ATTEMPT_EVENT_URN =
  "urn:telemetry:gram_service:log:mcp_connection_attempt";

/**
 * One window, whole milliseconds, shared by every query in the section so
 * counts, trend, hotspots, and evidence all describe the same [from, to).
 */
export function healthWindow(
  window: HealthWindow,
  now: Date,
): { from: Date; to: Date } {
  const to = new Date(now.getTime());
  return { from: new Date(to.getTime() - WINDOW_MS[window]), to };
}

type OutcomeStyle = { outcome: string; label: string; color: string };

const POLICY_BLOCKED: OutcomeStyle = {
  outcome: "policy_blocked",
  label: "Blocked by policy",
  color: SEVERITY.medium,
};
const FAILED: OutcomeStyle = {
  outcome: "failed",
  label: "Failed",
  color: ACCENT_RED,
};
const UNKNOWN: OutcomeStyle = {
  outcome: "unknown",
  label: "Outcome not recorded",
  color: OTHER_SERIES,
};

const OUTCOMES: Record<HealthStage, OutcomeStyle[]> = {
  inbound_authorize: [
    { outcome: "accepted", label: "Sent to sign-in", color: GOOD_GREEN },
    POLICY_BLOCKED,
    FAILED,
    UNKNOWN,
  ],
  inbound_token: [
    {
      outcome: "code_issued",
      label: "Sign-in token written",
      color: GOOD_GREEN,
    },
    { outcome: "refreshed", label: "Refresh answered", color: SERIES[0]! },
    {
      outcome: "refresh_replayed",
      label: "Refresh answered again",
      color: SERIES[3]!,
    },
    {
      outcome: "assertion_issued",
      label: "Enterprise assertion answered",
      color: SERIES[5]!,
    },
    POLICY_BLOCKED,
    FAILED,
    UNKNOWN,
  ],
  discovery_listing: [
    { outcome: "complete", label: "Complete", color: GOOD_GREEN },
    { outcome: "partial", label: "Partial", color: SEVERITY.medium },
    { outcome: "failed", label: "Failed", color: ACCENT_RED },
  ],
  discovery_member: [
    { outcome: "listed", label: "Listed", color: GOOD_GREEN },
    { outcome: "failed", label: "Failed", color: ACCENT_RED },
    {
      outcome: "credential_blocked",
      label: "Needs sign-in",
      color: SEVERITY.medium,
    },
    {
      outcome: "consent_blocked",
      label: "Needs consent",
      color: SEVERITY.medium,
    },
  ],
};

function outcomeStyles(stage: string): OutcomeStyle[] {
  return OUTCOMES[stage as HealthStage] ?? [];
}

export function countOf(
  outcomes: GatewayConnectionOutcomeCount[],
  outcome: string,
): number {
  return outcomes.find((count) => count.outcome === outcome)?.count ?? 0;
}

export type StageTile = {
  title: string;
  value: number;
  tone: "neutral" | "information" | "destructive" | "success" | "warning";
  subtext?: string;
  tooltip?: string;
};

/**
 * Tiles for one stage. Every count names the stage's attempts as its
 * denominator, and blocked members stay in it rather than reading as failed.
 */
export function stageTiles(stage: GatewayConnectionStage): StageTile[] {
  const total = stage.attempts.toLocaleString();
  const of = (count: number, tone: StageTile["tone"]): StageTile["tone"] =>
    count > 0 ? tone : "neutral";
  const blockedByPolicy = countOf(stage.outcomes, "policy_blocked");
  const policyTile: StageTile = {
    title: "Blocked by policy",
    value: blockedByPolicy,
    tone: of(blockedByPolicy, "warning"),
    subtext: `of ${total}`,
    tooltip:
      "Refused by an administrator's client metadata, blocked-tool, or workload policy. Counted apart from failures.",
  };
  if (stage.stage === "inbound_authorize") {
    const failed = countOf(stage.outcomes, "failed");
    return [
      {
        title: "Sign-in requests",
        value: stage.attempts,
        tone: "information",
      },
      {
        title: "Sent to sign-in",
        value: countOf(stage.outcomes, "accepted"),
        tone: "neutral",
        subtext: `of ${total}`,
        tooltip:
          "The gateway attempted a redirect to sign in. Whether it arrived and whether sign-in finished are not observed.",
      },
      policyTile,
      {
        title: "Failed",
        value: failed,
        tone: of(failed, "destructive"),
        subtext: `of ${total}`,
      },
    ];
  }
  if (stage.stage === "inbound_token") {
    const failed = countOf(stage.outcomes, "failed");
    return [
      { title: "Token requests", value: stage.attempts, tone: "information" },
      {
        title: "Sign-in tokens written",
        value: countOf(stage.outcomes, "code_issued"),
        tone: "neutral",
        subtext: `of ${total}`,
        tooltip:
          "Authorization-code requests the gateway answered with a token. It does not show that a client used the token.",
      },
      {
        title: "Refreshes answered",
        value:
          countOf(stage.outcomes, "refreshed") +
          countOf(stage.outcomes, "refresh_replayed"),
        tone: "neutral",
        subtext: `of ${total}`,
        tooltip:
          "Refresh requests the gateway answered, including a repeated refresh answered again. A refresh is never a sign-in.",
      },
      policyTile,
      {
        title: "Failed",
        value: failed,
        tone: of(failed, "destructive"),
        subtext: `of ${total}`,
      },
    ];
  }
  if (stage.stage === "discovery_listing") {
    const complete = countOf(stage.outcomes, "complete");
    const partial = countOf(stage.outcomes, "partial");
    const failed = countOf(stage.outcomes, "failed");
    return [
      { title: "Listings", value: stage.attempts, tone: "information" },
      {
        title: "Complete",
        value: complete,
        tone: "neutral",
        subtext: `of ${total}`,
      },
      {
        title: "Partial",
        value: partial,
        tone: of(partial, "warning"),
        subtext: `of ${total}`,
        tooltip: "Some servers were left out of the client's tool list.",
      },
      {
        title: "Failed",
        value: failed,
        tone: of(failed, "destructive"),
        subtext: `of ${total}`,
      },
    ];
  }
  const failed = countOf(stage.outcomes, "failed");
  const blocked =
    countOf(stage.outcomes, "credential_blocked") +
    countOf(stage.outcomes, "consent_blocked");
  return [
    { title: "Member checks", value: stage.attempts, tone: "information" },
    {
      title: "Failed",
      value: failed,
      tone: of(failed, "destructive"),
      subtext: `of ${total}`,
    },
    {
      title: "Needs sign-in or consent",
      value: blocked,
      tone: of(blocked, "warning"),
      subtext: `of ${total}`,
      tooltip:
        "The person can fix these by signing in or approving the server.",
    },
    {
      title: "Listed after an internal retry",
      value: stage.recovered,
      tone: "neutral",
      subtext: `of ${total}`,
      tooltip:
        "Listed on a retry inside the same listing. This is not a later reconnect.",
    },
  ];
}

export function stageChartData(
  stage: GatewayConnectionStage,
  intervalSeconds: number,
  from: Date,
  to: Date,
): {
  bucketMs: number;
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
} {
  const timestamps = stage.buckets.map((bucket) =>
    unixNanoToMs(bucket.bucketTimeUnixNano),
  );
  return {
    bucketMs: intervalSeconds * 1000,
    timestamps,
    labels: timestamps.map((t) =>
      formatChartLabel(new Date(t), to.getTime() - from.getTime()),
    ),
    tooltipLabels: timestamps.map((t) =>
      new Date(t).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      }),
    ),
    datasets: outcomeStyles(stage.stage).map((style) => ({
      label: style.label,
      data: stage.buckets.map((bucket) =>
        countOf(bucket.outcomes, style.outcome),
      ),
      backgroundColor: style.color,
      hoverBackgroundColor: style.color,
    })),
  };
}

// Every reason the gateway records, in plain words with the next step where
// there is one. A code added later falls back to generic copy.
const REASONS: Record<string, string> = {
  stale_cursor:
    "The client asked for a later page of an earlier listing. Restart tool discovery in the client. If signing in again leaves this error, remove and add the connection in the client.",
  consent_not_approved:
    "A server was added after the person approved this connection. Reconnecting lets them approve it.",
  consent_auth_changed:
    "The server's sign-in changed since approval. Reconnecting lets the person approve it again.",
  consent_config_changed:
    "The server's configuration changed since approval. Reconnecting lets the person approve it again.",
  no_member_listed: "No server in the gateway could be listed.",
  rate_limited: "The server's upstream rate-limited the listing.",
  upstream_unavailable: "The server's upstream was unavailable.",
  upstream_rejected: "The server's upstream rejected the listing.",
  listing_timeout: "The server did not answer in time.",
  listing_budget_exhausted:
    "The gateway ran out of listing time before reaching this server.",
  unreachable: "The server could not be reached.",
  needs_sign_in: "The person has not connected this server.",
  credential_rejected: "The server rejected the stored credential.",
  reauthentication_required:
    "The person needs to sign in to the gateway again.",
  // Sign-in redirects and token requests.
  unobserved_exit: "The request finished without a recorded outcome.",
  response_write_failed:
    "The gateway prepared a response but could not send it; the client may have disconnected.",
  unavailable:
    "A service the gateway relies on was unavailable. Try again shortly.",
  unexpected: "The gateway hit an unexpected error. Try again shortly.",
  oauth_error: "The request was refused.",
  cimd_fetch_failed:
    "The client's metadata document could not be fetched. Check that its address is reachable.",
  cimd_metadata_invalid:
    "The client's metadata document is not valid. Ask the client's publisher to fix it.",
  cimd_admission_denied:
    "This server does not accept the client's metadata document. An administrator can change which clients are allowed.",
  cimd_admission_disabled:
    "This server does not accept client metadata documents. An administrator can turn them on.",
  cimd_denied_disabled:
    "This server does not accept client metadata documents. An administrator can turn them on.",
  cimd_denied_not_listed:
    "The client is not on this server's allowed list. An administrator can add it.",
  cimd_denied_oversized:
    "The client's metadata address is too long to accept. Ask the client's publisher for a shorter one.",
  cimd_denied_unknown_mode:
    "The server's client policy is not readable, so the client was refused. An administrator can reset the policy.",
  ai_tool_blocked:
    "An administrator blocked this app. They can allow it in the blocked tools settings.",
  agent_admission_denied:
    "The agent is no longer allowed to connect. Authorize it again.",
  issuer_untrusted:
    "The workload's identity issuer is not trusted. An administrator can add it.",
  subject_not_admitted:
    "The workload is not allowed to connect. An administrator can admit it.",
  unknown_client_id:
    "The client is not registered with this gateway's sign-in.",
  client_id_names_endpoint:
    "The client's ID is this server's own address. Configure the client with its own ID.",
  redirect_uri_not_registered:
    "The client asked to return to an address it did not register. Reconnect the client.",
  redirect_uri_mismatch:
    "The client returned a different address than it started with. Reconnect the client.",
  redirect_validation_failed:
    "The client's return address could not be used. Reconnect the client.",
  resource_mismatch:
    "The client asked for a different server than this one. Check its configured address.",
  federation_unavailable:
    "Sign-in through the organization's identity provider is unavailable. Contact your administrator.",
  pkce_mismatch:
    "The client's sign-in proof did not match. Sign in again from the client.",
  code_not_found_or_expired:
    "The sign-in code was unknown, used, or expired. Sign in again from the client.",
  code_client_mismatch:
    "The sign-in code belongs to a different client. Sign in again from the client.",
  authorization_expired: "The sign-in has expired. Sign in again.",
  sign_in_revoked: "The sign-in was revoked. Sign in again.",
  refresh_token_expired: "The refresh token has expired. Sign in again.",
  refresh_token_unknown_or_already_used:
    "The refresh token was unknown or already used. Sign in again from the client.",
  refresh_token_client_mismatch:
    "The refresh token belongs to a different client. Sign in again from the client.",
  refresh_token_resource_mismatch:
    "The refresh token was issued for a different server. Sign in again here.",
  upstream_reconnect_required:
    "A connected provider needs to be signed in again before refreshing. Reconnect it.",
  client_secret_mismatch:
    "The client's secret was wrong. Update the client's credentials.",
  invalid_client:
    "The client could not be authenticated. Check its credentials.",
  invalid_grant:
    "The sign-in grant was not valid. Sign in again from the client.",
  unauthorized_client:
    "This client is not allowed to use this kind of request.",
  unsupported_grant_type:
    "The client used a kind of request this gateway does not support.",
  endpoint_not_agent_addressable:
    "Workloads cannot connect to a gateway directly. Connect them to an MCP server.",
  resource_not_agent_addressable:
    "Assistants cannot get tokens for a gateway directly. Connect them to an MCP server.",
  not_found: "Something the request needed was not found.",
  forbidden: "The request was not allowed.",
  response_too_large:
    "The server returned a tool list larger than the gateway accepts. Ask the server's owner to reduce its size.",
  invalid_listing:
    "The server returned tool definitions the gateway could not list. Ask the server's owner to check its tool definitions.",
  incomplete_catalog:
    "The server returned tool definitions the gateway could not list. Ask the server's owner to check its tool definitions.",
  invalid_response:
    "The server returned a response the gateway could not read. Ask the server's owner to check it.",
  authorization_server_unavailable:
    "The server's sign-in provider could not be reached. Try again later.",
  enterprise_authorization_unavailable:
    "Your organization's sign-in for this server could not be reached. Try again later.",
  credential_load_failed:
    "The gateway could not load this server's connection details. Try again shortly.",
  connection_check_failed:
    "The gateway could not check this server's connection. Try again later.",
  unclassified: "The server could not be listed. Try again later.",
  credential_misconfigured:
    "The server's upstream credential is misconfigured or was issued for a different resource. Contact the server's administrator.",
  credential_ambiguous:
    "More than one upstream credential matches this server, so none could be selected. Ask an administrator to review the connection.",
  enterprise_authorization_failed:
    "Your organization's sign-in for this server failed. Contact your administrator.",
  invalid_request:
    "The client sent a request the gateway could not read. Update the client or reconnect it.",
  member_snapshot_failed:
    "The gateway could not load its servers. Try again shortly.",
  ambiguous_tool_name:
    "Two tools in the gateway resolve to the same name, so the gateway could not list them. Rename one of the tools or servers.",
  inventory_too_large:
    "The gateway's combined tool list is too large to send. Remove servers or tools from the gateway.",
  inventory_error:
    "The gateway could not build its tool list. Try again shortly.",
  encoding_failed:
    "The gateway could not send its tool list. Try again shortly.",
  canceled:
    "The listing stopped before it finished. Restart tool discovery in the client.",
  member_check_failed:
    "A server's availability could not be checked, so the list may be missing its tools. Try again shortly.",
};

/** Plain copy for a reason code; the code itself stays technical detail. */
export function reasonText(reason: string): string {
  return REASONS[reason] ?? "Another failure was recorded.";
}

/**
 * The exact key of a hotspot whose person, provider, and client the caller
 * may see; null when any part is hidden, so it cannot be selected.
 */
export function hotspotKey(
  stage: HealthStage,
  hotspot: GatewayConnectionHotspot,
): GatewayConnectionKey | null {
  const { subject, provider, client } = hotspot;
  if (
    subject.key === undefined ||
    provider.key === undefined ||
    client.key === undefined
  ) {
    return null;
  }
  return {
    stage,
    subject: subject.key,
    providerId: provider.key,
    client: client.key,
  };
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Whether a key can be matched to sign-in token responses: only a signed-in
 * caller using a registered client can have redeemed an authorization code.
 */
export function canMatchSignIn(key: GatewayConnectionKey): boolean {
  return key.subject !== "" && UUID.test(key.client);
}

/**
 * Connection filters for a hotspot key. Only exact equivalents are used: the
 * caller, a provider ID, and a registered client ID. An attempt with no
 * provider is not the same as a session with none, and a recorded client kind
 * names no registered client, so those parts narrow nothing and are named.
 */
export function matchingConnectionFilters(key: GatewayConnectionKey): {
  filters: ActiveLogFilter[];
  unmatched: string[];
} {
  if (key.subject === "") {
    return { filters: [], unmatched: ["caller"] };
  }
  let filters = withIncludedValues([], "identity", [key.subject]);
  const unmatched: string[] = [];
  if (UUID.test(key.providerId)) {
    filters = withIncludedValues(filters, "provider", [key.providerId]);
  } else {
    unmatched.push("provider");
  }
  if (UUID.test(key.client)) {
    filters = withIncludedValues(filters, "client", [key.client]);
  } else {
    unmatched.push("client");
  }
  return { filters, unmatched };
}

/**
 * Keeps only events strictly before the window end. Search bounds are
 * millisecond dates with an inclusive end, while events carry nanoseconds, so
 * the end itself is sent and anything at or after it is dropped here.
 */
export function beforeWindowEnd<T extends { timeUnixNano: string }>(
  events: T[],
  to: Date,
): T[] {
  const end = BigInt(to.getTime()) * 1_000_000n;
  return events.filter((event) => BigInt(event.timeUnixNano) < end);
}

export function failureRate(hotspot: GatewayConnectionHotspot): string {
  if (hotspot.attempts === 0) return "—";
  return `${((hotspot.failed / hotspot.attempts) * 100).toFixed(0)}%`;
}

/**
 * Forbidden and not found both mean the caller cannot read health here: logs
 * may be off, or the gateway or its logs are not theirs to read.
 */
export function isUnavailable(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const status = (error as { statusCode?: unknown }).statusCode;
  return status === 403 || status === 404;
}

/** SearchLogs filters selecting exactly one hotspot key's attempts. */
export function attemptFilters(
  gatewayId: string,
  key: GatewayConnectionKey,
): LogFilter[] {
  const eq = (path: string, value: string): LogFilter => ({
    path,
    operator: "eq",
    values: [value],
  });
  return [
    eq("gram.event.urn", ATTEMPT_EVENT_URN),
    eq("gram.meta_mcp_server.id", gatewayId),
    eq("gram.connection_attempt.stage", key.stage),
    eq("gram.connection_attempt.subject", key.subject),
    eq("gram.connection_attempt.provider_id", key.providerId),
    eq("gram.connection_attempt.client_key", key.client),
  ];
}

export type Attempt = {
  id: string;
  // When the request started: outcomes are recorded once it finishes.
  at: Date;
  outcome: string;
  reason: string;
  grantType: string;
};

function attemptField(attributes: unknown, field: string): string {
  if (typeof attributes !== "object" || attributes === null) return "";
  const attrs = attributes as Record<string, unknown>;
  const flat = attrs[`gram.connection_attempt.${field}`];
  if (typeof flat === "string") return flat;
  const gram = attrs.gram as Record<string, unknown> | undefined;
  const attempt = gram?.connection_attempt as
    | Record<string, unknown>
    | undefined;
  const nested = attempt?.[field];
  return typeof nested === "string" ? nested : "";
}

export function toAttempt(log: TelemetryLogRecord): Attempt {
  return {
    id: log.id,
    at: new Date(unixNanoToMs(log.timeUnixNano)),
    outcome: attemptField(log.attributes, "outcome"),
    reason: attemptField(log.attributes, "reason"),
    grantType: attemptField(log.attributes, "grant_type"),
  };
}

const GRANTS: Record<string, string> = {
  authorization_code: "Sign-in code",
  refresh_token: "Refresh",
  "urn:ietf:params:oauth:grant-type:jwt-bearer": "Enterprise assertion",
  client_credentials: "Assistant credential",
};

/** Plain words for a token request's grant; empty when there is none. */
export function grantLabel(grantType: string): string {
  if (grantType === "") return "";
  return GRANTS[grantType] ?? "Other grant";
}

export function outcomeLabel(stage: string, outcome: string): string {
  return (
    outcomeStyles(stage).find((style) => style.outcome === outcome)?.label ??
    "Other outcome"
  );
}

export function formatTime(date: Date): string {
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Whether a result echoes exactly this window, so it may be shown for it. */
export function matchesWindow(
  result: { from: Date; to: Date } | undefined,
  from: Date,
  to: Date,
): boolean {
  return (
    result !== undefined &&
    result.from.getTime() === from.getTime() &&
    result.to.getTime() === to.getTime()
  );
}
