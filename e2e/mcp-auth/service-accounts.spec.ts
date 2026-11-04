import { randomUUID } from "node:crypto";
import { Dashboard } from "./dashboard.ts";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { gatewayEchoes } from "./gateway.ts";
import { ServiceAccountActions } from "./service-account-actions.ts";

test.describe.configure({ mode: "default" });
test.setTimeout(240_000);

async function sharedSources(auth: AuthScenario) {
  const actions = new ServiceAccountActions(auth);
  const registration = await actions.registration();
  const resource = auth.mock.profiles.serviceAccount;
  const first = await auth.dashboard.remote("self-first", resource);
  const offer = await actions.offer(
    first.serverURL,
    registration,
    "shared-service-account",
  );
  const second = await auth.dashboard.remote("self-second", resource);
  await auth.dashboard.attachExistingOffer(offer, first.name, second.name);
  const graph = await auth.graph();
  const modeled = graph.servers.filter((server) =>
    [first.name, second.name].includes(server.name ?? ""),
  );
  expect(
    modeled,
    "FIXTURE_MODEL_MISMATCH: both UI-created self sources must be present",
  ).toHaveLength(2);
  const live = modeled.map((server) =>
    server.offers.filter(
      (client) => !client.deleted && !client.bindingDeleted && client.enabled,
    ),
  );
  for (const offers of live) {
    expect(offers).toHaveLength(1);
    expect(offers[0]).toMatchObject({
      type: "service_account",
      source: "admin",
      selectedServerCount: 2,
      issuerBridge: "missing",
      directLegacyAssociation: "missing",
    });
  }
  expect(live[0]![0]!.client).toBe(live[1]![0]!.client);
  expect(live[0]![0]!.registration).toBe(live[1]![0]!.registration);
  return { actions, first, second, offer, resource };
}

function assertSelfOnly(auth: AuthScenario) {
  const exchanges = auth.mock.events.filter(
    (event) => event.kind === "client_credentials",
  );
  expect(
    exchanges.length,
    "SELF_GRANT_FAILURE: real token exchange is required",
  ).toBeGreaterThan(0);
  for (const exchange of exchanges) {
    expect(
      exchange.resource,
      "SELF_RESOURCE_REGRESSION: typed self login must request the selected server resource",
    ).toBe(auth.mock.profiles.serviceAccount);
    expect(exchange).toMatchObject({
      provider: "alpha",
      clientId: auth.mock.providers.alpha.clientId,
      resource: auth.mock.profiles.serviceAccount,
      scopes: ["fixture:read"],
      outcome: "success",
    });
  }
  // A self credential must never silently fall back to a person's upstream
  // authorization-code flow, even though downstream human login is expected.
  expect(
    auth.mock.events.filter((event) => event.kind === "authorize"),
  ).toEqual([]);
  expect(auth.mock.events.filter((event) => event.kind === "refresh")).toEqual(
    [],
  );
}

async function authorizeSelf(
  auth: AuthScenario,
  client: ReturnType<AuthScenario["client"]>,
) {
  let failure: unknown;
  try {
    await auth.authorize(client);
  } catch (error) {
    failure = error;
  }
  if (auth.mock.events.some((event) => event.kind === "client_credentials"))
    assertSelfOnly(auth);
  if (failure !== undefined) throw failure;
}

for (const expired of [false, true]) {
  test(`shared service account on two direct servers: existing and fresh clients${expired ? " after nominal expiry" : ""}`, async ({
    auth,
  }) => {
    const { actions, first, second, offer, resource } =
      await sharedSources(auth);
    const existing = [
      auth.client(first.endpoint),
      auth.client(second.endpoint),
    ];
    for (const client of existing) {
      await authorizeSelf(auth, client);
      await auth.invoke(client, resource, "alpha-service");
    }
    const issuedBefore = auth.mock.events.filter(
      (event) =>
        event.kind === "client_credentials" && event.outcome === "success",
    ).length;
    if (expired) {
      // The fixture issues a two-second token for this title. Waiting beyond
      // nominal expiry is deliberate; a fresh downstream grant alone would
      // not prove the cached upstream self token can renew.
      await new Promise((resolve) => setTimeout(resolve, 2_200));
    }
    for (const client of existing)
      await auth.invoke(client, resource, "alpha-service");
    for (const server of [first, second]) {
      const fresh = auth.client(server.endpoint);
      await authorizeSelf(auth, fresh);
      await auth.invoke(fresh, resource, "alpha-service");
    }
    if (expired) {
      expect(
        auth.mock.events.filter(
          (event) =>
            event.kind === "client_credentials" && event.outcome === "success",
        ).length,
        "SELF_RENEWAL_FAILURE: a new client_credentials exchange must replace the expired token",
      ).toBeGreaterThan(issuedBefore);
    }
    assertSelfOnly(auth);
    await actions.cleanup();
  });
}

test("shared service account on the same direct sources and a stored gateway", async ({
  auth,
}) => {
  const { actions, first, second, offer, resource } = await sharedSources(auth);
  for (const server of [first, second]) {
    const direct = auth.client(server.endpoint);
    await authorizeSelf(auth, direct);
    await auth.invoke(direct, resource, "alpha-service");
  }
  const gateway = await new GatewayActions(auth).create("self-stored-gateway", [
    first.name,
    second.name,
  ]);
  for (let attempt = 0; attempt < 2; attempt++) {
    const client = auth.client(gateway.endpoint);
    await authorizeSelf(auth, client);
    const echoes = await gatewayEchoes(client, [first.name, second.name]);
    expect(echoes).toHaveLength(2);
    for (const echo of echoes) {
      const offset = auth.mock.events.length;
      const marker = randomUUID();
      const result = await echo.call({ marker });
      expect(result).not.toMatchObject({ isError: true });
      expect(JSON.stringify(result)).toContain(marker);
      const calls = auth.mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      expect(calls.length).toBeGreaterThan(0);
      expect(
        calls.every(
          (event) =>
            event.resource === resource &&
            event.principal === "alpha-service" &&
            event.outcome === "authorized",
        ),
      ).toBe(true);
    }
  }
  assertSelfOnly(auth);
  await actions.cleanup();
});

test("organization-scoped self credential is reused across two UI-created projects", async ({
  auth,
}) => {
  const actions = new ServiceAccountActions(auth);
  const registration = await actions.registration();
  const resource = auth.mock.profiles.serviceAccount;
  const first = await auth.dashboard.remote("self-project-one", resource);
  const offer = await actions.offer(
    first.serverURL,
    registration,
    "cross-project-self",
  );
  const other = new Dashboard(auth.page, auth.dashboard.site);
  other.organizationSlug = auth.dashboard.organizationSlug;
  await auth.page.goto(
    `${auth.dashboard.site}/${auth.dashboard.organizationSlug}`,
  );
  await other.createProject(`auth-e2e-${randomUUID().slice(0, 8)}`);
  // Keep a failed fixture available for diagnosis, matching the shared fixture.
  let complete = false;
  try {
    const second = await other.remote("self-project-two", resource);
    await other.attachExistingOffer(offer, first.name, second.name);
    for (const server of [first, second]) {
      const client = auth.client(server.endpoint);
      await authorizeSelf(auth, client);
      await auth.invoke(client, resource, "alpha-service");
    }
    assertSelfOnly(auth);
    await actions.cleanup();
    complete = true;
  } finally {
    await auth.graph([other.projectURL]);
    if (complete) await other.removeProject();
  }
});
