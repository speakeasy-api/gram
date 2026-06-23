#!/usr/bin/env node
// Local cost-dashboard seed (dev only — never run against dev/prod databases).
//
// Generates a coherent, plausible @speakeasy.com org tree and writes telemetry
// rows into local ClickHouse `telemetry_logs`. The attribute_metrics_summaries
// materialized view aggregates them, so the org-scoped telemetry.query endpoint
// (and the cost dashboard) can group/filter by every existing dimension:
//   division_name, department_name, job_title, employee_type, cost_center_name,
//   email (user), model, hook_source (agent), role[], group[] (== team here).
//
// A deterministic slice of users are missing attributes, so every breakdown has
// a populated, drillable "(unset)" bucket (scalar → '', array → []).
//
// Manager + team are NOT queryable dimensions yet (deferred), but we stamp
//   user.attributes.manager_email / manager_name  and  user.groups = [team]
// so the data is ready and "Team" works today via the existing `group` axis.
//
// Pure Node builtins + the `docker` CLI — no workspace deps.
//   Run:  node server/clickhouse/scripts/seed-costs.mjs
// Idempotent: deletes all @speakeasy.com rows from telemetry_logs AND
// attribute_metrics_summaries first (the MV only appends, so we must clear the
// aggregate too or re-runs double-count), then re-inserts.

import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import { execFileSync } from "node:child_process";

// Resolve container names by substring — compose sometimes recreates them with
// a hash prefix (e.g. 936ee56b0865_gram-clickhouse-1), so a hardcoded name rots.
function resolveContainer(substr, fallback) {
  try {
    const out = execFileSync(
      "docker",
      ["ps", "--filter", `name=${substr}`, "--format", "{{.Names}}"],
      { encoding: "utf8" },
    )
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    return out[0] || fallback;
  } catch {
    return fallback;
  }
}
const CH_CONTAINER = resolveContainer("clickhouse", "gram-clickhouse-1");
const PG_CONTAINER = resolveContainer("gram-gram-db", "gram-gram-db-1");
const EMAIL_DOMAIN = "speakeasy.com";
const DAYS_BACK = 28; // stay under the aggregate's 30-day TTL
const NOW = Date.now();
const MS_PER_DAY = 24 * 60 * 60 * 1000;

// ---- deterministic PRNG (mulberry32) so re-runs are identical -------------
let _s = 0x9e3779b9;
function rnd() {
  _s |= 0;
  _s = (_s + 0x6d2b79f5) | 0;
  let t = Math.imul(_s ^ (_s >>> 15), 1 | _s);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const randint = (lo, hi) => lo + Math.floor(rnd() * (hi - lo + 1));
const pick = (arr) => arr[Math.floor(rnd() * arr.length)];
function weightedPick(items, weights) {
  const total = weights.reduce((a, b) => a + b, 0);
  let r = rnd() * total;
  for (let i = 0; i < items.length; i++) {
    if ((r -= weights[i]) < 0) return items[i];
  }
  return items[items.length - 1];
}

// ---- name pools (no apostrophes — they'd break the SQL string literals) ----
const FIRST = [
  "Adam",
  "Maya",
  "Liam",
  "Sofia",
  "Noah",
  "Priya",
  "Ethan",
  "Olivia",
  "Lucas",
  "Ava",
  "Mateo",
  "Zoe",
  "Kai",
  "Nina",
  "Owen",
  "Aisha",
  "Leo",
  "Ruby",
  "Felix",
  "Hana",
  "Jonas",
  "Elena",
  "Theo",
  "Iris",
  "Diego",
  "Clara",
  "Sven",
  "Mira",
  "Arjun",
  "Lena",
  "Marcus",
  "Yuki",
  "Caleb",
  "Freya",
  "Omar",
  "Isla",
  "Victor",
  "Anya",
  "Dane",
  "Nora",
  "Pablo",
  "Tara",
  "Hugo",
  "Esme",
  "Rafa",
  "Lia",
  "Soren",
  "Mae",
  "Niko",
  "Vera",
];
const LAST = [
  "Reyes",
  "Novak",
  "Chen",
  "Patel",
  "Okafor",
  "Vasquez",
  "Lindqvist",
  "Haddad",
  "Suzuki",
  "Moreau",
  "Bauer",
  "Costa",
  "Ivanov",
  "Nguyen",
  "Schmidt",
  "Rossi",
  "Khan",
  "Andersen",
  "Silva",
  "Kowalski",
  "Mensah",
  "Park",
  "Dubois",
  "Romano",
  "Becker",
  "Sato",
  "Lopez",
  "Fischer",
  "Tanaka",
  "Weber",
  "Marsh",
  "Cole",
  "Frost",
  "Vance",
  "Holt",
  "Quinn",
  "Drake",
  "Sloane",
  "Beck",
  "Wren",
];

// ---- org model: divisions -> departments -> teams --------------------------
// Each department: division, cost center, IC titles, manager title, the agent
// surfaces its people lean on (weighted, first = primary), role pool, and the
// rough session intensity of its ICs (drives cost distribution + top_n).
const DEPARTMENTS = [
  {
    name: "Engineering",
    division: "R&D",
    cc: "CC-ENG-1000",
    teams: ["Platform", "SDK Generation", "Gram / MCP", "Infrastructure"],
    icTitles: [
      "Software Engineer",
      "Senior Software Engineer",
      "Staff Engineer",
    ],
    mgrTitle: "Engineering Manager",
    agents: ["claude-code", "cursor", "codex"],
    agentW: [60, 30, 10],
    roles: ["developer"],
    intensity: [25, 60],
    tokenScale: 2.2,
    icPerTeam: [7, 11],
  },
  {
    name: "Product",
    division: "R&D",
    cc: "CC-PRD-1100",
    teams: ["Core Product", "Growth"],
    icTitles: ["Product Manager", "Senior Product Manager"],
    mgrTitle: "Director of Product",
    agents: ["claude-code", "cowork"],
    agentW: [55, 45],
    roles: ["analyst", "viewer"],
    intensity: [12, 30],
    tokenScale: 1.2,
    icPerTeam: [4, 6],
  },
  {
    name: "Design",
    division: "R&D",
    cc: "CC-DSN-1200",
    teams: ["Product Design", "Brand"],
    icTitles: ["Product Designer", "Brand Designer"],
    mgrTitle: "Design Lead",
    agents: ["cowork", "claude-code"],
    agentW: [70, 30],
    roles: ["viewer"],
    intensity: [8, 22],
    tokenScale: 1.0,
    icPerTeam: [3, 5],
  },
  {
    name: "Developer Relations",
    division: "Go-To-Market",
    cc: "CC-DRL-2000",
    teams: ["DevRel", "Docs"],
    icTitles: ["Developer Advocate", "Technical Writer"],
    mgrTitle: "Head of DevRel",
    agents: ["claude-code", "cowork"],
    agentW: [55, 45],
    roles: ["developer", "viewer"],
    intensity: [20, 45],
    tokenScale: 1.6,
    icPerTeam: [3, 5],
  },
  {
    name: "Sales",
    division: "Go-To-Market",
    cc: "CC-SAL-2100",
    teams: ["Enterprise", "Mid-Market", "SDR"],
    icTitles: ["Account Executive", "Sales Development Rep"],
    mgrTitle: "Sales Manager",
    agents: ["cowork", "cursor"],
    agentW: [80, 20],
    roles: ["viewer"],
    intensity: [6, 20],
    tokenScale: 0.8,
    icPerTeam: [5, 8],
  },
  {
    name: "Marketing",
    division: "Go-To-Market",
    cc: "CC-MKT-2200",
    teams: ["Demand Gen", "Content"],
    icTitles: ["Marketing Manager", "Content Strategist"],
    mgrTitle: "Head of Marketing",
    agents: ["cowork"],
    agentW: [100],
    roles: ["viewer", "analyst"],
    intensity: [8, 24],
    tokenScale: 1.0,
    icPerTeam: [3, 5],
  },
  {
    name: "Customer Success",
    division: "Operations",
    cc: "CC-CS-3000",
    teams: ["Onboarding", "Support"],
    icTitles: ["Customer Success Manager", "Support Engineer"],
    mgrTitle: "CS Manager",
    agents: ["claude-code", "cowork"],
    agentW: [45, 55],
    roles: ["viewer"],
    intensity: [10, 28],
    tokenScale: 1.1,
    icPerTeam: [4, 7],
  },
  {
    name: "Finance & Ops",
    division: "Operations",
    cc: "CC-FIN-3100",
    teams: ["Finance", "RevOps"],
    icTitles: ["Financial Analyst", "Operations Analyst"],
    mgrTitle: "Finance Manager",
    agents: ["cowork"],
    agentW: [100],
    roles: ["billing", "viewer"],
    intensity: [6, 18],
    tokenScale: 0.9,
    icPerTeam: [3, 4],
  },
  {
    name: "People",
    division: "Operations",
    cc: "CC-PPL-3200",
    teams: ["Recruiting", "People Ops"],
    icTitles: ["Recruiter", "People Partner"],
    mgrTitle: "Head of People",
    agents: ["cowork"],
    agentW: [100],
    roles: ["viewer"],
    intensity: [5, 16],
    tokenScale: 0.8,
    icPerTeam: [2, 4],
  },
];

const EMP_TYPES = ["full_time", "contractor", "part_time"];
const EMP_WEIGHTS = [85, 11, 4];
const MODELS = [
  ["claude-sonnet-4-6", "anthropic"],
  ["claude-haiku-4-5", "anthropic"],
  ["claude-opus-4-8", "anthropic"],
  ["gpt-4o", "openai"],
  ["gpt-4o-mini", "openai"],
];
const MODEL_WEIGHTS = [40, 22, 8, 18, 12];
const TOOLS = [
  "tools:http:github:create_issue",
  "tools:http:github:list_pull_requests",
  "tools:http:slack:send_message",
  "tools:http:linear:create_issue",
  "tools:http:postgres:run_query",
  "tools:http:stripe:create_charge",
  "tools:functions:gram:generate_sdk",
  "tools:http:notion:create_page",
  "tools:http:datadog:search_logs",
];

// ---- build the people ------------------------------------------------------
const usedEmails = new Set();
function emailFor(first, last) {
  let base = `${first}.${last}`.toLowerCase().replace(/[^a-z.]/g, "");
  let e = `${base}@${EMAIL_DOMAIN}`;
  let n = 1;
  while (usedEmails.has(e)) e = `${base}${++n}@${EMAIL_DOMAIN}`;
  usedEmails.add(e);
  return e;
}
let nameCursor = 0;
function nextName() {
  const f = FIRST[nameCursor % FIRST.length];
  const l =
    LAST[
      (Math.floor(nameCursor / FIRST.length) % LAST.length) + (nameCursor % 3)
    ];
  nameCursor++;
  return [f, l];
}

const people = []; // {name,email,division,department,team,title,empType,roles,groups,agents,agentW,manager,intensity,tokenScale}
let uid = 0;
function addPerson(p) {
  p.userId = `sk-user-${uid++}`;
  people.push(p);
  return p;
}

// CEO (the logged-in user) — top of the tree.
const ceo = addPerson({
  name: "Adam Bull",
  email: `adam@${EMAIL_DOMAIN}`,
  division: "Executive",
  department: "Executive",
  team: "Leadership",
  title: "CEO / Co-Founder",
  empType: "full_time",
  roles: ["admin", "billing"],
  groups: ["Leadership"],
  agents: ["claude-code", "cursor"],
  agentW: [70, 30],
  managerEmail: "",
  managerName: "",
  intensity: [8, 18],
  tokenScale: 1.5,
});
usedEmails.add(ceo.email);

// One VP per division, reporting to the CEO. VP department = flagship dept.
const DIVISIONS = ["R&D", "Go-To-Market", "Operations"];
const flagshipDept = {
  "R&D": "Engineering",
  "Go-To-Market": "Sales",
  Operations: "Finance & Ops",
};
const vpByDivision = {};
for (const div of DIVISIONS) {
  const [f, l] = nextName();
  const dept = DEPARTMENTS.find((d) => d.name === flagshipDept[div]);
  const vp = addPerson({
    name: `${f} ${l}`,
    email: emailFor(f, l),
    division: div,
    department: dept.name,
    team: "Leadership",
    title: `VP of ${div === "R&D" ? "Engineering" : div === "Go-To-Market" ? "Go-To-Market" : "Operations"}`,
    empType: "full_time",
    roles: ["admin"],
    groups: ["Leadership"],
    agents: dept.agents,
    agentW: dept.agentW,
    managerEmail: ceo.email,
    managerName: ceo.name,
    intensity: [6, 14],
    tokenScale: 1.3,
  });
  vpByDivision[div] = vp;
}

// Team managers + ICs per department.
for (const dept of DEPARTMENTS) {
  const vp = vpByDivision[dept.division];
  for (const team of dept.teams) {
    const [mf, ml] = nextName();
    const mgr = addPerson({
      name: `${mf} ${ml}`,
      email: emailFor(mf, ml),
      division: dept.division,
      department: dept.name,
      team,
      title: dept.mgrTitle,
      empType: "full_time",
      roles: [...new Set(["admin", ...dept.roles])],
      groups: [team],
      agents: dept.agents,
      agentW: dept.agentW,
      managerEmail: vp.email,
      managerName: vp.name,
      intensity: [8, 20],
      tokenScale: dept.tokenScale * 0.9,
    });
    const icCount = randint(dept.icPerTeam[0], dept.icPerTeam[1]);
    for (let i = 0; i < icCount; i++) {
      const [f, l] = nextName();
      const empType = weightedPick(EMP_TYPES, EMP_WEIGHTS);
      const roles = [...new Set([...dept.roles, "viewer"])];
      addPerson({
        name: `${f} ${l}`,
        email: emailFor(f, l),
        division: dept.division,
        department: dept.name,
        team,
        title: pick(dept.icTitles),
        empType,
        roles,
        groups: [team],
        agents: dept.agents,
        agentW: dept.agentW,
        managerEmail: mgr.email,
        managerName: mgr.name,
        intensity: dept.intensity,
        tokenScale: dept.tokenScale,
      });
    }
  }
}

// ---- generate telemetry rows ----------------------------------------------
const projectIds = discoverProjectIds();
const projectWeights = projectIds.map((_, i) =>
  i === 0 ? 70 : 30 / (projectIds.length - 1 || 1),
);

function uuidv5(name) {
  const h = crypto
    .createHash("sha1")
    .update("speakeasy-costs-ns")
    .update(name)
    .digest();
  h[6] = (h[6] & 0x0f) | 0x50;
  h[8] = (h[8] & 0x3f) | 0x80;
  const x = h.toString("hex").slice(0, 32);
  return `${x.slice(0, 8)}-${x.slice(8, 12)}-${x.slice(12, 16)}-${x.slice(16, 20)}-${x.slice(20, 32)}`;
}
const sql = (s) => String(s).replace(/'/g, "''");
function attrJSON(obj) {
  return sql(JSON.stringify(obj));
}

// Fake chat titles so the seeded Postgres chats (and the agent-sessions list)
// read like real sessions rather than blank rows.
const TITLES = [
  "Debugging a failing test",
  "Refactor request",
  "Feature scaffolding",
  "Code review",
  "Data analysis",
  "API integration",
  "Test generation",
  "Writing docs",
  "Incident triage",
  "Schema migration",
];

let chatSeq = 0;
let totalChats = 0;
const rows = [];
// Postgres chat records mirroring the telemetry gram_chat_id values, so the
// per-session detail view (chat.load, project-scoped) resolves a real chat.
const chatRecords = [];

for (const p of people) {
  // Real directory syncs are incomplete: a slice of people are missing one or
  // more attributes. Blanking them here populates the "(unset)" bucket in every
  // breakdown (scalar → '', array → []) so the now-drillable "(unset)" rows have
  // real cost behind them. Keep the CEO (the logged-in user) fully populated.
  const full = p === ceo;
  const orBlank = (prob, value) => (!full && rnd() < prob ? "" : value);
  const orEmpty = (prob, value) => (!full && rnd() < prob ? [] : value);
  const costCenter =
    (DEPARTMENTS.find((d) => d.name === p.department) || {}).cc ||
    "CC-EXEC-0000";

  // Shared WorkOS-style attribute block for every row this user emits.
  const ua = {
    "user.email": p.email,
    "user.id": p.userId,
    "user.attributes.division_name": orBlank(0.06, p.division),
    "user.attributes.department_name": orBlank(0.08, p.department),
    "user.attributes.team_name": p.team, // not a dimension yet (deferred)
    "user.attributes.job_title": orBlank(0.18, p.title),
    "user.attributes.employee_type": orBlank(0.1, p.empType),
    "user.attributes.cost_center_name": orBlank(0.1, costCenter),
    "user.attributes.manager_email": p.managerEmail, // deferred dim, stamped for later
    "user.attributes.manager_name": p.managerName,
    "user.roles": orEmpty(0.12, p.roles), // [] → "(unset)" on the Role axis
    "user.groups": orEmpty(0.15, p.groups), // == team; [] → "(unset)" Team
  };

  const sessions = randint(p.intensity[0], p.intensity[1]);
  for (let s = 0; s < sessions; s++) {
    totalChats++;
    const chatId = uuidv5(`chat-${chatSeq++}`);
    const projectId = weightedPick(projectIds, projectWeights);
    const hookSource = weightedPick(p.agents, p.agentW);
    const daysAgo = rnd() * DAYS_BACK;
    const tsMs = Math.floor(NOW - daysAgo * MS_PER_DAY);
    const t = BigInt(tsMs) * 1000000n;
    chatRecords.push({
      id: chatId,
      projectId,
      userId: p.userId,
      title: pick(TITLES),
      tsMs,
    });
    const traceId = crypto.randomBytes(16).toString("hex");
    const baseAttrs = {
      ...ua,
      "gram.hook.source": hookSource,
      "gram.project.id": projectId,
    };

    // 1) tool-call row(s) — drives total_tool_calls (urn must start "tools:")
    const nTools = randint(1, 4);
    for (let k = 0; k < nTools; k++) {
      const toolUrn = pick(TOOLS);
      const status = rnd() < 0.93 ? 200 : pick([400, 500, 502]);
      const latency = (0.05 + rnd() * 2).toFixed(3);
      const a = {
        "http.response.status_code": status,
        "http.server.request.duration": Number(latency),
        "gram.tool.urn": toolUrn,
        "gen_ai.conversation.id": chatId,
        ...baseAttrs,
      };
      const tt = t + BigInt(k * 1000);
      rows.push(
        `(${tt}, ${tt}, 'INFO', 'Tool call: ${sql(toolUrn)}', '${traceId}', '${attrJSON(a)}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', '${sql(toolUrn)}', 'gram-mcp-gateway', '${chatId}')`,
      );
    }

    // 2) chat-completion row — drives cost + token measures + total_chats
    const [model, provider] = weightedPick(MODELS, MODEL_WEIGHTS);
    const scale = p.tokenScale;
    const inputTokens = Math.floor((500 + rnd() * 7500) * scale);
    const outputTokens = Math.floor((100 + rnd() * 2900) * scale);
    const cacheRead = Math.floor(inputTokens * (rnd() * 0.5));
    const cacheCreate = Math.floor(inputTokens * (rnd() * 0.15));
    const cost = ((inputTokens * 3 + outputTokens * 15) / 1_000_000).toFixed(6);
    const finish = rnd() < 0.7 ? "stop" : rnd() < 0.9 ? "length" : "error";
    const duration = 30 + Math.floor(rnd() * 150);
    const ct = t + 1000000n;
    const ca = {
      "gen_ai.response.finish_reasons": [finish],
      "gen_ai.conversation.id": chatId,
      "gen_ai.conversation.duration": duration,
      "gen_ai.usage.input_tokens": inputTokens,
      "gen_ai.usage.output_tokens": outputTokens,
      "gen_ai.usage.total_tokens": inputTokens + outputTokens,
      "gen_ai.usage.cache_read.input_tokens": cacheRead,
      "gen_ai.usage.cache_creation.input_tokens": cacheCreate,
      "gen_ai.usage.cost": Number(cost),
      "gen_ai.response.model": model,
      "gen_ai.provider.name": provider,
      "gram.resource.urn": "agents:chat:completion",
      "http.response.status_code": rnd() < 0.95 ? 200 : 500,
      ...baseAttrs,
    };
    rows.push(
      `(${ct}, ${ct}, 'INFO', 'Chat completion', '${traceId}', '${attrJSON(ca)}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'agents:chat:completion', 'gram-mcp-gateway', '${chatId}')`,
    );
  }
}

// ---- flush to ClickHouse ---------------------------------------------------
const CLEANUP = [
  "SET mutations_sync = 1;",
  `ALTER TABLE telemetry_logs DELETE WHERE user_email LIKE '%@${EMAIL_DOMAIN}';`,
  `ALTER TABLE attribute_metrics_summaries DELETE WHERE user_email LIKE '%@${EMAIL_DOMAIN}';`,
];
const COLS =
  "(time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id, attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)";

const parts = [...CLEANUP];
const CHUNK = 2000;
for (let i = 0; i < rows.length; i += CHUNK) {
  parts.push(
    `INSERT INTO telemetry_logs ${COLS} VALUES\n${rows.slice(i, i + CHUNK).join(",\n")};`,
  );
}

const tmp = `${os.tmpdir()}/seed_costs_${process.pid}.sql`;
fs.writeFileSync(tmp, parts.join("\n"));
console.log(
  `Generated ${people.length} users across ${DEPARTMENTS.length} departments / ${DIVISIONS.length} divisions, ` +
    `${totalChats} chat sessions → ${rows.length} telemetry rows.`,
);
console.log(`Target org projects: ${projectIds.join(", ")}`);

try {
  execFileSync("docker", ["cp", tmp, `${CH_CONTAINER}:/tmp/seed_costs.sql`], {
    stdio: "inherit",
  });
  execFileSync(
    "docker",
    [
      "exec",
      CH_CONTAINER,
      "clickhouse-client",
      "--multiquery",
      "--queries-file",
      "/tmp/seed_costs.sql",
    ],
    { stdio: "inherit" },
  );
  fs.unlinkSync(tmp);
  console.log("Inserted into ClickHouse. Verifying aggregate…");
  verify();
} catch (e) {
  console.error("ClickHouse insert failed:", e.message);
  console.error(`SQL kept at ${tmp} for inspection.`);
  process.exit(1);
}

// Mirror the telemetry sessions into Postgres `chats` so clicking a session in
// the cost dashboard resolves a real chat (the detail view is project-scoped).
try {
  seedChats();
} catch (e) {
  console.error("Postgres chats seed failed:", e.message);
  process.exit(1);
}

// ---- helpers that shell out ------------------------------------------------
function discoverProjectIds() {
  const q =
    "SELECT id FROM projects WHERE organization_id = " +
    "(SELECT organization_id FROM projects GROUP BY organization_id ORDER BY count(*) DESC LIMIT 1) ORDER BY id;";
  const out = execFileSync(
    "docker",
    ["exec", PG_CONTAINER, "psql", "-U", "gram", "-d", "gram", "-tA", "-c", q],
    {
      encoding: "utf8",
    },
  );
  const ids = out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
  if (!ids.length) {
    console.error(
      "No projects found in local Postgres — run `mise run seed` first.",
    );
    process.exit(1);
  }
  return ids;
}

// The organization_id that owns a discovered project — needed for the (NOT NULL)
// chats.organization_id column. Derived from one of the project ids we actually
// seed (not a second independent "top org" query), so a tie in project counts
// can't drift the org away from the projects the chats reference.
function discoverOrgId(projectId) {
  const q = `SELECT organization_id FROM projects WHERE id = '${sql(projectId)}' LIMIT 1;`;
  const out = execFileSync(
    "docker",
    ["exec", PG_CONTAINER, "psql", "-U", "gram", "-d", "gram", "-tA", "-c", q],
    { encoding: "utf8" },
  );
  const id = out.trim().split("\n")[0]?.trim();
  if (!id) {
    console.error("No organization found for project in local Postgres.");
    process.exit(1);
  }
  return id;
}

// Insert a chats row per seeded session (id == telemetry gram_chat_id) so the
// dashboard's project-scoped chat.load resolves. Deterministic ids → ON CONFLICT
// keeps re-runs idempotent. Messages aren't seeded; the detail view still shows
// the session's telemetry logs (searchLogs keys on gram_chat_id).
function seedChats() {
  const orgId = discoverOrgId(projectIds[0]);
  const values = chatRecords.map((c) => {
    const iso = new Date(c.tsMs).toISOString();
    return `('${c.id}', '${c.projectId}', '${sql(orgId)}', '${sql(c.userId)}', '${sql(c.title)}', '${iso}', '${iso}')`;
  });

  const stmts = [];
  const CHUNK_CHATS = 1000;
  for (let i = 0; i < values.length; i += CHUNK_CHATS) {
    stmts.push(
      `INSERT INTO chats (id, project_id, organization_id, user_id, title, created_at, updated_at) VALUES\n` +
        `${values.slice(i, i + CHUNK_CHATS).join(",\n")}\n` +
        `ON CONFLICT (id) DO UPDATE SET project_id = EXCLUDED.project_id, ` +
        `organization_id = EXCLUDED.organization_id, user_id = EXCLUDED.user_id, ` +
        `title = EXCLUDED.title, created_at = EXCLUDED.created_at, ` +
        `updated_at = EXCLUDED.updated_at;`,
    );
  }

  const tmp2 = `${os.tmpdir()}/seed_costs_chats_${process.pid}.sql`;
  fs.writeFileSync(tmp2, stmts.join("\n"));
  execFileSync(
    "docker",
    ["cp", tmp2, `${PG_CONTAINER}:/tmp/seed_costs_chats.sql`],
    { stdio: "inherit" },
  );
  execFileSync(
    "docker",
    [
      "exec",
      PG_CONTAINER,
      "psql",
      "-U",
      "gram",
      "-d",
      "gram",
      "-v",
      "ON_ERROR_STOP=1",
      "-f",
      "/tmp/seed_costs_chats.sql",
    ],
    { stdio: "inherit" },
  );
  fs.unlinkSync(tmp2);
  console.log(
    `Inserted ${chatRecords.length} chats into Postgres (org ${orgId}).`,
  );
}

function verify() {
  const q =
    "SELECT division_name, department_name, uniqExact(user_email) AS users, " +
    "round(sumIfMerge(total_cost), 2) AS cost " +
    `FROM attribute_metrics_summaries WHERE user_email LIKE '%@${EMAIL_DOMAIN}' ` +
    "GROUP BY division_name, department_name ORDER BY division_name, department_name " +
    "FORMAT PrettyCompactMonoBlock;";
  const out = execFileSync(
    "docker",
    ["exec", CH_CONTAINER, "clickhouse-client", "-q", q],
    { encoding: "utf8" },
  );
  console.log(out);
}
