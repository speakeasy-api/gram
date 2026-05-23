#!/usr/bin/env -S node

//MISE description="Seed the local ClickHouse with synthetic authz deny challenges so the org home page has data to render"

import crypto from "node:crypto";

import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "@gram/client/core.js";
import { authInfo } from "@gram/client/funcs/authInfo.js";

const CLICKHOUSE_URL = `http://${process.env.CLICKHOUSE_HOST ?? "127.0.0.1"}:${
  process.env.CLICKHOUSE_HTTP_PORT ?? "8123"
}`;
const CLICKHOUSE_USER = process.env.CLICKHOUSE_USERNAME ?? "gram";
const CLICKHOUSE_PASSWORD = process.env.CLICKHOUSE_PASSWORD ?? "gram";
const CLICKHOUSE_DATABASE = process.env.CLICKHOUSE_DATABASE ?? "default";

type ChallengeRow = {
  timestamp: string;
  organization_id: string;
  project_id: string;
  trace_id: string;
  span_id: string;
  request_id: string;
  principal_urn: string;
  principal_type: string;
  user_id: string | null;
  user_external_id: string | null;
  user_email: string | null;
  api_key_id: string | null;
  session_id: string | null;
  role_slugs: string[];
  operation: string;
  outcome: string;
  reason: string;
  scope: string;
  resource_kind: string;
  resource_id: string;
  selector: string;
  expanded_scopes: string[];
  "requested_checks.scope": string[];
  "requested_checks.resource_kind": string[];
  "requested_checks.resource_id": string[];
  "requested_checks.selector": string[];
  "matched_grants.principal_urn": string[];
  "matched_grants.scope": string[];
  "matched_grants.selector": string[];
  "matched_grants.matched_via_check_scope": string[];
  evaluated_grant_count: number;
  filter_candidate_count: number;
  filter_allowed_count: number;
};

function hex(len: number): string {
  return crypto.randomBytes(len / 2).toString("hex");
}

function formatTs(date: Date): string {
  // ClickHouse DateTime64(9) accepts "YYYY-MM-DD HH:mm:ss.SSSSSSSSS".
  // JSONEachRow tolerates ISO-ish strings too — millisecond precision is enough here.
  return date.toISOString().replace("T", " ").replace("Z", "");
}

async function clickhouseInsert(rows: ChallengeRow[]): Promise<void> {
  const body = rows.map((r) => JSON.stringify(r)).join("\n");
  const url = new URL(CLICKHOUSE_URL);
  url.searchParams.set("database", CLICKHOUSE_DATABASE);
  url.searchParams.set(
    "query",
    "INSERT INTO authz_challenges FORMAT JSONEachRow",
  );

  const auth = Buffer.from(
    `${CLICKHOUSE_USER}:${CLICKHOUSE_PASSWORD}`,
  ).toString("base64");

  const res = await fetch(url.toString(), {
    method: "POST",
    headers: {
      Authorization: `Basic ${auth}`,
      "Content-Type": "application/x-ndjson",
    },
    body,
  });

  if (!res.ok) {
    const text = await res.text();
    throw new Error(`ClickHouse insert failed: ${res.status} ${text}`);
  }
}

async function authenticateViaDevIDP(serverURL: string): Promise<string> {
  const loginRes = await fetch(`${serverURL}/rpc/auth.login`, {
    redirect: "manual",
  });
  const authorizeURL = loginRes.headers.get("location");
  if (!authorizeURL) {
    throw new Error("auth.login did not return a redirect");
  }
  const nonceCookie = loginRes.headers
    .getSetCookie()
    .find((c) => c.startsWith("gram_auth_nonce="));
  if (!nonceCookie) {
    throw new Error("auth.login did not set gram_auth_nonce cookie");
  }
  const nonceCookieValue = nonceCookie.split(";")[0];
  const authorizeRes = await fetch(authorizeURL, { redirect: "manual" });
  const callbackLocation = authorizeRes.headers.get("location");
  if (!callbackLocation) {
    throw new Error("dev-idp authorize did not return a redirect");
  }
  const callbackRes = await fetch(callbackLocation, {
    redirect: "manual",
    headers: { cookie: nonceCookieValue },
  });
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken) {
    throw new Error(
      `auth.callback did not return a session (status=${callbackRes.status})`,
    );
  }
  return sessionToken;
}

type Scenario = {
  principalUrn: string;
  principalType: "user" | "api_key";
  userId?: string;
  userEmail?: string;
  apiKeyId?: string;
  operation: "require" | "require_any" | "filter";
  outcome: "deny" | "allow";
  reason: string;
  scope: string;
  resourceKind: string;
  resourceId: string;
  selector: string;
  expandedScopes: string[];
  // Role principal URN that granted access; only used for allow outcomes.
  grantedByRoleUrn?: string;
  grantedByRoleSlug?: string;
  copies: number;
};

function buildScenarios(args: {
  organizationId: string;
  projectId: string;
  userId: string;
  userEmail: string;
}): Scenario[] {
  const { projectId } = args;
  return [
    // Deny scenarios — what the Recent challenges box surfaces.
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "deny",
      reason: "scope_unsatisfied",
      scope: "toolset:admin",
      resourceKind: "toolset",
      resourceId: `tst_${hex(16)}`,
      selector: JSON.stringify({ project: projectId }),
      expandedScopes: ["toolset:admin", "toolset:write", "toolset:read"],
      copies: 4,
    },
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "deny",
      reason: "no_grants",
      scope: "project:admin",
      resourceKind: "project",
      resourceId: projectId,
      selector: JSON.stringify({ id: projectId }),
      expandedScopes: ["project:admin"],
      copies: 2,
    },
    {
      principalUrn: `api_key:akey_${hex(12)}`,
      principalType: "api_key",
      apiKeyId: `akey_${hex(12)}`,
      operation: "require",
      outcome: "deny",
      reason: "scope_unsatisfied",
      scope: "mcp:invoke",
      resourceKind: "mcp",
      resourceId: `mcp_${hex(16)}`,
      selector: JSON.stringify({ project: projectId }),
      expandedScopes: ["mcp:invoke", "mcp:read"],
      copies: 7,
    },
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "deny",
      reason: "deny_grant",
      scope: "environment:write",
      resourceKind: "environment",
      resourceId: `env_${hex(16)}`,
      selector: JSON.stringify({ project: projectId, name: "production" }),
      expandedScopes: ["environment:write", "environment:read"],
      copies: 1,
    },
    // Allow scenarios — for the full /access/challenges page when filtering
    // to "approvals". Each has a matched grant so the bucket is interpretable.
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "allow",
      reason: "grant_matched",
      scope: "toolset:read",
      resourceKind: "toolset",
      resourceId: `tst_${hex(16)}`,
      selector: JSON.stringify({ project: projectId }),
      expandedScopes: ["toolset:read"],
      grantedByRoleUrn: "role:organization:admin",
      grantedByRoleSlug: "admin",
      copies: 12,
    },
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "allow",
      reason: "grant_matched",
      scope: "project:read",
      resourceKind: "project",
      resourceId: projectId,
      selector: JSON.stringify({ id: projectId }),
      expandedScopes: ["project:read"],
      grantedByRoleUrn: "role:organization:admin",
      grantedByRoleSlug: "admin",
      copies: 9,
    },
    {
      principalUrn: `api_key:akey_${hex(12)}`,
      principalType: "api_key",
      apiKeyId: `akey_${hex(12)}`,
      operation: "require",
      outcome: "allow",
      reason: "grant_matched",
      scope: "mcp:read",
      resourceKind: "mcp",
      resourceId: `mcp_${hex(16)}`,
      selector: JSON.stringify({ project: projectId }),
      expandedScopes: ["mcp:read"],
      grantedByRoleUrn: "role:organization:viewer",
      grantedByRoleSlug: "viewer",
      copies: 18,
    },
    {
      principalUrn: `user:${args.userId}`,
      principalType: "user",
      userId: args.userId,
      userEmail: args.userEmail,
      operation: "require",
      outcome: "allow",
      reason: "grant_matched",
      scope: "environment:read",
      resourceKind: "environment",
      resourceId: `env_${hex(16)}`,
      selector: JSON.stringify({ project: projectId, name: "staging" }),
      expandedScopes: ["environment:read"],
      grantedByRoleUrn: "role:organization:editor",
      grantedByRoleSlug: "editor",
      copies: 5,
    },
  ];
}

function scenarioToRows(
  s: Scenario,
  organizationId: string,
  projectId: string,
): ChallengeRow[] {
  const rows: ChallengeRow[] = [];
  const now = Date.now();
  const isAllow = s.outcome === "allow";
  // Allow rows must carry a non-empty matched_grants entry — that's the
  // signal the bucket query uses to compute matched_grant_count.
  const matchedPrincipal =
    isAllow && s.grantedByRoleUrn ? [s.grantedByRoleUrn] : [];
  const matchedScope = isAllow ? [s.scope] : [];
  const matchedSelector = isAllow ? [s.selector] : [];
  const matchedVia = isAllow ? [s.scope] : [];
  const roleSlugs = isAllow && s.grantedByRoleSlug ? [s.grantedByRoleSlug] : [];

  for (let i = 0; i < s.copies; i++) {
    // Spread challenges over the last 6 hours so timestamps look real.
    const ts = new Date(now - Math.floor(Math.random() * 6 * 60 * 60 * 1000));
    rows.push({
      timestamp: formatTs(ts),
      organization_id: organizationId,
      project_id: projectId,
      trace_id: hex(32),
      span_id: hex(16),
      request_id: `req_${hex(16)}`,
      principal_urn: s.principalUrn,
      principal_type: s.principalType,
      user_id: s.userId ?? null,
      user_external_id: null,
      user_email: s.userEmail ?? null,
      api_key_id: s.apiKeyId ?? null,
      session_id: null,
      role_slugs: roleSlugs,
      operation: s.operation,
      outcome: s.outcome,
      reason: s.reason,
      scope: s.scope,
      resource_kind: s.resourceKind,
      resource_id: s.resourceId,
      selector: s.selector,
      expanded_scopes: s.expandedScopes,
      "requested_checks.scope": [s.scope],
      "requested_checks.resource_kind": [s.resourceKind],
      "requested_checks.resource_id": [s.resourceId],
      "requested_checks.selector": [s.selector],
      "matched_grants.principal_urn": matchedPrincipal,
      "matched_grants.scope": matchedScope,
      "matched_grants.selector": matchedSelector,
      "matched_grants.matched_via_check_scope": matchedVia,
      evaluated_grant_count: isAllow ? 3 : 0,
      filter_candidate_count: 0,
      filter_allowed_count: 0,
    });
  }
  return rows;
}

async function main(): Promise<void> {
  intro("Seeding synthetic authz challenges (deny + allow) into ClickHouse...");
  let success = false;
  using _ = {
    [Symbol.dispose]() {
      outro(success ? "Seeding complete!" : "Seeding failed.");
    },
  };

  const serverURL = process.env["GRAM_SERVER_URL"];
  if (!serverURL) {
    throw new Error(
      "GRAM_SERVER_URL is not set — run via `mise run seed-challenges`",
    );
  }

  const gram = new GramCore({ serverURL });
  const sessionId = await authenticateViaDevIDP(serverURL);
  log.info("Authenticated via dev-idp");

  const res = await authInfo(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (!res.ok) {
    throw new Error(`authInfo failed: ${JSON.stringify(res.error)}`);
  }
  const session = res.value.result;
  const orgId = session.activeOrganizationId;
  if (!orgId) {
    throw new Error("No active organization on session");
  }
  const org = session.organizations.find((o) => o.id === orgId);
  if (!org || org.projects.length === 0) {
    throw new Error(
      "Active org has no projects — run `mise run seed` first so there are projects to scope challenges to",
    );
  }
  const userId = session.userId;
  const userEmail = session.userEmail;
  const project = org.projects[0]!;

  log.info(
    `Org=${orgId} project=${project.slug} user=${userEmail} — building scenarios`,
  );

  const scenarios = buildScenarios({
    organizationId: orgId,
    projectId: project.id,
    userId,
    userEmail,
  });
  const rows = scenarios.flatMap((s) => scenarioToRows(s, orgId, project.id));

  const denyBuckets = scenarios.filter((s) => s.outcome === "deny").length;
  const allowBuckets = scenarios.filter((s) => s.outcome === "allow").length;
  log.info(
    `Inserting ${rows.length} challenge rows across ${scenarios.length} buckets (${denyBuckets} deny, ${allowBuckets} allow)`,
  );
  await clickhouseInsert(rows);

  log.info(
    "Done. Refresh the org home page — Recent challenges should now render the buckets.",
  );
  success = true;
}

await main();
