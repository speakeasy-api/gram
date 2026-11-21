import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { GatewayActions } from "./gateway-actions.ts";
import {
  consentObserver,
  inventoryObserver,
  sourceTopology,
} from "./gateway-control-diagnostics.ts";
import { gatewayEchoes } from "./gateway.ts";
import type { FixtureMcpClient } from "./client.ts";

test.setTimeout(300_000);

type Member = Awaited<ReturnType<typeof member>>;
async function member(
  auth: AuthScenario,
  name: string,
  provider: "alpha" | "beta",
  projectIssuer = false,
) {
  const resource =
    provider === "alpha"
      ? auth.mock.profiles.oauthAlpha
      : auth.mock.profiles.oauthBeta;
  const server = await auth.dashboard.remote(
    `${name}-${randomUUID().slice(0, 8)}`,
    resource,
    "user",
    projectIssuer,
  );
  const direct = auth.client(server.endpoint);
  await auth.authorize(direct);
  const principal = `${provider}-user`;
  await auth.invoke(direct, resource, principal);
  return {
    ...server,
    direct,
    resource,
    provider,
    principal,
    slug: new URL(server.serverURL).pathname.split("/").at(-1)!,
  };
}

async function connect(
  auth: AuthScenario,
  endpoint: string,
  members?: Member[],
  phase = "gateway",
) {
  const client = auth.client(endpoint);
  await auth.authorize(
    client,
    undefined,
    members
      ? consentObserver(
          auth.page,
          members.map((member) => member.slug),
          phase,
        )
      : undefined,
  );
  return client;
}

async function echoes(
  auth: AuthScenario,
  client: FixtureMcpClient,
  members: Member[],
  phase?: string,
) {
  const tools = await gatewayEchoes(
    client,
    members.map((m) => m.slug),
    phase ? inventoryObserver(phase) : undefined,
  );
  for (const [index, tool] of tools.entries()) {
    const target = members[index]!;
    const offset = auth.mock.events.length;
    const marker = randomUUID();
    const result = await tool.call({ marker });
    expect(result).toMatchObject({ isError: false });
    expect(JSON.stringify(result)).toContain(marker);
    const calls = auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call");
    expect(
      calls.length,
      "UPSTREAM_ROUTING_FAILURE: gateway call must reach the exact member",
    ).toBeGreaterThan(0);
    for (const call of calls) {
      expect(
        call,
        "GATEWAY_CREDENTIAL_LEAK: reject even a wrong-member attempt followed by a successful retry",
      ).toMatchObject({
        resource: target.resource,
        provider: target.provider,
        principal: target.principal,
        outcome: "authorized",
      });
    }
  }
}

function structured(result: unknown): Record<string, unknown> {
  const value = result as {
    structuredContent?: Record<string, unknown>;
    content?: { type: string; text?: string }[];
  };
  if (value.structuredContent) return value.structuredContent;
  const text = value.content?.find((chunk) => chunk.type === "text")?.text;
  if (!text)
    throw new Error(
      "GATEWAY_CATALOG_FAILURE: discovery must return structured content",
    );
  return JSON.parse(text) as Record<string, unknown>;
}

async function excludes(
  auth: AuthScenario,
  client: FixtureMcpClient,
  forbidden: Member,
  formerlyQualified?: string,
) {
  const offset = auth.mock.events.length;
  const { tools } = await client.listTools();
  if (tools.some((tool) => tool.name === "list_servers")) {
    const catalog = structured(await client.callTool("list_servers"));
    expect(Array.isArray(catalog.servers)).toBe(true);
    const servers = catalog.servers as { slug: string }[];
    expect(
      servers.some(
        (server) =>
          server.slug === forbidden.slug ||
          server.slug.endsWith(`.${forbidden.slug}`),
      ),
      "GATEWAY_MEMBERSHIP_BLEED: unrelated member must not be discoverable",
    ).toBe(false);
  } else {
    expect(
      tools.some(
        (tool) =>
          tool.name.startsWith(`${forbidden.slug}--`) ||
          tool.name.includes(`.${forbidden.slug}--`),
      ),
    ).toBe(false);
  }
  if (formerlyQualified) {
    // Removed tools can be rejected at the JSON-RPC boundary rather than
    // returning an MCP tool error. Network failures are not rejection evidence.
    const outcome = await (
      tools.some((tool) => tool.name === "list_servers")
        ? client.callTool("execute_tool", {
            name: formerlyQualified,
            arguments: { marker: randomUUID() },
          })
        : client.callTool(formerlyQualified, { marker: randomUUID() })
    ).then(
      (result) => ({ result, error: undefined }),
      (error: unknown) => ({ result: undefined, error: String(error) }),
    );
    if (outcome.error !== undefined)
      expect(outcome.error).toMatch(/HTTP (401|403|404)|JSON-RPC error/i);
    else expect(outcome.result).toMatchObject({ isError: true });
  }
  expect(
    auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call"),
    "GATEWAY_MEMBERSHIP_BLEED: removed/unrelated member must never execute upstream",
  ).toEqual([]);
}

for (const kind of ["project", "stored"] as const) {
  test(`enforced: same direct server through ${kind} gateway preserves both provider grants`, async ({
    auth,
  }) => {
    const a = await member(auth, "overlap-a", "alpha");
    const b = await member(auth, "overlap-b", "beta", true);
    const ui = new GatewayActions(auth);
    const names = [a.name, b.name];
    const before = sourceTopology(await auth.graph(), names);
    const stored = await ui.create(
      `overlap-stored-${randomUUID().slice(0, 8)}`,
      names,
    );
    try {
      const control = await connect(
        auth,
        stored.endpoint,
        [a, b],
        "stored-control",
      );
      await echoes(auth, control, [a, b], "stored-control");
    } finally {
      expect(
        sourceTopology(await auth.graph(), names),
        "Stored control must preserve both source bindings",
      ).toEqual(before);
    }
    if (kind === "project") {
      try {
        const gateway = await connect(
          auth,
          await ui.projectEndpoint(),
          [a, b],
          "automatic-project",
        );
        await echoes(auth, gateway, [a, b], "automatic-project");
      } finally {
        expect(
          sourceTopology(await auth.graph(), names),
          "Automatic control must preserve both source bindings",
        ).toEqual(before);
      }
    }
    await auth.invoke(a.direct, a.resource, a.principal);
    await auth.invoke(b.direct, b.resource, b.principal);
  });
}

test("enforced: G1={A,B}, G2={A,C} isolate catalogs and membership changes from direct callers", async ({
  auth,
}) => {
  const a = await member(auth, "overlap-a", "alpha");
  const b = await member(auth, "overlap-b", "beta", true);
  const c = await member(auth, "overlap-c", "alpha");
  const ui = new GatewayActions(auth);
  const g1 = await ui.create(`overlap-g1-${randomUUID().slice(0, 8)}`, [
    a.name,
    b.name,
  ]);
  const g2 = await ui.create(`overlap-g2-${randomUUID().slice(0, 8)}`, [
    a.name,
    c.name,
  ]);
  const first = await connect(auth, g1.endpoint);
  const second = await connect(auth, g2.endpoint);
  await echoes(auth, first, [a, b]);
  await echoes(auth, second, [a, c]);
  await excludes(auth, second, b);
  await excludes(auth, first, c);
  const [oldA] = await gatewayEchoes(first, [a.slug]);
  await ui.remove(g1, a.name);
  await excludes(auth, first, a, oldA!.qualifiedName);
  await echoes(auth, first, [b]);
  await echoes(auth, second, [a, c]);
  await auth.invoke(a.direct, a.resource, a.principal);
  await ui.add(g1, [a.name]);
  await echoes(auth, first, [a, b]);
  await echoes(auth, second, [a, c]);
  await excludes(auth, second, b);
  for (const source of [a, b, c])
    await auth.invoke(source.direct, source.resource, source.principal);
});

test("enforced: organization, automatic project, and stored gateways overlap on the same source", async ({
  auth,
}) => {
  const a = await member(auth, "overlap-a", "alpha");
  const b = await member(auth, "overlap-b", "beta", true);
  const ui = new GatewayActions(auth);
  const names = [a.name, b.name];
  const before = sourceTopology(await auth.graph(), names);
  const stored = await ui.create(
    `overlap-stored-${randomUUID().slice(0, 8)}`,
    names,
  );
  const callers = [];
  try {
    const control = await connect(
      auth,
      stored.endpoint,
      [a, b],
      "stored-control",
    );
    await echoes(auth, control, [a, b], "stored-control");
    callers.push(control);
  } finally {
    expect(
      sourceTopology(await auth.graph(), names),
      "Stored control must preserve both source bindings",
    ).toEqual(before);
  }
  const organization = await auth.dashboard.organizationGateway();
  const project = await ui.projectEndpoint();
  expect(new Set([organization, project, stored.endpoint]).size).toBe(3);
  for (const [phase, endpoint] of [
    ["automatic-organization", organization],
    ["automatic-project", project],
  ] as const) {
    try {
      const caller = await connect(auth, endpoint, [a, b], phase);
      await echoes(auth, caller, [a, b], phase);
      callers.push(caller);
    } finally {
      expect(
        sourceTopology(await auth.graph(), names),
        "Automatic control must preserve both source bindings",
      ).toEqual(before);
    }
  }
  // Keep existing sessions alive while the overlapping callers are established.
  for (const caller of callers) await echoes(auth, caller, [a, b]);
  await auth.invoke(a.direct, a.resource, a.principal);
  await auth.invoke(b.direct, b.resource, b.principal);
});

test("enforced: typed multi-offer source keeps direct OAuth while stored gateways select static credentials", async ({
  auth,
}) => {
  const resource = auth.mock.profiles.multiMethod;
  const server = await auth.dashboard.remote(
    `overlap-multi-${randomUUID().slice(0, 8)}`,
    resource,
    "user",
  );
  const offer = `overlap-shared-static-${randomUUID().slice(0, 8)}`;
  // Establish OAuth before adding the second offer, avoiding ambiguous consent defaults.
  const oauth = auth.client(server.endpoint);
  await auth.authorize(oauth);
  await auth.invoke(oauth, resource, "alpha-user");
  await auth.page.goto(`${server.serverURL}/settings`);
  await auth.dashboard.addStaticOffer(offer);
  const directStatic = auth.client(server.endpoint);
  await auth.authorize(directStatic, offer);
  await auth.invoke(directStatic, resource, "static-user");
  const ui = new GatewayActions(auth);
  const slug = new URL(server.serverURL).pathname.split("/").at(-1)!;
  for (const name of ["overlap-static-g1", "overlap-static-g2"]) {
    const gateway = await ui.create(name, [server.name]);
    const client = auth.client(gateway.endpoint);
    await auth.authorize(client, offer);
    const [echo] = await gatewayEchoes(client, [slug]);
    const offset = auth.mock.events.length;
    const result = await echo!.call({ marker: name });
    expect(result).toMatchObject({ isError: false });
    expect(JSON.stringify(result)).toContain(name);
    const calls = auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call");
    expect(calls.length).toBeGreaterThan(0);
    for (const call of calls) {
      expect(call).toMatchObject({
        resource,
        principal: "static-user",
        outcome: "authorized",
      });
      expect(call.provider).toBeUndefined();
    }
    await auth.invoke(oauth, resource, "alpha-user");
    await auth.invoke(directStatic, resource, "static-user");
  }
});
