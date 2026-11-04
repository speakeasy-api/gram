import fs from "node:fs";
import path from "node:path";
import { request as playwrightRequest } from "@playwright/test";
import { ApiClient, type Credential } from "./api.ts";
import { DevIdp } from "./devidp.ts";
import { env } from "./env.ts";

// The people (and keys) every authorization assertion is made as. Each is a
// real dev-idp user with a real role assignment, so a request goes through the
// same principal resolution a production request does -- no scope override
// header, which would skip exactly the code an authorization change touches.
//
//   admin     the built-in Admin role
//   member    the built-in Member role
//   reader    a custom role with org:read and project:read only
//   blocked   broad project and MCP write grants, plus project:blocked_write
//             on the default project (but not on the sandbox project)
//   elsewhere project:read everywhere, but MCP read/write/connect bounded to
//             the sandbox project; any MCP action it can take in the default
//             project comes through a check that ignores the project
//   norole    an organization member with no role at all
//   outsider  a user whose only organization is a different one
//   anonymous no credential
//   consumerKey / producerKey  organization API keys with those key scopes

export const USER_PERSONAS = [
  "admin",
  "member",
  "reader",
  "blocked",
  "elsewhere",
  "norole",
  "outsider",
] as const;
export const KEY_PERSONAS = ["consumerKey", "producerKey"] as const;
export const PERSONAS = [
  ...USER_PERSONAS,
  ...KEY_PERSONAS,
  "anonymous",
] as const;

export type UserPersona = (typeof USER_PERSONAS)[number];
export type KeyPersona = (typeof KEY_PERSONAS)[number];
export type Persona = (typeof PERSONAS)[number];

const EMAIL_DOMAIN = "rbac-e2e.local";
const ROLE_PREFIX = "rbac-e2e";
export const SANDBOX_PROJECT_NAME = "rbac-e2e-sandbox";
const OUTSIDE_ORG_SLUG = "rbac-e2e-outside";

type RoleGrant = {
  scope: string;
  selectors?: Record<string, string>[];
};

export type World = {
  orgId: string;
  orgSlug: string;
  projectId: string;
  projectSlug: string;
  sandboxProjectId: string;
  sandboxProjectSlug: string;
  users: Record<UserPersona, { userId: string; email: string; token: string }>;
  keys: Record<KeyPersona, { id: string; key: string }>;
  roleIds: Record<string, string>;
};

export const outputDir = path.resolve(
  import.meta.dirname,
  "..",
  "..",
  ".playwright",
);
const worldFile = path.join(outputDir, "rbac-world.json");
export const storageStateFile = (persona: UserPersona): string =>
  path.join(outputDir, `rbac-state-${persona}.json`);

export function loadWorld(): World {
  if (!fs.existsSync(worldFile)) {
    throw new Error(
      `${worldFile} is missing: the suite's global setup did not run. ` +
        "Run through `mise run test:rbac-e2e`.",
    );
  }
  return JSON.parse(fs.readFileSync(worldFile, "utf8")) as World;
}

// Test files are collected before the global setup has written the world, so
// a spec cannot read it at import time. This defers the read to the first
// property access, which happens inside a test.
export function lazyWorld(): World {
  let cached: World | undefined;
  return new Proxy({} as World, {
    get: (_target, key) => (cached ??= loadWorld())[key as keyof World],
  });
}

export function credentialFor(world: World, persona: Persona): Credential {
  if (persona === "anonymous") return { kind: "none" };
  if (persona === "consumerKey" || persona === "producerKey") {
    return { kind: "key", key: world.keys[persona].key };
  }
  return { kind: "session", token: world.users[persona].token };
}

// Logs in through the real OAuth flow as whoever dev-idp currently signs in,
// and returns the session token plus a browser storage state carrying it.
async function login(persona: UserPersona | "bootstrap"): Promise<string> {
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  try {
    const res = await ctx.get(
      `${env.serverURL}/rpc/auth.login?redirect=${encodeURIComponent(`${env.siteURL}/`)}`,
    );
    if (!res.ok()) {
      throw new Error(
        `login as ${persona} ended on ${res.status()} ${res.url()}`,
      );
    }
    const state = await ctx.storageState();
    const cookie = state.cookies.find((c) => c.name === "gram_session");
    if (!cookie) {
      throw new Error(
        `login as ${persona} set no gram_session cookie (ended on ${res.url()})`,
      );
    }
    if (persona !== "bootstrap") {
      fs.writeFileSync(
        storageStateFile(persona),
        JSON.stringify(state, null, 2),
      );
    }
    return cookie.value;
  } finally {
    await ctx.dispose();
  }
}

type AuthInfo = {
  user_id: string;
  active_organization_id: string;
  organizations: {
    id: string;
    slug: string;
    projects: { id: string; slug: string }[];
  }[];
};

function customRoles(
  projectId: string,
  sandboxId: string,
): Record<string, RoleGrant[]> {
  const all = (kind: string) => [{ resource_kind: kind, resource_id: "*" }];
  return {
    [`${ROLE_PREFIX}-reader`]: [
      { scope: "org:read", selectors: all("org") },
      { scope: "project:read", selectors: all("project") },
    ],
    [`${ROLE_PREFIX}-blocked`]: [
      { scope: "org:read", selectors: all("org") },
      { scope: "project:read", selectors: all("project") },
      { scope: "project:write", selectors: all("project") },
      { scope: "mcp:read", selectors: all("mcp") },
      { scope: "mcp:write", selectors: all("mcp") },
      { scope: "mcp:connect", selectors: all("mcp") },
      {
        scope: "project:blocked_write",
        selectors: [{ resource_kind: "project", resource_id: projectId }],
      },
    ],
    [`${ROLE_PREFIX}-elsewhere`]: [
      { scope: "org:read", selectors: all("org") },
      { scope: "project:read", selectors: all("project") },
      ...["mcp:read", "mcp:write", "mcp:connect"].map((scope) => ({
        scope,
        selectors: [
          { resource_kind: "mcp", resource_id: "*", project_id: sandboxId },
        ],
      })),
    ],
  };
}

// Idempotent: a second run reuses the users, roles, project and keys the first
// one made, and only re-logs in.
export async function provisionWorld(): Promise<World> {
  fs.mkdirSync(outputDir, { recursive: true });
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  const idp = new DevIdp(ctx);
  try {
    // The developer the local seed adopted is the organization admin who sets
    // everything else up.
    await idp.clearCurrentUser();
    const bootstrap = new ApiClient(ctx, {
      kind: "session",
      token: await login("bootstrap"),
    });
    const info = await bootstrap.ok<AuthInfo>("GET", "/rpc/auth.info", {
      project: null,
    });
    const org = info.organizations.find(
      (o) => o.id === info.active_organization_id,
    );
    const project = org?.projects.find((p) => p.slug === env.projectSlug);
    if (!org || !project) {
      throw new Error(
        `bootstrap user has no project "${env.projectSlug}" in its active organization; ` +
          "has `mise run seed` run?",
      );
    }

    const sandbox = await ensureProject(
      bootstrap,
      org.id,
      SANDBOX_PROJECT_NAME,
    );
    const roleIds = await ensureRoles(bootstrap, project.id, sandbox.id);

    const idpOrgs = await idp.ensureOrg("speakeasy", "Speakeasy");
    const outsideOrg = await idp.ensureOrg(
      OUTSIDE_ORG_SLUG,
      "RBAC E2E Outside",
    );

    const rolesFor: Record<UserPersona, string[]> = {
      admin: [roleIds["admin"]!],
      member: [roleIds["member"]!],
      reader: [roleIds[`${ROLE_PREFIX}-reader`]!],
      blocked: [roleIds[`${ROLE_PREFIX}-blocked`]!],
      elsewhere: [roleIds[`${ROLE_PREFIX}-elsewhere`]!],
      norole: [],
      outsider: [],
    };

    const users = {} as World["users"];
    for (const persona of USER_PERSONAS) {
      const email = `${persona}@${EMAIL_DOMAIN}`;
      const user = await idp.ensureUser(email, `RBAC E2E ${persona}`);
      await idp.ensureMembership(
        user.id,
        persona === "outsider" ? outsideOrg.id : idpOrgs.id,
      );
      await idp.setCurrentUser(user.id);
      // First login creates the Speakeasy user and its organization membership.
      let token = await login(persona);
      const me = await new ApiClient(ctx, {
        kind: "session",
        token,
      }).ok<AuthInfo>("GET", "/rpc/auth.info", { project: null });
      // A role assignment cannot be emptied, so the no-role persona simply
      // never receives one.
      if (rolesFor[persona].length > 0) {
        await bootstrap.ok("PUT", "/rpc/access.updateMemberRoles", {
          project: null,
          body: { user_id: me.user_id, role_ids: rolesFor[persona] },
        });
        // A fresh session after the role change, so nothing cached at login
        // can stand in for the assignment.
        token = await login(persona);
      }
      users[persona] = { userId: me.user_id, email, token };
    }
    await idp.clearCurrentUser();

    const keys = {
      consumerKey: await createKey(bootstrap, "rbac-e2e-consumer", [
        "consumer",
      ]),
      producerKey: await createKey(bootstrap, "rbac-e2e-producer", [
        "producer",
      ]),
    };

    const world: World = {
      orgId: org.id,
      orgSlug: org.slug,
      projectId: project.id,
      projectSlug: project.slug,
      sandboxProjectId: sandbox.id,
      sandboxProjectSlug: sandbox.slug,
      users,
      keys,
      roleIds,
    };
    fs.writeFileSync(worldFile, JSON.stringify(world, null, 2));
    return world;
  } finally {
    await ctx.dispose();
  }
}

async function ensureProject(
  admin: ApiClient,
  orgId: string,
  name: string,
): Promise<{ id: string; slug: string }> {
  const list = await admin.ok<{
    projects: { id: string; slug: string; name: string }[];
  }>("GET", "/rpc/projects.list", {
    project: null,
    query: { organization_id: orgId },
  });
  const hit = list.projects.find((p) => p.name === name);
  if (hit) return hit;
  const created = await admin.ok<{ project: { id: string; slug: string } }>(
    "POST",
    "/rpc/projects.create",
    { project: null, body: { organization_id: orgId, name } },
  );
  return created.project;
}

async function ensureRoles(
  admin: ApiClient,
  projectId: string,
  sandboxId: string,
): Promise<Record<string, string>> {
  type Role = { id: string; name: string; slug?: string };
  const list = await admin.ok<{ roles: Role[] }>(
    "GET",
    "/rpc/access.listRoles",
  );
  const ids: Record<string, string> = {};
  for (const r of list.roles) {
    if (r.slug === "admin" || r.slug === "member") ids[r.slug] = r.id;
  }
  for (const [name, grants] of Object.entries(
    customRoles(projectId, sandboxId),
  )) {
    const existing = list.roles.find((r) => r.name === name);
    if (existing) {
      // Recreated rather than patched, so an edited definition here always
      // replaces the stored grants exactly. Assignments are redone below.
      await admin.ok("DELETE", "/rpc/access.deleteRole", {
        query: { id: existing.id },
      });
    }
    const created = await admin.ok<Role>("POST", "/rpc/access.createRole", {
      body: {
        name,
        description: "Created by the RBAC end-to-end suite.",
        grants,
      },
    });
    ids[name] = created.id;
  }
  if (!ids["admin"] || !ids["member"]) {
    throw new Error("the built-in admin and member roles are missing");
  }
  return ids;
}

async function createKey(
  admin: ApiClient,
  name: string,
  scopes: string[],
): Promise<{ id: string; key: string }> {
  // A key's secret is only returned at creation, so every run mints a fresh
  // key and revokes the previous ones with the same name.
  const list = await admin.ok<{ keys: { id: string; name: string }[] }>(
    "GET",
    "/rpc/keys.list",
    { project: null },
  );
  for (const k of list.keys.filter((k) => k.name === name)) {
    await admin.ok("DELETE", "/rpc/keys.revoke", {
      project: null,
      query: { id: k.id },
    });
  }
  const key = await admin.ok<{ id: string; key: string }>(
    "POST",
    "/rpc/keys.create",
    {
      project: null,
      body: { name, scopes },
    },
  );
  return { id: key.id, key: key.key };
}
