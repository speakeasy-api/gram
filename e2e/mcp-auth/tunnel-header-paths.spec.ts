import { randomUUID } from "node:crypto";
import { test as base, expect, type AuthScenario } from "./fixtures.ts";
import { TunnelActions } from "./tunnel-actions.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { gatewayEchoes } from "./gateway.ts";

const test = base.extend<{ tunnel: TunnelActions }>({
  tunnel: async ({ auth }, use) => {
    const tunnel = new TunnelActions(auth);
    try {
      await use(tunnel);
    } finally {
      await tunnel.close();
    }
  },
});
test.describe.configure({ mode: "default" });
test.setTimeout(300_000);
// Native tunnel creation displays a one-time credential.
test.use({ trace: "off", screenshot: "off", video: "off" });

async function accepted(
  auth: AuthScenario,
  call: (args: Record<string, unknown>) => Promise<unknown>,
  resource: string,
  principal: string,
  channel: string,
) {
  const marker = randomUUID();
  const offset = auth.mock.events.length;
  const result = await call({ marker });
  expect(result).not.toMatchObject({ isError: true });
  expect(JSON.stringify(result)).toContain(marker);
  const calls = auth.mock.events
    .slice(offset)
    .filter((event) => event.method === "tools/call");
  expect(
    calls.length,
    "G23: a real native-tunnel call must reach the upstream",
  ).toBeGreaterThan(0);
  for (const event of calls) {
    expect(event).toMatchObject({ resource, principal, outcome: "authorized" });
    expect(event.headerNames).toContain(channel);
    expect(event.headerNames).not.toContain("authorization");
    expect(event.headerNames).not.toContain(
      channel === "x-fixture-key" ? "x-fixture-user" : "x-fixture-key",
    );
  }
}

for (const method of ["static", "caller"] as const) {
  for (const surface of ["direct", "stored", "project"] as const) {
    test(`G23: native tunnel ${method} header reaches only its selected upstream through ${surface}`, async ({
      auth,
      tunnel,
    }) => {
      test
        .info()
        .annotations.push({ type: "gap-hypothesis", description: "G23" });
      const resource =
        method === "static"
          ? auth.mock.profiles.static
          : auth.mock.profiles.passThrough;
      const source = await test.step(
        "UI: create connected tunnel and typed header offer",
        async () => {
          const source = await tunnel.create(
            `fixture-header-${method}-${randomUUID().slice(0, 8)}`,
            resource,
            resource,
          );
          if (method === "static") await auth.dashboard.staticHeader();
          else await auth.dashboard.forwardedHeader();
          const graph = await auth.graph();
          const server = graph.servers.find(
            (server) => server.name === source.name,
          );
          expect(server).toMatchObject({
            kind: "tunneled",
            visibility: "private",
          });
          const offers = server!.offers.filter(
            (offer) => !offer.deleted && !offer.bindingDeleted && offer.enabled,
          );
          expect(offers).toHaveLength(1);
          expect(offers[0]!.type).toBe(
            method === "static" ? "static_credential" : "pass_through",
          );
          return source;
        },
        { timeout: 100_000 },
      );
      const gateways = new GatewayActions(auth);
      const endpoint =
        surface === "direct"
          ? source.endpoint
          : surface === "stored"
            ? (
                await gateways.create(
                  `fixture-header-gateway-${randomUUID().slice(0, 8)}`,
                  [source.name],
                )
              ).endpoint
            : await gateways.projectEndpoint();
      const client = auth.client(
        endpoint,
        method === "caller"
          ? { "X-Client-Fixture": "fixture-user-secret" }
          : undefined,
      );
      await test.step(
        "Consent: authorize the actual downstream session",
        async () => {
          await auth.authorize(client);
        },
        { timeout: 70_000 },
      );
      const { tools } = await client.listTools();
      const directName = tools.find((tool) =>
        tool.name.endsWith("fixture_echo"),
      )?.name;
      if (surface === "direct") expect(directName).toBeTruthy();
      const call =
        surface === "direct"
          ? (args: Record<string, unknown>) =>
              client.callTool(directName!, args)
          : (
              await gatewayEchoes(client, [
                new URL(source.serverURL).pathname.split("/").at(-1)!,
              ])
            )[0]!.call;
      const principal =
        method === "static" ? "static-user" : "pass-through-user";
      const channel = method === "static" ? "x-fixture-key" : "x-fixture-user";
      await test.step(
        "Runtime: exact header and principal before and after downstream refresh",
        async () => {
          await accepted(auth, call, resource, principal, channel);
          await client.refresh();
          await accepted(auth, call, resource, principal, channel);
        },
        { timeout: 65_000 },
      );
      if (method === "caller") {
        await test.step(
          "Runtime: absent and wrong caller headers are denied, then valid header recovers",
          async () => {
            for (const value of [undefined, "fixture-wrong-user"]) {
              client.setCallerHeader("X-Client-Fixture", value);
              const offset = auth.mock.events.length;
              const outcome = await call({ marker: randomUUID() }).then(
                (result) => ({ result, error: undefined }),
                (error: unknown) => ({
                  result: undefined,
                  error: String(error),
                }),
              );
              if (outcome.error !== undefined)
                expect(outcome.error).toMatch(
                  /401|403|412|credential|unauthoriz|forbidden|JSON-RPC error/i,
                );
              else expect(outcome.result).toMatchObject({ isError: true });
              expect(
                auth.mock.events
                  .slice(offset)
                  .filter(
                    (event) =>
                      event.method === "tools/call" &&
                      event.outcome === "authorized",
                  ),
              ).toHaveLength(0);
            }
            client.setCallerHeader("X-Client-Fixture", "fixture-user-secret");
            await accepted(auth, call, resource, principal, channel);
          },
          { timeout: 50_000 },
        );
      }
    });
  }
}

for (const kind of ["remote", "tunnel"] as const) {
  test(`G8: configuring personal OAuth leaves an existing unsigned ${kind} sibling anonymous`, async ({
    auth,
    tunnel,
  }) => {
    test.info().annotations.push({ type: "gap-hypothesis", description: "G8" });
    const unsigned =
      kind === "remote"
        ? await auth.dashboard.remote(
            `fixture-unsigned-${randomUUID().slice(0, 8)}`,
            auth.mock.profiles.noAuth,
          )
        : await tunnel.create(
            `fixture-unsigned-${randomUUID().slice(0, 8)}`,
            auth.mock.profiles.noAuth,
          );
    const existing = auth.client(unsigned.endpoint);
    await auth.authorize(existing);
    await auth.invoke(existing, auth.mock.profiles.noAuth, "anonymous");
    const before = (await auth.graph()).servers.find(
      (server) => server.name === unsigned.name,
    )!;
    const signed = await auth.dashboard.remote(
      `fixture-signed-${randomUUID().slice(0, 8)}`,
      auth.mock.profiles.oauthAlpha,
      "user",
    );
    const personal = auth.client(signed.endpoint);
    await auth.authorize(personal);
    await auth.invoke(personal, auth.mock.profiles.oauthAlpha, "alpha-user");
    const after = (await auth.graph()).servers.find(
      (server) => server.name === unsigned.name,
    )!;
    expect
      .soft(
        after.signInIssuer,
        "G8: configuring another server must preserve downstream sign-in policy",
      )
      .toBe(before.signInIssuer);
    expect
      .soft(
        after.derivedUpstreamIssuer,
        "G8: an unsigned sibling must not inherit another server's provider",
      )
      .toBe(before.derivedUpstreamIssuer);
    const offset = auth.mock.events.length;
    await test.step(
      "Runtime: old and fresh unsigned sessions remain anonymous",
      async () => {
        await auth.invoke(existing, auth.mock.profiles.noAuth, "anonymous");
        const fresh = auth.client(unsigned.endpoint);
        await auth.authorize(fresh);
        await auth.invoke(fresh, auth.mock.profiles.noAuth, "anonymous");
      },
      { timeout: 75_000 },
    );
    const calls = auth.mock.events
      .slice(offset)
      .filter(
        (event) =>
          event.method === "tools/call" &&
          event.resource === auth.mock.profiles.noAuth,
      );
    expect(calls.length).toBeGreaterThanOrEqual(2);
    for (const call of calls) {
      expect(call).toMatchObject({
        principal: "anonymous",
        outcome: "authorized",
      });
      expect(call.headerNames).not.toContain("authorization");
      expect(call.headerNames).not.toContain("x-fixture-key");
      expect(call.headerNames).not.toContain("x-fixture-user");
    }
    await auth.invoke(personal, auth.mock.profiles.oauthAlpha, "alpha-user");
  });
}
