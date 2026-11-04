import { randomUUID } from "node:crypto";
import { test as base, expect, type AuthScenario } from "./fixtures.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { gatewayEchoes } from "./gateway.ts";
import { ServiceAccountActions } from "./service-account-actions.ts";
import { TunnelActions } from "./tunnel-actions.ts";

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
test.setTimeout(360_000);
// Creation displays a one-time connection secret. Do not retain DOM snapshots,
// screenshots, or locator-return values from that page in Playwright artifacts.
test.use({ trace: "off", screenshot: "off", video: "off" });

async function gatewayCall(
  auth: AuthScenario,
  call: (args: Record<string, unknown>) => Promise<unknown>,
  resource: string,
  principal: string,
  authorization: boolean,
) {
  const offset = auth.mock.events.length;
  const marker = randomUUID();
  const result = await call({ marker });
  expect(result).not.toMatchObject({ isError: true });
  expect(JSON.stringify(result)).toContain(marker);
  const calls = auth.mock.events
    .slice(offset)
    .filter((event) => event.method === "tools/call");
  expect(
    calls.length,
    "TUNNEL_UPSTREAM_FAILURE: gateway call must reach the standalone mock",
  ).toBeGreaterThan(0);
  for (const event of calls) {
    expect(event).toMatchObject({ resource, principal, outcome: "authorized" });
    expect(event.headerNames?.includes("authorization")).toBe(authorization);
  }
}

test("native tunnel without an upstream resource stays anonymous beside a signed-in sibling, direct and stored gateway", async ({
  auth,
  tunnel,
}) => {
  // Prime a real upstream connection in the same project. An unset tunnel
  // resource is not permission to guess this sibling's URL or forward its token.
  const sibling = await auth.dashboard.remote(
    "tunnel-signed-sibling",
    auth.mock.profiles.oauthBeta,
    "user",
  );
  const siblingClient = auth.client(sibling.endpoint);
  await auth.authorize(siblingClient);
  await auth.invoke(siblingClient, auth.mock.profiles.oauthBeta, "beta-user");
  const source = await tunnel.create(
    "tunnel-unqualified",
    auth.mock.profiles.noAuth,
  );
  const direct = auth.client(source.endpoint);
  const offset = auth.mock.events.length;
  await auth.authorize(direct);
  await auth.invoke(direct, auth.mock.profiles.noAuth, "anonymous");
  const forwarded = auth.mock.events
    .slice(offset)
    .filter(
      (event) =>
        event.resource === auth.mock.profiles.noAuth &&
        event.method === "tools/call",
    );
  expect(forwarded.length).toBeGreaterThan(0);
  for (const event of forwarded)
    expect(event.headerNames).not.toContain("authorization");
  expect(
    auth.mock.events
      .slice(offset)
      .filter(
        (event) =>
          event.kind === "authorize" || event.kind === "client_credentials",
      ),
  ).toEqual([]);

  const gateway = await new GatewayActions(auth).create(
    "tunnel-unqualified-gateway",
    [source.name, sibling.name],
  );
  const gatewayClient = auth.client(gateway.endpoint);
  await auth.authorize(gatewayClient);
  const echoes = await gatewayEchoes(gatewayClient, [
    source.name,
    sibling.name,
  ]);
  await gatewayCall(
    auth,
    echoes[0]!.call,
    auth.mock.profiles.noAuth,
    "anonymous",
    false,
  );
  await gatewayCall(
    auth,
    echoes[1]!.call,
    auth.mock.profiles.oauthBeta,
    "beta-user",
    true,
  );
});

for (const surface of ["stored", "project"] as const) {
  test(`native tunnel with explicit resource and scoped shared self credential, direct and ${surface} gateway`, async ({
    auth,
    tunnel,
  }) => {
    const accounts = new ServiceAccountActions(auth);
    const registration = await accounts.registration();
    const resource = auth.mock.profiles.serviceAccount;
    const sibling = await auth.dashboard.remote(
      "tunnel-scoped-sibling",
      resource,
    );
    const offer = await accounts.offer(
      sibling.serverURL,
      registration,
      "tunnel-shared-self",
    );
    // This is deliberately a shared registration also offered on a real remote
    // source. Its RFC 8707 resource comes from that recorded remote attachment;
    // the tunnel's resource identifier is an explicit routing match, NOT a
    // claim that adding a tunnel identifier alone provisions OAuth metadata.
    const source = await tunnel.create("tunnel-scoped", resource, resource);
    await auth.dashboard.attachExistingOffer(offer, sibling.name, source.name);
    const graph = await auth.graph();
    const offers = [sibling.name, source.name].map((name) =>
      graph.servers
        .find((server) => server.name === name)
        ?.offers.filter(
          (offer) =>
            offer.type === "service_account" &&
            offer.enabled &&
            !offer.deleted &&
            !offer.bindingDeleted,
        ),
    );
    for (const offered of offers) expect(offered).toHaveLength(1);
    expect(offers[0]![0]!.client).toBe(offers[1]![0]!.client);
    expect(offers[0]![0]!.registration).toBe(offers[1]![0]!.registration);
    for (const endpoint of [sibling.endpoint, source.endpoint]) {
      const client = auth.client(endpoint);
      // A single attached self offer is automatic, not a multi-choice picker.
      await auth.authorize(client);
      await auth.invoke(client, resource, "alpha-service");
    }
    const gateways = new GatewayActions(auth);
    const endpoint =
      surface === "stored"
        ? (
            await gateways.create("tunnel-scoped-gateway", [
              sibling.name,
              source.name,
            ])
          ).endpoint
        : await gateways.projectEndpoint();
    const client = auth.client(endpoint);
    await auth.authorize(client, offer);
    const echoes = await gatewayEchoes(client, [sibling.name, source.name]);
    expect(echoes).toHaveLength(2);
    for (const echo of echoes)
      await gatewayCall(auth, echo.call, resource, "alpha-service", true);
    const exchanges = auth.mock.events.filter(
      (event) => event.kind === "client_credentials",
    );
    expect(
      exchanges.length,
      "TUNNEL_SCOPE_FAILURE: require a real client_credentials exchange",
    ).toBeGreaterThan(0);
    for (const exchange of exchanges) {
      expect(exchange).toMatchObject({
        provider: "alpha",
        clientId: auth.mock.providers.alpha.clientId,
        resource,
        scopes: ["fixture:read"],
        outcome: "success",
      });
    }
    expect(
      auth.mock.events.filter(
        (event) => event.kind === "authorize" || event.kind === "refresh",
      ),
    ).toEqual([]);
    await accounts.cleanup();
  });
}
