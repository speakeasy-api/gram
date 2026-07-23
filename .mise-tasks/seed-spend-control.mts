#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed spend-control test data: org members with directory attributes and roles in Postgres, plus per-member LLM spend in ClickHouse"

// Seeds a coherent local dataset for the Spend Control feature:
//
//   Postgres   – synthetic org members (@acme.test) with directory profiles
//                (department, job title, groups, ...) and org role
//                assignments (admin/member). Your real account is enriched
//                with a directory profile too so you can target yourself.
//   ClickHouse – hourly `gen_ai` usage rows per member across the current
//                month, shaped to satisfy the spend_rule_usage_summaries_mv
//                predicate so spend-rule evaluation sees the spend.
//
// Re-runnable: every write is keyed on stable seed identifiers, ClickHouse
// seed rows are replaced wholesale, and existing spend rules are backdated to
// the start of the month for local testing so the seeded history counts.

import crypto from "node:crypto";

import { intro, log, outro } from "@clack/prompts";
import { $ } from "zx";

// Seed usage rows carry a Cursor usage URN so they match is_generic_usage_row
// in the spend-rule and attribute-metrics rollups (both key on the
// codex:/cursor: usage prefix). The distinct suffix keeps the wholesale delete
// below from touching real cursor:usage telemetry.
const SEED_GRAM_URN = "cursor:usage:spend-control-seed";

interface Persona {
  name: string;
  email: string;
  department: string;
  jobTitle: string;
  employeeType: string;
  division: string;
  costCenter: string;
  groups: string[];
  role: "admin" | "member";
  /** Target spend in USD across the elapsed days of the current month. */
  monthlySpendUsd: number;
}

// Spread across departments/roles so every rule shape has someone to match:
// with a $500 monthly limit, Ada breaches, Grace is approaching (86%), and
// the rest stay healthy.
const PERSONAS: Persona[] = [
  {
    name: "Ada Lovelace",
    email: "ada.lovelace@acme.test",
    department: "Engineering",
    jobTitle: "Staff Engineer",
    employeeType: "full_time",
    division: "R&D",
    costCenter: "CC-1001",
    groups: ["eng-frontier", "leadership"],
    role: "member",
    monthlySpendUsd: 620,
  },
  {
    name: "Grace Hopper",
    email: "grace.hopper@acme.test",
    department: "Engineering",
    jobTitle: "Software Engineer",
    employeeType: "full_time",
    division: "R&D",
    costCenter: "CC-1001",
    groups: ["eng-frontier"],
    role: "member",
    monthlySpendUsd: 430,
  },
  {
    name: "Alan Turing",
    email: "alan.turing@acme.test",
    department: "Engineering",
    jobTitle: "Software Engineer",
    employeeType: "intern",
    division: "R&D",
    costCenter: "CC-1001",
    groups: ["interns"],
    role: "member",
    monthlySpendUsd: 120,
  },
  {
    name: "Margaret Hamilton",
    email: "margaret.hamilton@acme.test",
    department: "Engineering",
    jobTitle: "Engineering Manager",
    employeeType: "full_time",
    division: "R&D",
    costCenter: "CC-1002",
    groups: ["leadership"],
    role: "admin",
    monthlySpendUsd: 260,
  },
  {
    name: "Katherine Johnson",
    email: "katherine.johnson@acme.test",
    department: "Data Science",
    jobTitle: "Data Scientist",
    employeeType: "full_time",
    division: "R&D",
    costCenter: "CC-2043",
    groups: ["ml-team"],
    role: "member",
    monthlySpendUsd: 310,
  },
  {
    name: "Annie Easley",
    email: "annie.easley@acme.test",
    department: "Data Science",
    jobTitle: "ML Engineer",
    employeeType: "contractor",
    division: "R&D",
    costCenter: "CC-2043",
    groups: ["ml-team"],
    role: "member",
    monthlySpendUsd: 180,
  },
  {
    name: "Mary Jackson",
    email: "mary.jackson@acme.test",
    department: "Finance",
    jobTitle: "Analyst",
    employeeType: "full_time",
    division: "Go-To-Market",
    costCenter: "CC-3100",
    groups: [],
    role: "member",
    monthlySpendUsd: 45,
  },
  {
    name: "Dorothy Vaughan",
    email: "dorothy.vaughan@acme.test",
    department: "Support",
    jobTitle: "Support Engineer",
    employeeType: "full_time",
    division: "Go-To-Market",
    costCenter: "CC-3100",
    groups: [],
    role: "member",
    monthlySpendUsd: 25,
  },
];

/** Directory profile applied to your real account so block rules can target
 *  you end-to-end; the spend puts you past a $500 monthly limit. */
const SELF_PROFILE = {
  department: "Engineering",
  jobTitle: "Staff Engineer",
  employeeType: "full_time",
  division: "R&D",
  costCenter: "CC-1001",
  groups: ["eng-frontier"],
  monthlySpendUsd: 520,
};

const GROUPS = ["eng-frontier", "leadership", "interns", "ml-team"];

function hash(value: string): string {
  return crypto.createHash("sha1").update(value).digest("hex").slice(0, 16);
}

function sqlString(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

async function authenticateViaDevIDP(serverURL: string): Promise<string> {
  const loginRes = await fetch(`${serverURL}/rpc/auth.login`, {
    redirect: "manual",
  });
  const authorizeURL = loginRes.headers.get("location");
  if (!authorizeURL) throw new Error("auth.login did not return a redirect");
  const nonceCookie = loginRes.headers
    .getSetCookie()
    .find((c) => c.startsWith("gram_auth_nonce="));
  if (!nonceCookie) throw new Error("auth.login did not set gram_auth_nonce");
  const nonceCookieValue = nonceCookie.split(";")[0];
  const authorizeRes = await fetch(authorizeURL, { redirect: "manual" });
  const callbackLocation = authorizeRes.headers.get("location");
  if (!callbackLocation) {
    throw new Error("dev-idp authorize did not return a redirect");
  }
  const callbackRes = await fetch(callbackLocation, {
    redirect: "manual",
    headers: { cookie: nonceCookieValue ?? "" },
  });
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken) {
    throw new Error(
      `auth.callback did not return a session (status=${callbackRes.status})`,
    );
  }
  return sessionToken;
}

async function psql(sql: string): Promise<string> {
  const dbUser = process.env.DB_USER ?? "gram";
  const dbName = process.env.DB_NAME ?? "gram";
  const out =
    await $`docker compose exec -T gram-db psql -U ${dbUser} -d ${dbName} -v ON_ERROR_STOP=1 -Atc ${sql}`.quiet();
  return out.stdout.trim();
}

async function clickhouse(query: string): Promise<string> {
  const host = process.env.CLICKHOUSE_HOST ?? "127.0.0.1";
  const port = process.env.CLICKHOUSE_HTTP_PORT ?? "8123";
  const user = process.env.CLICKHOUSE_USERNAME ?? "gram";
  const password = process.env.CLICKHOUSE_PASSWORD ?? "gram";
  const database = process.env.CLICKHOUSE_DATABASE ?? "default";
  const params = new URLSearchParams({ database, mutations_sync: "1" });
  const res = await fetch(`http://${host}:${port}/?${params}`, {
    method: "POST",
    headers: {
      Authorization: `Basic ${Buffer.from(`${user}:${password}`).toString("base64")}`,
    },
    body: query,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`clickhouse query failed: ${text}`);
  return text.trim();
}

function directoryAttributesJSON(p: {
  department: string;
  jobTitle: string;
  employeeType: string;
  division: string;
  costCenter: string;
}): string {
  return JSON.stringify({
    department_name: p.department,
    job_title: p.jobTitle,
    employee_type: p.employeeType,
    division_name: p.division,
    cost_center_name: p.costCenter,
  });
}

/** One member's usage rows: three fixed slots per elapsed day of the current
 *  month, deterministic so re-seeding lands on identical totals. */
function usageRows(
  email: string,
  profile: {
    department: string;
    jobTitle: string;
    employeeType: string;
    division: string;
    costCenter: string;
    groups: string[];
  },
  roles: string[],
  monthlySpendUsd: number,
  projectId: string,
): string[] {
  const now = new Date();
  const monthStart = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1);
  const daysElapsed = now.getUTCDate();
  const slotsPerDay = [9, 12, 15]; // UTC hours

  // Collect only the slots at or before now — spend is never seeded in the
  // future. perSlot divides by the slots actually emitted (not every scheduled
  // slot) so the month-to-date total lands on monthlySpendUsd; dividing by all
  // scheduled slots would undershoot whenever the current day's later slots are
  // skipped, and the advertised breach thresholds might then never trigger.
  const slots: { ts: number; day: number }[] = [];
  for (let day = 0; day < daysElapsed; day++) {
    for (const hour of slotsPerDay) {
      const ts = monthStart + day * 86_400_000 + hour * 3_600_000;
      if (ts > now.getTime()) continue;
      slots.push({ ts, day });
    }
  }
  const perSlot = slots.length > 0 ? monthlySpendUsd / slots.length : 0;

  const rows: string[] = [];
  for (const { ts, day } of slots) {
    const conversationId = `seed-spend-${hash(email)}-${day + 1}`;
    const nanos = BigInt(ts) * 1_000_000n;
    const cost = perSlot;
    const inputTokens = Math.round(cost * 1000);
    const outputTokens = Math.round(cost * 400);

    // Flat dotted keys land on the same JSON paths the ingest pipeline writes.
    // The SEED_GRAM_URN (a cursor:usage prefix) is what makes the row count as
    // is_generic_usage_row; gen_ai.usage.cost supplies the cost the rollup sums.
    const attrs = JSON.stringify({
      "user.email": email,
      "user.attributes.department_name": profile.department,
      "user.attributes.job_title": profile.jobTitle,
      "user.attributes.employee_type": profile.employeeType,
      "user.attributes.division_name": profile.division,
      "user.attributes.cost_center_name": profile.costCenter,
      "user.roles": [...roles].sort(),
      "user.groups": [...profile.groups].sort(),
      "gen_ai.operation.name": "chat",
      "gen_ai.conversation.id": conversationId,
      "gen_ai.response.model": "claude-sonnet-4-5",
      "gen_ai.usage.cost": cost.toFixed(6),
      "gen_ai.usage.input_tokens": String(inputTokens),
      "gen_ai.usage.output_tokens": String(outputTokens),
      "gen_ai.usage.total_tokens": String(inputTokens + outputTokens),
    });

    rows.push(
      `(${nanos}, ${nanos}, 'INFO', 'seed: spend control usage', ${sqlString(attrs)}, '{}', '${projectId}', ${sqlString(SEED_GRAM_URN)}, 'gram-seed', ${sqlString(conversationId)})`,
    );
  }
  return rows;
}

async function main(): Promise<void> {
  intro("Seeding spend-control test data...");

  const serverURL = process.env.GRAM_SERVER_URL;
  if (!serverURL) {
    throw new Error(
      "GRAM_SERVER_URL is not set — run via `mise run seed-spend-control`",
    );
  }

  const sessionId = await authenticateViaDevIDP(serverURL);
  const infoResponse = await fetch(`${serverURL}/rpc/auth.info`, {
    headers: { "Gram-Session": sessionId },
  });
  if (!infoResponse.ok) {
    throw new Error(
      `auth.info failed (${infoResponse.status}): ${await infoResponse.text()}`,
    );
  }
  const info = (await infoResponse.json()) as {
    active_organization_id?: string;
  };
  const orgId = info.active_organization_id;
  if (!orgId) throw new Error("No active organization on session");
  log.info(`Active org: ${orgId}`);

  /* ---------------------------- Postgres ---------------------------- */

  // Synthetic members. Stable ids derived from the email keep every rerun
  // hitting the same rows. userId/workosId identify the (global) user and stay
  // email-keyed so the same person is shared across orgs; the directory id is
  // org-scoped, so it is namespaced with orgId to avoid a second seeded org
  // stealing or mutating the first org's directory rows.
  const members = PERSONAS.map((p) => ({
    ...p,
    userId: `usr_spend_${hash(p.email)}`,
    workosId: `spend_workos_${hash(p.email)}`,
    dirId: `spend_dir_${hash(orgId + p.email)}`,
  }));

  const usersValues = members
    .map(
      (m) =>
        `(${sqlString(m.userId)}, ${sqlString(m.email)}, ${sqlString(m.name)}, ${sqlString(m.workosId)})`,
    )
    .join(",\n");
  await psql(
    `INSERT INTO users (id, email, display_name, workos_id) VALUES\n${usersValues}\nON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name;`,
  );

  const relationshipValues = members
    .map(
      (m) =>
        `(${sqlString(orgId)}, ${sqlString(m.userId)}, ${sqlString(m.workosId)}, ${sqlString(`spend_mem_${hash(orgId + m.email)}`)})`,
    )
    .join(",\n");
  await psql(
    `INSERT INTO organization_user_relationships (organization_id, user_id, workos_user_id, workos_membership_id) VALUES\n${relationshipValues}\nON CONFLICT DO NOTHING;`,
  );
  log.info(`Upserted ${members.length} synthetic org members`);

  // Directory groups are org-scoped, so their workos ids are namespaced with
  // orgId (the readable name is unchanged).
  const groupId = (name: string): string => `spend_grp_${hash(orgId + name)}`;
  const groupValues = GROUPS.map(
    (name) =>
      `(${sqlString(orgId)}, ${sqlString(groupId(name))}, ${sqlString(name)}, now(), now())`,
  ).join(",\n");
  await psql(
    `INSERT INTO directory_groups (organization_id, workos_directory_group_id, name, workos_created_at, workos_updated_at) VALUES\n${groupValues}\nON CONFLICT (workos_directory_group_id) DO UPDATE SET name = EXCLUDED.name, deleted_at = NULL, workos_deleted_at = NULL;`,
  );

  // Real (non-seeded) org members — typically just you. They get the same
  // directory treatment so rules can target them.
  const selfRows = await psql(
    `SELECT u.id, COALESCE(NULLIF(du.email, ''), u.email), COALESCE(u.workos_id, ''), COALESCE(du.workos_directory_user_id, '') FROM organization_user_relationships our JOIN users u ON u.id = our.user_id LEFT JOIN LATERAL (SELECT d.email, d.workos_directory_user_id FROM directory_users d WHERE d.organization_id = our.organization_id AND d.user_id = u.id AND d.deleted IS FALSE AND d.workos_deleted IS FALSE ORDER BY d.created_at DESC LIMIT 1) du ON TRUE WHERE our.organization_id = ${sqlString(orgId)} AND our.deleted IS FALSE AND u.id NOT LIKE 'usr_spend_%' AND u.id NOT LIKE 'usr_seed_%';`,
  );
  const selfMembers = selfRows
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [userId, email, workosId, dirId] = line.split("|");
      return {
        userId: userId!,
        email: email!,
        workosId: workosId!,
        dirId: dirId || `spend_dir_self_${hash(orgId + userId!)}`,
      };
    });

  if (selfMembers.length > 0) {
    const selfEmailValues = selfMembers
      .map((m) => `(${sqlString(m.userId)}, ${sqlString(m.email)})`)
      .join(",\n");
    await psql(
      `UPDATE users u SET email = self.email FROM (VALUES\n${selfEmailValues}\n) AS self (user_id, email) WHERE u.id = self.user_id AND u.email <> self.email;`,
    );
  }

  const directoryValues = [
    ...members.map(
      (m) =>
        `(${sqlString(orgId)}, ${sqlString(m.userId)}, ${sqlString(m.dirId)}, ${sqlString(m.email)}, ${sqlString(directoryAttributesJSON(m))}::jsonb, now(), now())`,
    ),
    ...selfMembers.map(
      (m) =>
        `(${sqlString(orgId)}, ${sqlString(m.userId)}, ${sqlString(m.dirId)}, ${sqlString(m.email)}, ${sqlString(directoryAttributesJSON(SELF_PROFILE))}::jsonb, now(), now())`,
    ),
  ].join(",\n");
  await psql(
    `INSERT INTO directory_users (organization_id, user_id, workos_directory_user_id, email, attributes, workos_created_at, workos_updated_at) VALUES\n${directoryValues}\nON CONFLICT (workos_directory_user_id) DO UPDATE SET user_id = EXCLUDED.user_id, email = EXCLUDED.email, attributes = EXCLUDED.attributes, deleted_at = NULL, workos_deleted_at = NULL;`,
  );
  log.info(
    `Upserted ${members.length + selfMembers.length} directory profiles (${selfMembers.map((m) => m.email).join(", ") || "no real members found"})`,
  );

  const membershipPairs = [
    ...members.flatMap((m) =>
      m.groups.map((g) => ({ dirId: m.dirId, groupId: groupId(g) })),
    ),
    ...selfMembers.flatMap((m) =>
      SELF_PROFILE.groups.map((g) => ({
        dirId: m.dirId,
        groupId: groupId(g),
      })),
    ),
  ];
  const membershipValues = membershipPairs
    .map((p) => `(${sqlString(p.dirId)}, ${sqlString(p.groupId)})`)
    .join(",\n");
  await psql(
    `INSERT INTO directory_user_group_memberships (directory_user_id, directory_group_id, workos_directory_user_id, workos_directory_group_id, workos_created_at)\nSELECT du.id, dg.id, du.workos_directory_user_id, dg.workos_directory_group_id, now()\nFROM (VALUES\n${membershipValues}\n) AS pair (dir_workos_id, grp_workos_id)\nJOIN directory_users du ON du.workos_directory_user_id = pair.dir_workos_id\nJOIN directory_groups dg ON dg.workos_directory_group_id = pair.grp_workos_id\nON CONFLICT (directory_user_id, directory_group_id) WHERE deleted IS FALSE DO NOTHING;`,
  );

  // Role assignments resolve global role ids by slug, mirroring how WorkOS
  // sync writes them ('role:global:<uuid>').
  const roleValues = members
    .map(
      (m) =>
        `(${sqlString(m.workosId)}, ${sqlString(m.userId)}, ${sqlString(m.role)})`,
    )
    .join(",\n");
  await psql(
    `INSERT INTO organization_role_assignments (organization_id, workos_user_id, user_id, role_urn, workos_updated_at)\nSELECT ${sqlString(orgId)}, assignment.workos_user_id, assignment.user_id, 'role:global:' || gr.id::text, now()\nFROM (VALUES\n${roleValues}\n) AS assignment (workos_user_id, user_id, role_slug)\nJOIN global_roles gr ON gr.workos_slug = assignment.role_slug AND gr.deleted IS FALSE\nON CONFLICT DO NOTHING;`,
  );
  log.info("Assigned org roles (admins: margaret.hamilton + your account)");

  /* --------------------------- ClickHouse --------------------------- */

  const projectRows = await psql(
    `SELECT id FROM projects WHERE organization_id = ${sqlString(orgId)} AND deleted IS FALSE;`,
  );
  const projectIds = projectRows.split("\n").filter(Boolean);
  if (projectIds.length === 0) throw new Error("org has no projects");
  const projectId = projectIds[0]!;

  const spenders = [
    ...members.map((m) => ({
      email: m.email,
      profile: m,
      roles: [m.role],
      monthlySpendUsd: m.monthlySpendUsd,
    })),
    ...selfMembers.map((m) => ({
      email: m.email,
      profile: { ...SELF_PROFILE, groups: SELF_PROFILE.groups },
      roles: ["admin"],
      monthlySpendUsd: SELF_PROFILE.monthlySpendUsd,
    })),
  ];

  const allEmails = spenders.map((s) => sqlString(s.email)).join(", ");
  const projectList = projectIds.map((id) => sqlString(id)).join(", ");

  // Replace previous seed rows so reruns do not double-count. The summary
  // delete is scoped to the seeded emails: aggregates for those members are
  // rebuilt from the fresh insert below.
  await clickhouse(
    `ALTER TABLE telemetry_logs DELETE WHERE gram_urn = ${sqlString(SEED_GRAM_URN)} AND gram_project_id IN (${projectList})`,
  );
  await clickhouse(
    `ALTER TABLE attribute_metrics_summaries DELETE WHERE gram_project_id IN (${projectList}) AND user_email IN (${allEmails})`,
  );
  await clickhouse(
    `ALTER TABLE spend_rule_usage_summaries DELETE WHERE gram_project_id IN (${projectList}) AND user_email IN (${allEmails})`,
  );

  const rows = spenders.flatMap((s) =>
    usageRows(s.email, s.profile, s.roles, s.monthlySpendUsd, projectId),
  );
  await clickhouse(
    `INSERT INTO telemetry_logs (time_unix_nano, observed_time_unix_nano, severity_text, body, attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id) VALUES\n${rows.join(",\n")}`,
  );
  log.info(`Inserted ${rows.length} usage rows into ClickHouse`);

  const spendCheck = await clickhouse(
    `SELECT user_email, round(sum(total_cost), 2) AS spend FROM spend_rule_usage_summaries WHERE gram_project_id IN (${projectList}) AND user_email IN (${allEmails}) GROUP BY user_email ORDER BY spend DESC FORMAT PrettyCompactMonoBlock`,
  );
  log.info(`Aggregated month-to-date spend per member:\n${spendCheck}`);

  log.info(
    [
      "Try these rule targets:",
      '  department_name == "Engineering"  ($500/mo: Ada breaches, Grace approaches, you breach)',
      '  "ml-team" in groups               ($250/mo: Katherine breaches)',
      '  "admin" in roles                  (Margaret + your account)',
      `  email == "${selfMembers[0]?.email ?? "you@example.com"}"        (block yourself to test the Claude hook gate)`,
    ].join("\n"),
  );
  outro("Done.");
}

await main().catch((error: unknown) => {
  outro("Seeding failed.");
  throw error;
});
