#!/usr/bin/env -S node

//MISE description="Seed realistic AI Insights test data — patterned tool-call traces and logs for the default project."

/*
 * What this seeds and why:
 *
 *   The AI Insights agent in the dashboard sidebar reads telemetry_logs via
 *   the observability MCP (gram_search_logs). Out of the box the local DB
 *   has plenty of generic logs but no story — no recurring error theme the
 *   agent can group, hypothesize about, and propose a tool-variation fix
 *   for. This script writes ~500-1000 records with five intentional
 *   patterns (see PATTERN_* below) plus one user with disproportionate
 *   failures. After running, you can ask the sidebar agent things like
 *   "What's the most common error in the last 7 days?" and get a
 *   meaningful answer.
 *
 * Idempotency:
 *
 *   Every row carries `attributes.gram.seed_batch = "insights-demo"`. On
 *   each run we DELETE rows with that tag (via ALTER TABLE ... DELETE) and
 *   then INSERT fresh ones. ClickHouse mutations are async but we wait for
 *   them. Synthetic-only — no risk to real data because the tag is unique
 *   to this script.
 */

import crypto from "node:crypto";

import { intro, log, outro } from "@clack/prompts";
import { $ } from "zx";

// ---- Constants ----

const SEED_BATCH = "insights-demo";
// The observability MCP (the `mcp-logs` toolset) is scoped to the
// `ecommerce-api` project, so the AI Insights agent's gram_search_logs tool
// only finds rows tagged with that project's ID. We resolve the UUID at
// runtime from the slug because `mise db:reset` + `mise seed` mints fresh
// IDs each time, and a hardcoded constant would silently put rows where
// the agent can't see them.
const PROJECT_SLUG = "ecommerce-api";
const ORG_ID = "550e8400-e29b-41d4-a716-446655440000";

let PROJECT_ID = "";

async function resolveProjectID(): Promise<string> {
  const result =
    await $`docker exec gram-gram-db-1 psql -U gram -d gram -tA -c "SELECT id FROM projects WHERE slug = '${PROJECT_SLUG}' LIMIT 1"`.quiet();
  const id = result.stdout.trim();
  if (!id) {
    throw new Error(
      `Could not find project '${PROJECT_SLUG}' in Postgres. Run \`mise seed\` first to seed the base projects/toolsets.`,
    );
  }
  return id;
}

// Real tool URNs from the ecommerce-api project so any agent-proposed variation
// resolves to a real tool the human can review/apply. The synthetic narratives
// (slug-vs-UUID, auth failures, slow latency) attach to these existing tools.
const TOOL_CREATE_INVOICE =
  "tools:http:ecommerce-api:ecommerce_api_create_order";
const TOOL_GET_ORG = "tools:http:ecommerce-api:ecommerce_api_get_product";
const TOOL_GET_USER_WORKSPACES =
  "tools:http:ecommerce-api:ecommerce_api_list_orders";
const TOOL_GET_WORKSPACE_USERS =
  "tools:http:ecommerce-api:ecommerce_api_search_products";

// Distinct synthetic users — short hex suffixes so they read as real-ish.
const USERS = [
  "user-7f3a", // the troubled one
  "user-2bd1",
  "user-8c45",
  "user-3e90",
  "user-1a76",
  "user-9d28",
  "user-4f0e",
  "user-6b3c",
  "user-5e21",
  "user-0c8a",
];

const TROUBLED_USER = USERS[0]!; // user-7f3a — gets disproportionate failures

const SEVEN_DAYS_HOURS = 24 * 7;
const NOW_NS = BigInt(Date.now()) * 1_000_000n;

// ---- Helpers ----

function traceId(): string {
  return crypto.randomBytes(16).toString("hex"); // 32 hex chars
}

function spanId(): string {
  return crypto.randomBytes(8).toString("hex"); // 16 hex chars
}

function pickRandomTime(): bigint {
  // Distribute across the last 7 days, biased slightly toward more-recent so
  // the data looks like an active system not an ancient backfill. The 7-day
  // window matches the dashboard's default "last 7 days" overview range.
  const offsetMs = BigInt(
    Math.floor(Math.random() ** 1.4 * SEVEN_DAYS_HOURS * 60 * 60 * 1000),
  );
  return NOW_NS - offsetMs * 1_000_000n;
}

function pickUser(): string {
  return USERS[Math.floor(Math.random() * USERS.length)]!;
}

function chatId(): string {
  return crypto.randomUUID();
}

function isoFromNs(ns: bigint): string {
  return new Date(Number(ns / 1_000_000n)).toISOString();
}

// ---- Record builder ----

interface LogRecord {
  time_unix_nano: string; // bigint as string for JSON
  observed_time_unix_nano: string;
  severity_text: "DEBUG" | "INFO" | "WARN" | "ERROR";
  body: string;
  trace_id: string;
  span_id: string;
  attributes: Record<string, unknown>;
  resource_attributes: Record<string, unknown>;
  gram_project_id: string;
  gram_urn: string;
  service_name: string;
}

function buildToolCallRecord(opts: {
  tool: string;
  toolName: string;
  severity: LogRecord["severity_text"];
  body: string;
  statusCode: number;
  durationMs: number;
  errorType?: string;
  user?: string;
  service?: string;
  extraAttrs?: Record<string, unknown>;
}): LogRecord {
  const ts = pickRandomTime();
  const tsStr = ts.toString();
  const userId = opts.user ?? pickUser();
  // Server name displayed in the dashboard's Top Servers card. Extract the
  // source slug from a URN of the shape "tools:http:<source>:<tool_name>".
  const urnParts = opts.tool.split(":");
  const toolSource = urnParts[2] ?? "unknown";
  // The trace_summaries materialized view filters event_source = 'hook' and
  // groups by hook.event for success/failure counters. Errors and 4xx/5xx
  // status codes get PostToolUseFailure; everything else PostToolUse.
  const isFailure = opts.severity === "ERROR" || opts.statusCode >= 400;
  const hookEvent = isFailure ? "PostToolUseFailure" : "PostToolUse";

  return {
    time_unix_nano: tsStr,
    observed_time_unix_nano: tsStr,
    severity_text: opts.severity,
    body: opts.body,
    trace_id: traceId(),
    span_id: spanId(),
    attributes: {
      gram: {
        project: { id: PROJECT_ID },
        tool: { urn: opts.tool, name: opts.toolName },
        // tool_call.source is the displayed "server name" in Top Servers card.
        tool_call: { source: toolSource },
        // event.source = 'hook' is the filter used by GetTopServers.
        event: { source: "hook" },
        // hook.event drives hook_has_success / hook_has_failure aggregates.
        hook: { event: hookEvent, source: toolSource },
        seed_batch: SEED_BATCH,
      },
      http: {
        // Path matches what trace_summaries_mv reads:
        // attributes.http.response.status_code
        response: { status_code: opts.statusCode },
        duration_ms: opts.durationMs,
      },
      user: { id: userId, email: `${userId}@example.com` },
      gen_ai: { conversation: { id: chatId() } },
      ...(opts.errorType ? { error: { type: opts.errorType } } : {}),
      ...(opts.extraAttrs ?? {}),
    },
    resource_attributes: {
      gram: {
        project: { id: PROJECT_ID },
        organization: { id: ORG_ID },
      },
      service: { name: opts.service ?? "gram-http-gateway" },
    },
    gram_project_id: PROJECT_ID,
    gram_urn: opts.tool,
    service_name: opts.service ?? "gram-http-gateway",
  };
}

// ---- Pattern generators ----

function patternCreateInvoiceSlugErrors(): LogRecord[] {
  // ~40 ERROR logs across 6 distinct users. 20 of them go to TROUBLED_USER.
  const records: LogRecord[] = [];
  const slugs = [
    "acme-corp",
    "globex",
    "umbrella",
    "initech",
    "hooli",
    "soylent",
  ];
  const fakeIds = ["abc-123", "customer-001", "demo", "test-customer"];

  for (let i = 0; i < 20; i++) {
    const slug = slugs[i % slugs.length] ?? "acme-corp";
    records.push(
      buildToolCallRecord({
        tool: TOOL_CREATE_INVOICE,
        toolName: "ecommerce_api_create_order",
        severity: "ERROR",
        statusCode: 422,
        durationMs: 80 + Math.floor(Math.random() * 60),
        errorType: "invalid_argument",
        body: `ecommerce_api_create_order failed: customer_id '${slug}' is not a valid UUID`,
        user: TROUBLED_USER,
      }),
    );
  }
  // 20 more spread across the other 5 users
  const otherUsers = USERS.slice(1, 6);
  for (let i = 0; i < 20; i++) {
    const value =
      i % 2 === 0 ? slugs[i % slugs.length] : fakeIds[i % fakeIds.length];
    records.push(
      buildToolCallRecord({
        tool: TOOL_CREATE_INVOICE,
        toolName: "ecommerce_api_create_order",
        severity: "ERROR",
        statusCode: 422,
        durationMs: 80 + Math.floor(Math.random() * 60),
        errorType: "invalid_argument",
        body: `ecommerce_api_create_order failed: customer_id '${value}' is not a valid UUID`,
        user: otherUsers[i % otherUsers.length]!,
      }),
    );
  }
  return records;
}

function patternGetOrgSlugErrors(): LogRecord[] {
  const records: LogRecord[] = [];
  const slugs = [
    "blue-widget",
    "premium-tee",
    "summer-sale",
    "vintage-mug",
    "leather-bag",
  ];
  for (let i = 0; i < 25; i++) {
    const slug = slugs[i % slugs.length] ?? "blue-widget";
    records.push(
      buildToolCallRecord({
        tool: TOOL_GET_ORG,
        toolName: "ecommerce_api_get_product",
        severity: "ERROR",
        statusCode: 404,
        durationMs: 60 + Math.floor(Math.random() * 40),
        errorType: "not_found",
        body: `product not found: slug '${slug}' — product_id must be a UUID`,
      }),
    );
  }
  return records;
}

function patternAuthFailures(): LogRecord[] {
  const records: LogRecord[] = [];
  // 10 go to TROUBLED_USER, 5 to others
  for (let i = 0; i < 10; i++) {
    records.push(
      buildToolCallRecord({
        tool: TOOL_GET_USER_WORKSPACES,
        toolName: "ecommerce_api_list_orders",
        severity: "ERROR",
        statusCode: 401,
        durationMs: 25 + Math.floor(Math.random() * 30),
        errorType: "missing_token",
        body: "ecommerce_api_list_orders failed: missing or invalid bearer token",
        user: TROUBLED_USER,
      }),
    );
  }
  for (let i = 0; i < 5; i++) {
    records.push(
      buildToolCallRecord({
        tool: TOOL_GET_USER_WORKSPACES,
        toolName: "ecommerce_api_list_orders",
        severity: "ERROR",
        statusCode: 401,
        durationMs: 25 + Math.floor(Math.random() * 30),
        errorType: "missing_token",
        body: "ecommerce_api_list_orders failed: missing or invalid bearer token",
        user: USERS[1 + (i % (USERS.length - 1))]!,
      }),
    );
  }
  return records;
}

function patternLatencyAnomalies(): LogRecord[] {
  const records: LogRecord[] = [];
  for (let i = 0; i < 30; i++) {
    const slowMs = 8000 + Math.floor(Math.random() * 7000); // 8-15s
    records.push(
      buildToolCallRecord({
        tool: TOOL_GET_WORKSPACE_USERS,
        toolName: "ecommerce_api_search_products",
        severity: "WARN",
        statusCode: 200,
        durationMs: slowMs,
        body: `ecommerce_api_search_products completed slowly (${slowMs}ms)`,
        extraAttrs: {
          gram: {
            tool: {
              urn: TOOL_GET_WORKSPACE_USERS,
              name: "speakeasy_get_workspace_users",
            },
            project: { id: PROJECT_ID },
            tool_call: { source: "mcp" },
            seed_batch: SEED_BATCH,
          },
          performance: { anomaly: true },
        },
      }),
    );
  }
  return records;
}

function patternBackgroundSuccess(): LogRecord[] {
  const records: LogRecord[] = [];
  const tools: Array<[string, string]> = [
    [TOOL_GET_ORG, "ecommerce_api_get_product"],
    [TOOL_GET_USER_WORKSPACES, "ecommerce_api_list_orders"],
    [TOOL_GET_WORKSPACE_USERS, "ecommerce_api_search_products"],
  ];
  for (let i = 0; i < 300; i++) {
    const [tool, toolName] = tools[i % tools.length]!;
    records.push(
      buildToolCallRecord({
        tool,
        toolName,
        severity: "INFO",
        statusCode: 200,
        durationMs: 120 + Math.floor(Math.random() * 200),
        body: `${toolName} succeeded`,
        // a handful via "gram-functions" service to vary service_name
        service: i % 25 === 0 ? "gram-functions" : "gram-http-gateway",
      }),
    );
  }
  return records;
}

// ---- Main ----

async function main() {
  intro("seed:insights-data");

  // Resolve the project UUID at runtime — see the PROJECT_SLUG comment above
  // for why this can't be a hardcoded constant.
  PROJECT_ID = await resolveProjectID();
  log.info(`Targeting project '${PROJECT_SLUG}' (${PROJECT_ID})`);

  const allRecords: LogRecord[] = [
    ...patternCreateInvoiceSlugErrors(),
    ...patternGetOrgSlugErrors(),
    ...patternAuthFailures(),
    ...patternLatencyAnomalies(),
    ...patternBackgroundSuccess(),
  ];

  log.info(
    `Prepared ${allRecords.length} records (40 invoice errors, 25 org errors, 15 auth errors, 30 latency, 300 success).`,
  );

  // 1) Idempotency: delete any prior batch with the same sentinel tag.
  log.info(`Deleting prior rows tagged seed_batch="${SEED_BATCH}" (if any)…`);
  const deleteSql = `ALTER TABLE default.telemetry_logs DELETE WHERE toString(attributes.gram.seed_batch) = '${SEED_BATCH}'`;
  await $`docker exec gram-clickhouse-1 clickhouse-client --query ${deleteSql}`;
  // Wait for the mutation to complete — synchronous would be nicer but
  // ALTER ... DELETE in ClickHouse is async; we poll the system table.
  for (let i = 0; i < 30; i++) {
    const pendingRaw =
      await $`docker exec gram-clickhouse-1 clickhouse-client --query "SELECT count() FROM system.mutations WHERE table = 'telemetry_logs' AND is_done = 0 AND command LIKE '%${SEED_BATCH}%' FORMAT TabSeparated"`.quiet();
    const pending = pendingRaw.stdout.trim();
    if (pending === "0") break;
    await new Promise((r) => setTimeout(r, 500));
  }
  log.info("Prior batch cleared.");

  // 2) Insert all rows in one batch via JSONEachRow piped on stdin.
  const ndjson = allRecords.map((r) => JSON.stringify(r)).join("\n");
  log.info(`Inserting ${allRecords.length} rows…`);
  // Pipe stdin to docker exec via zx's $.stdin
  const proc = $`docker exec -i gram-clickhouse-1 clickhouse-client --query ${"INSERT INTO default.telemetry_logs FORMAT JSONEachRow"}`;
  proc.stdin.write(ndjson);
  proc.stdin.end();
  await proc;

  // 3) Verify counts.
  const totalRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();
  const errInvoiceRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE severity_text = 'ERROR' AND urn = '${TOOL_CREATE_INVOICE}' AND toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();
  const errOrgRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE severity_text = 'ERROR' AND urn = '${TOOL_GET_ORG}' AND toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();
  const errAuthRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE severity_text = 'ERROR' AND urn = '${TOOL_GET_USER_WORKSPACES}' AND toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();
  const latencyRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE severity_text = 'WARN' AND urn = '${TOOL_GET_WORKSPACE_USERS}' AND toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();
  const troubledUserRaw =
    await $`docker exec gram-clickhouse-1 clickhouse-client --query ${`SELECT count() FROM default.telemetry_logs WHERE severity_text = 'ERROR' AND user_id = '${TROUBLED_USER}' AND toString(attributes.gram.seed_batch) = '${SEED_BATCH}' FORMAT TabSeparated`}`.quiet();

  log.info(`Total rows tagged ${SEED_BATCH}: ${totalRaw.stdout.trim()}`);
  log.info(`  ERROR create_invoice: ${errInvoiceRaw.stdout.trim()}`);
  log.info(`  ERROR get_organization: ${errOrgRaw.stdout.trim()}`);
  log.info(`  ERROR get_user_workspaces: ${errAuthRaw.stdout.trim()}`);
  log.info(`  WARN get_workspace_users (slow): ${latencyRaw.stdout.trim()}`);
  log.info(`  ERROR for ${TROUBLED_USER}: ${troubledUserRaw.stdout.trim()}`);

  log.info(
    `Earliest record: ${isoFromNs(allRecords.reduce((min, r) => (BigInt(r.time_unix_nano) < min ? BigInt(r.time_unix_nano) : min), NOW_NS))}`,
  );
  log.info(
    `Latest record:   ${isoFromNs(allRecords.reduce((max, r) => (BigInt(r.time_unix_nano) > max ? BigInt(r.time_unix_nano) : max), 0n))}`,
  );

  outro(
    "Done. Open the dashboard's AI Insights sidebar and ask: 'What's the most common error in the last 7 days?'",
  );
}

await main();
