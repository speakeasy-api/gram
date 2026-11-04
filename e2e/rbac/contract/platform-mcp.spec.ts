import fs from "node:fs";
import path from "node:path";
import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
} from "@playwright/test";
import { ApiClient } from "../lib/api.ts";
import { DevIdp } from "../lib/devidp.ts";
import {
  connectPlatformMcp,
  mcpOutcome,
  PlatformMcpClient,
  revokePlatformMcp,
  toolErrorText,
  type Connection,
} from "../lib/platformmcp.ts";
import {
  credentialFor,
  lazyWorld,
  PERSONAS,
  USER_PERSONAS,
  type Persona,
  type World,
} from "../lib/personas.ts";

// Golden matrix for the Platform MCP, the way an MCP client reaches it: every
// person connects through the real OAuth flow, then lists tools and calls a
// fixed set of them. Three things are pinned per persona: whether the
// connection is granted, which tools it is offered, and whether each call is
// allowed or refused. Organization API keys and no credential are tried as
// bearer tokens too, and must be turned away.
//
// Like the read sweep, the baseline is recorded from the stack, compared by
// outcome class, and re-recorded with RBAC_E2E_UPDATE_BASELINE=1 after an
// intended change.

type CallSpec = {
  tool: string;
  args: (world: World) => Record<string, unknown>;
};

// Reads that cover organization-level, project-level and access decisions,
// with the project-level ones asked of both projects so a persona bounded to
// one project shows the difference.
const CALLS: Record<string, CallSpec> = {
  whoami: { tool: "whoami", args: () => ({}) },
  get_platform_context: { tool: "get_platform_context", args: () => ({}) },
  list_projects: { tool: "list_projects", args: () => ({}) },
  list_access_roles: { tool: "list_access_roles", args: () => ({}) },
  list_access_members: {
    tool: "list_access_members",
    args: () => ({ query: "rbac-e2e" }),
  },
  list_risk_policies: { tool: "list_risk_policies", args: () => ({}) },
  list_chats: { tool: "list_chats", args: () => ({}) },
  "get_project_overview (default)": {
    tool: "get_project_overview",
    args: (w) => ({ project_id: w.projectId }),
  },
  "get_project_overview (sandbox)": {
    tool: "get_project_overview",
    args: (w) => ({ project_id: w.sandboxProjectId }),
  },
  "find_mcp (default)": {
    tool: "find_mcp",
    args: (w) => ({ project_slug: w.projectSlug }),
  },
  "find_mcp (sandbox)": {
    tool: "find_mcp",
    args: (w) => ({ project_slug: w.sandboxProjectSlug }),
  },
  "list_skills (default)": {
    tool: "list_skills",
    args: (w) => ({ project_slug: w.projectSlug }),
  },
  "list_plugins (default)": {
    tool: "list_plugins",
    args: (w) => ({ project_id: w.projectId }),
  },
};
const WRITE_LABEL = "rename_project (fresh project)";
const LISTED = "tools/list";

type Baseline = {
  connect: Partial<Record<Persona, string>>;
  tools: Partial<Record<Persona, string[]>>;
  calls: Record<string, Partial<Record<Persona, string>>>;
};

const baselineFile = path.join(
  import.meta.dirname,
  "platform-mcp.baseline.json",
);
const updating = process.env["RBAC_E2E_UPDATE_BASELINE"] === "1";
const baseline: Baseline | undefined = fs.existsSync(baselineFile)
  ? (JSON.parse(fs.readFileSync(baselineFile, "utf8")) as Baseline)
  : undefined;
const recorded: Baseline = { connect: {}, tools: {}, calls: {} };

const world = lazyWorld();
const connections = {} as Record<Persona, Connection>;
let ctx: APIRequestContext;

function bearerFor(persona: Persona): string | undefined {
  if (persona === "anonymous") return undefined;
  if (persona === "consumerKey" || persona === "producerKey") {
    return world.keys[persona].key;
  }
  return connections[persona].token;
}

async function clientFor(persona: Persona): Promise<PlatformMcpClient> {
  const client = new PlatformMcpClient(ctx, bearerFor(persona));
  await client.initialize();
  return client;
}

function compare(
  label: string,
  observed: Partial<Record<Persona, string>>,
  expected: Partial<Record<Persona, string>> | undefined,
  detail: Partial<Record<Persona, string>>,
) {
  expect(
    expected,
    `${label} has no baseline row; re-record with RBAC_E2E_UPDATE_BASELINE=1`,
  ).toBeDefined();
  expect(observed, `messages: ${JSON.stringify(detail)}`).toEqual(expected);
}

test.describe("platform mcp", () => {
  test.beforeAll(async () => {
    ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
    const idp = new DevIdp(ctx);
    try {
      for (const persona of USER_PERSONAS) {
        const user = await idp.findUser(world.users[persona].email);
        if (!user) throw new Error(`dev-idp has no user for ${persona}`);
        await idp.setCurrentUser(user.id);
        // A fresh cookie jar per persona, so no sign-in carries over.
        const own = await playwrightRequest.newContext({
          ignoreHTTPSErrors: true,
        });
        try {
          connections[persona] = await connectPlatformMcp(own);
        } finally {
          await own.dispose();
        }
      }
    } finally {
      await idp.clearCurrentUser();
    }
  });

  test.afterAll(async () => {
    for (const persona of USER_PERSONAS) {
      if (connections[persona]) {
        await revokePlatformMcp(ctx, connections[persona]);
      }
    }
    await ctx?.dispose();
    if (!updating) return;
    const missing = [LISTED, WRITE_LABEL, ...Object.keys(CALLS)].filter(
      (l) => !(l in recorded.calls),
    );
    if (missing.length > 0 || Object.keys(recorded.connect).length === 0) {
      throw new Error(
        `baseline not written: ${missing.length} rows were not recorded`,
      );
    }
    fs.writeFileSync(baselineFile, `${JSON.stringify(recorded, null, 2)}\n`);
  });

  test("connecting", () => {
    const observed = Object.fromEntries(
      USER_PERSONAS.map((p) => [p, connections[p].outcome]),
    );
    if (updating) {
      recorded.connect = observed;
      return;
    }
    compare("connecting", observed, baseline?.connect, {});
  });

  test("tools offered", async () => {
    const observed: Partial<Record<Persona, string>> = {};
    const names: Partial<Record<Persona, string[]>> = {};
    const detail: Partial<Record<Persona, string>> = {};
    for (const persona of PERSONAS) {
      const client = await clientFor(persona);
      const res = await client.rpc("tools/list");
      observed[persona] = mcpOutcome(res);
      if (observed[persona] === "ok") {
        names[persona] = (res.answer?.result?.tools ?? [])
          .map((t) => t.name)
          .sort();
      } else {
        detail[persona] = toolErrorText(res.answer) || res.text.slice(0, 120);
      }
    }
    if (updating) {
      recorded.calls[LISTED] = observed;
      recorded.tools = names;
      return;
    }
    compare(LISTED, observed, baseline?.calls[LISTED], detail);
    // Name the tools that appeared or disappeared, persona by persona, rather
    // than dumping two long lists.
    const drift: Record<string, { added: string[]; removed: string[] }> = {};
    for (const [persona, list] of Object.entries(names)) {
      const before = new Set(baseline?.tools[persona as Persona] ?? []);
      const now = new Set(list);
      const added = list.filter((n) => !before.has(n));
      const removed = [...before].filter((n) => !now.has(n));
      if (added.length || removed.length) drift[persona] = { added, removed };
    }
    expect(drift, "tool exposure changed").toEqual({});
  });

  for (const [label, spec] of Object.entries(CALLS)) {
    test(label, async () => {
      const observed: Partial<Record<Persona, string>> = {};
      const detail: Partial<Record<Persona, string>> = {};
      for (const persona of PERSONAS) {
        const client = await clientFor(persona);
        const res = await client.rpc("tools/call", {
          name: spec.tool,
          arguments: spec.args(world),
        });
        observed[persona] = mcpOutcome(res);
        if (observed[persona] !== "ok") {
          detail[persona] = toolErrorText(res.answer) || res.text.slice(0, 120);
        }
      }
      if (updating) {
        recorded.calls[label] = observed;
        return;
      }
      compare(label, observed, baseline?.calls[label], detail);
    });
  }

  // One write, against a project made for each persona and deleted after, so
  // an allowed rename changes nothing anyone else sees.
  test(WRITE_LABEL, async ({ request }) => {
    const admin = new ApiClient(request, credentialFor(world, "admin"));
    const observed: Partial<Record<Persona, string>> = {};
    const detail: Partial<Record<Persona, string>> = {};
    for (const persona of PERSONAS) {
      const created = await admin.ok<{ project: { id: string } }>(
        "POST",
        "/rpc/projects.create",
        {
          project: null,
          body: {
            organization_id: world.orgId,
            name: `rbac-e2e-rn-${persona}-${Date.now().toString(36)}`,
          },
        },
      );
      try {
        const client = await clientFor(persona);
        const res = await client.rpc("tools/call", {
          name: "rename_project",
          arguments: {
            project_id: created.project.id,
            name: `rbac-e2e-renamed-${persona}`,
            idempotency_key: `rbac-e2e-${created.project.id}`,
            confirmed: true,
          },
        });
        observed[persona] = mcpOutcome(res);
        if (observed[persona] !== "ok") {
          detail[persona] = toolErrorText(res.answer) || res.text.slice(0, 120);
        }
      } finally {
        await admin.call("DELETE", "/rpc/projects.delete", {
          project: null,
          query: { id: created.project.id },
        });
      }
    }
    if (updating) {
      recorded.calls[WRITE_LABEL] = observed;
      return;
    }
    compare(WRITE_LABEL, observed, baseline?.calls[WRITE_LABEL], detail);
  });
});
