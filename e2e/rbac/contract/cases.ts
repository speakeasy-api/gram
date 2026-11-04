import { expect, type APIResponse } from "@playwright/test";
import type { ApiClient, Method } from "../lib/api.ts";
import type { Persona, World } from "../lib/personas.ts";

// The authorization contract, one row per (endpoint, situation). Each row says
// which status every listed persona must get. Personas a row leaves out are not
// called for it.
//
// Adding coverage for an endpoint is adding a row here; the spec that runs the
// rows never needs to change. Rows that mutate state use `prepare` to create
// whatever they act on (as admin, fresh for every persona, so one persona's
// call cannot change what the next one sees) and `cleanup` to remove it.

export type Slice = "organization" | "projects" | "mcp" | "agents" | "access";

export type Fixture = Record<string, string>;

export type CaseContext = {
  world: World;
  admin: ApiClient;
  unique: (prefix: string) => string;
};

export type ContractCase = {
  slice: Slice;
  name: string;
  method: Method;
  path: string;
  // Which project the `Gram-Project` header names. Defaults to the seeded
  // default project; `null` sends no header.
  project?: "default" | "sandbox" | null;
  prepare?: (ctx: CaseContext) => Promise<Fixture>;
  request?: (
    ctx: CaseContext,
    fx: Fixture,
  ) => { query?: Record<string, string>; body?: unknown };
  // Runs after every call, whatever its status. `res` is the persona's answer,
  // so a cleanup can remove what an allowed create made.
  cleanup?: (ctx: CaseContext, fx: Fixture, res: APIResponse) => Promise<void>;
  expect: Partial<Record<Persona, number>>;
  // Present the credential as a bearer token (MCP gateway calls).
  bearer?: boolean;
  // Extra assertions on an answer whose status alone does not say enough,
  // such as a JSON-RPC result carried in a 200.
  check?: (persona: Persona, status: number, body: unknown) => void;
  // Why the row exists or why an expectation is surprising.
  note?: string;
};

const deny = 403;
const unauthenticated = 401;
// A project-scoped call from someone with no grant on the project at all is
// turned away while the project header is resolved, before any scope check,
// and answers 401 rather than 403.
const noProjectAccess = 401;

// Most management endpoints answer a credential that is not a session, or no
// credential at all, the same way.
const sessionOnly = {
  consumerKey: unauthenticated,
  producerKey: unauthenticated,
  anonymous: unauthenticated,
} as const;

async function bodyOf<T>(res: APIResponse): Promise<T | undefined> {
  if (!res.ok()) return undefined;
  try {
    return (await res.json()) as T;
  } catch {
    return undefined;
  }
}

// --- fixture helpers -------------------------------------------------------

async function createToolset(
  ctx: CaseContext,
  project: string,
): Promise<Fixture> {
  const ts = await ctx.admin.ok<{ id: string; slug: string }>(
    "POST",
    "/rpc/toolsets.create",
    {
      project,
      body: { name: ctx.unique("rbac-e2e-toolset") },
    },
  );
  return { toolsetId: ts.id, toolsetSlug: ts.slug, project };
}

async function deleteToolset(ctx: CaseContext, fx: Fixture): Promise<void> {
  if (!fx["toolsetSlug"]) return;
  await ctx.admin.call("DELETE", "/rpc/toolsets.delete", {
    project: fx["project"] ?? ctx.world.projectSlug,
    query: { slug: fx["toolsetSlug"] },
  });
}

async function deleteCreatedToolset(
  ctx: CaseContext,
  project: string,
  res: APIResponse,
) {
  const created = await bodyOf<{ slug: string }>(res);
  if (created) await deleteToolset(ctx, { toolsetSlug: created.slug, project });
}

async function createThrowawayProject(ctx: CaseContext): Promise<Fixture> {
  const created = await ctx.admin.ok<{ project: { id: string; slug: string } }>(
    "POST",
    "/rpc/projects.create",
    {
      project: null,
      body: {
        organization_id: ctx.world.orgId,
        name: ctx.unique("rbac-e2e-tmp"),
      },
    },
  );
  return { projectId: created.project.id, projectSlug: created.project.slug };
}

async function deleteProject(
  ctx: CaseContext,
  id: string | undefined,
): Promise<void> {
  if (!id) return;
  await ctx.admin.call("DELETE", "/rpc/projects.delete", {
    project: null,
    query: { id },
  });
}

// The seeded private MCP server backed by an HTTP toolset; its tools call an
// upstream API the local stack does not run, so a permitted tools/call reaches
// execution and fails there.
const SUPPORT_ENDPOINT = "/mcp/speakeasy-support";
const SUPPORT_TOOLSET_ID = "10ca1000-0000-4000-a000-000000005e01";

function jsonRpc(method: string, params?: unknown) {
  return {
    jsonrpc: "2.0",
    id: 1,
    method,
    ...(params === undefined ? {} : { params }),
  };
}

type JsonRpcAnswer = {
  result?: { tools?: { name: string }[] };
  error?: { code: number; message: string };
};

async function createMcpServer(ctx: CaseContext): Promise<Fixture> {
  const ts = await createToolset(ctx, ctx.world.projectSlug);
  const server = await ctx.admin.ok<{ id: string }>(
    "POST",
    "/rpc/mcpServers.create",
    {
      body: {
        name: ctx.unique("rbac-e2e-server"),
        visibility: "private",
        toolset_id: ts["toolsetId"],
      },
    },
  );
  return { ...ts, mcpServerId: server.id };
}

async function deleteMcpServer(ctx: CaseContext, fx: Fixture): Promise<void> {
  if (fx["mcpServerId"]) {
    await ctx.admin.call("DELETE", "/rpc/mcpServers.delete", {
      query: { id: fx["mcpServerId"] },
    });
  }
  await deleteToolset(ctx, fx);
}

// --- the contract ----------------------------------------------------------

export const CASES: ContractCase[] = [
  // Organization administration ---------------------------------------------
  {
    slice: "organization",
    name: "send an invite",
    method: "POST",
    path: "/rpc/organizations.sendInvite",
    project: null,
    request: (ctx) => ({
      body: { email: `${ctx.unique("invitee")}@rbac-e2e.local` },
    }),
    cleanup: async (ctx, _fx, res) => {
      const invite = await bodyOf<{ invitation?: { id: string }; id?: string }>(
        res,
      );
      const id = invite?.invitation?.id ?? invite?.id;
      if (id) {
        await ctx.admin.call("DELETE", "/rpc/organizations.revokeInvite", {
          project: null,
          query: { invitation_id: id },
        });
      }
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      ...sessionOnly,
    },
  },
  {
    slice: "organization",
    name: "create an API key",
    method: "POST",
    path: "/rpc/keys.create",
    project: null,
    request: (ctx) => ({
      body: { name: ctx.unique("rbac-e2e-key"), scopes: ["consumer"] },
    }),
    cleanup: async (ctx, _fx, res) => {
      const key = await bodyOf<{ id: string }>(res);
      if (key) {
        await ctx.admin.call("DELETE", "/rpc/keys.revoke", {
          project: null,
          query: { id: key.id },
        });
      }
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      ...sessionOnly,
    },
  },
  {
    slice: "organization",
    name: "revoke an API key",
    method: "DELETE",
    path: "/rpc/keys.revoke",
    project: null,
    prepare: async (ctx) => {
      const key = await ctx.admin.ok<{ id: string }>(
        "POST",
        "/rpc/keys.create",
        {
          project: null,
          body: { name: ctx.unique("rbac-e2e-revoke"), scopes: ["consumer"] },
        },
      );
      return { keyId: key.id };
    },
    request: (_ctx, fx) => ({ query: { id: fx["keyId"]! } }),
    cleanup: async (ctx, fx) => {
      await ctx.admin.call("DELETE", "/rpc/keys.revoke", {
        project: null,
        query: { id: fx["keyId"]! },
      });
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      ...sessionOnly,
    },
  },

  // Projects ---------------------------------------------------------------
  {
    slice: "projects",
    name: "read the default project",
    note: "A project the caller cannot see answers not-found, so its existence is not disclosed.",
    method: "GET",
    path: "/rpc/projects.get",
    project: null,
    request: (ctx) => ({ query: { slug: ctx.world.projectSlug } }),
    expect: {
      admin: 200,
      member: 200,
      reader: 200,
      blocked: 200,
      norole: 404,
      outsider: 404,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "projects",
    name: "create a project",
    method: "POST",
    path: "/rpc/projects.create",
    project: null,
    request: (ctx) => ({
      body: {
        organization_id: ctx.world.orgId,
        name: ctx.unique("rbac-e2e-new"),
      },
    }),
    cleanup: async (ctx, _fx, res) => {
      const created = await bodyOf<{ project: { id: string } }>(res);
      await deleteProject(ctx, created?.project.id);
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "projects",
    name: "update the default project (blocked persona is blocked here)",
    method: "POST",
    path: "/rpc/projects.update",
    project: "default",
    // Renames to the name it already has, so the call changes nothing.
    request: () => ({ body: { name: "Default" } }),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "projects",
    name: "update the sandbox project (no block applies)",
    method: "POST",
    path: "/rpc/projects.update",
    project: "sandbox",
    request: () => ({ body: { name: "rbac-e2e-sandbox" } }),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "projects",
    name: "delete a project",
    method: "DELETE",
    path: "/rpc/projects.delete",
    project: null,
    prepare: createThrowawayProject,
    request: (_ctx, fx) => ({ query: { id: fx["projectId"]! } }),
    cleanup: async (ctx, fx) => deleteProject(ctx, fx["projectId"]),
    expect: {
      admin: 204,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },

  // MCP servers and toolsets ------------------------------------------------
  {
    slice: "mcp",
    name: "create a toolset in the default project",
    method: "POST",
    path: "/rpc/toolsets.create",
    project: "default",
    request: (ctx) => ({ body: { name: ctx.unique("rbac-e2e-toolset") } }),
    cleanup: (ctx, _fx, res) =>
      deleteCreatedToolset(ctx, ctx.world.projectSlug, res),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 200,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
    note: "project:blocked_write does not cover mcp:write, so the blocked persona may still create toolsets.",
  },
  {
    slice: "mcp",
    name: "read a toolset",
    method: "GET",
    path: "/rpc/toolsets.get",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (_ctx, fx) => ({ query: { slug: fx["toolsetSlug"]! } }),
    cleanup: (ctx, fx) => deleteToolset(ctx, fx),
    expect: {
      admin: 200,
      member: 200,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 200,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "mcp",
    name: "delete a toolset",
    method: "DELETE",
    path: "/rpc/toolsets.delete",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (_ctx, fx) => ({ query: { slug: fx["toolsetSlug"]! } }),
    cleanup: (ctx, fx) => deleteToolset(ctx, fx),
    expect: {
      admin: 204,
      member: deny,
      reader: deny,
      blocked: 204,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 204,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "mcp",
    name: "create a private MCP server",
    method: "POST",
    path: "/rpc/mcpServers.create",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (ctx, fx) => ({
      body: {
        name: ctx.unique("rbac-e2e-server"),
        visibility: "private",
        toolset_id: fx["toolsetId"],
      },
    }),
    cleanup: async (ctx, fx, res) => {
      const created = await bodyOf<{
        id?: string;
        mcp_server?: { id: string };
      }>(res);
      const id = created?.mcp_server?.id ?? created?.id;
      if (id) {
        await ctx.admin.call("DELETE", "/rpc/mcpServers.delete", {
          project: ctx.world.projectSlug,
          query: { id },
        });
      }
      await deleteToolset(ctx, fx);
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 200,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
  },

  {
    slice: "mcp",
    name: "update a toolset",
    method: "POST",
    path: "/rpc/toolsets.update",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (_ctx, fx) => ({
      query: { slug: fx["toolsetSlug"]! },
      body: { description: "updated by the RBAC end-to-end suite" },
    }),
    cleanup: (ctx, fx) => deleteToolset(ctx, fx),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 200,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "mcp",
    name: "clone a toolset",
    method: "POST",
    path: "/rpc/toolsets.clone",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (_ctx, fx) => ({ query: { slug: fx["toolsetSlug"]! } }),
    cleanup: async (ctx, fx, res) => {
      await deleteCreatedToolset(ctx, ctx.world.projectSlug, res);
      await deleteToolset(ctx, fx);
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: unauthenticated,
      consumerKey: unauthenticated,
      anonymous: unauthenticated,
    },
    note: "Cloning is a session-only endpoint, so API keys are not authenticated for it.",
  },
  {
    slice: "mcp",
    name: "update an MCP server",
    method: "POST",
    path: "/rpc/mcpServers.update",
    project: "default",
    prepare: createMcpServer,
    request: (_ctx, fx) => ({
      body: {
        id: fx["mcpServerId"],
        visibility: "private",
        toolset_id: fx["toolsetId"],
      },
    }),
    cleanup: (ctx, fx) => deleteMcpServer(ctx, fx),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: 200,
      norole: noProjectAccess,
      outsider: noProjectAccess,
      producerKey: 200,
      consumerKey: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "mcp",
    name: "MCP client lists tools on a private server",
    method: "POST",
    path: SUPPORT_ENDPOINT,
    project: null,
    bearer: true,
    request: () => ({ body: jsonRpc("tools/list") }),
    check: (_persona, status, body) => {
      if (status !== 200) return;
      const answer = body as JsonRpcAnswer;
      expect(answer.error).toBeUndefined();
      expect(answer.result?.tools?.length ?? 0).toBeGreaterThan(0);
    },
    expect: {
      // A dashboard session is not an MCP credential.
      admin: unauthenticated,
      member: unauthenticated,
      consumerKey: 200,
      producerKey: 200,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "mcp",
    name: "MCP client calls a tool on a private server",
    method: "POST",
    path: SUPPORT_ENDPOINT,
    project: null,
    bearer: true,
    request: () => ({
      body: jsonRpc("tools/call", {
        name: "check_health",
        arguments: { "x-gram-toolset-id": SUPPORT_TOOLSET_ID },
      }),
    }),
    check: (_persona, status, body) => {
      if (status !== 200) return;
      // Authorized calls reach the tool and fail against the absent upstream;
      // a refused one answers with a different error.
      expect((body as JsonRpcAnswer).error?.message).toBe(
        "failed to execute tool call",
      );
    },
    expect: {
      consumerKey: 200,
      producerKey: 200,
      anonymous: unauthenticated,
    },
  },

  // Agents and assistants ---------------------------------------------------
  {
    slice: "agents",
    name: "create an assistant",
    method: "POST",
    path: "/rpc/assistants.create",
    project: "default",
    prepare: (ctx) => createToolset(ctx, ctx.world.projectSlug),
    request: (ctx, fx) => ({
      body: {
        name: ctx.unique("rbac-e2e-assistant"),
        model: "openai/gpt-4o-mini",
        instructions: "Answer briefly.",
        toolsets: [{ toolset_slug: fx["toolsetSlug"] }],
      },
    }),
    cleanup: async (ctx, fx, res) => {
      const created = await bodyOf<{ id?: string; assistant?: { id: string } }>(
        res,
      );
      const id = created?.assistant?.id ?? created?.id;
      if (id) {
        await ctx.admin.call("DELETE", "/rpc/assistants.delete", {
          project: ctx.world.projectSlug,
          query: { id },
        });
      }
      await deleteToolset(ctx, fx);
    },
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "agents",
    name: "register an agent",
    method: "POST",
    path: "/rpc/agents.create",
    project: null,
    request: (ctx) => ({ body: { name: ctx.unique("rbac-e2e-agent") } }),
    expect: {
      admin: 201,
      member: 201,
      reader: 201,
      blocked: 201,
      norole: 201,
      outsider: 201,
      ...sessionOnly,
    },
    note:
      "Any organization member may register an agent they own, so every user persona is allowed. " +
      "Agents have no delete endpoint, so each run leaves these behind under unique names.",
  },

  // Access administration ---------------------------------------------------
  {
    slice: "access",
    name: "create a role",
    method: "POST",
    path: "/rpc/access.createRole",
    project: null,
    request: (ctx) => ({
      body: {
        name: ctx.unique("rbac-e2e-role"),
        grants: [
          {
            scope: "org:read",
            selectors: [{ resource_kind: "org", resource_id: "*" }],
          },
        ],
      },
    }),
    cleanup: async (ctx, _fx, res) => {
      const role = await bodyOf<{ id: string }>(res);
      if (role) {
        await ctx.admin.call("DELETE", "/rpc/access.deleteRole", {
          project: null,
          query: { id: role.id },
        });
      }
    },
    expect: {
      admin: 201,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
  {
    slice: "access",
    name: "reassign a member's roles",
    method: "PUT",
    path: "/rpc/access.updateMemberRoles",
    project: null,
    // Reassigns the member persona the role it already has.
    request: (ctx) => ({
      body: {
        user_id: ctx.world.users.member.userId,
        role_ids: [ctx.world.roleIds["member"]],
      },
    }),
    expect: {
      admin: 200,
      member: deny,
      reader: deny,
      blocked: deny,
      norole: deny,
      outsider: deny,
      anonymous: unauthenticated,
    },
  },
];
