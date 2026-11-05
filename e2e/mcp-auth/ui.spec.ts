import { randomUUID } from "node:crypto";
import { privateProtocolFailureRecorder } from "./authorization-diagnostics.ts";
import { test, expect, type Page } from "@playwright/test";
import { Dashboard, approveAuthorization } from "./dashboard.ts";
import { FixtureMcpClient } from "./client.ts";
import { startMockAuthServer } from "./mock-server.ts";
import { gatewayEchoes } from "./gateway.ts";
import { GatewayActions } from "./gateway-actions.ts";
import {
  consentObserver,
  inventoryObserver,
  sourceTopology,
} from "./gateway-control-diagnostics.ts";
import { captureAuthGraph } from "./graph.ts";

test.describe.configure({ mode: "default" }); // config uses one worker; a blocked case must not skip later cases.
test.setTimeout(180_000);

type Mock = Awaited<ReturnType<typeof startMockAuthServer>>;
let mock: Mock;
let dashboard: Dashboard;
const clients: FixtureMcpClient[] = [];
const authorizations = new Set<FixtureMcpClient>();

test.beforeEach(async ({ page }, testInfo) => {
  mock = await startMockAuthServer({
    accessTokenLifetimeSeconds: testInfo.title.includes("expiry") ? 2 : 3600,
  });
  dashboard = new Dashboard(page);
  await dashboard.login();
  await dashboard.enterProject();
});

test.afterEach(async ({}, testInfo) => {
  let graphFailure: unknown;
  if (dashboard?.ownedProjects.length > 0) {
    try {
      const graph = await captureAuthGraph(
        dashboard.ownedProjects.map((project) => project.url),
      );
      await testInfo.attach("fixture-auth-graph", {
        body: JSON.stringify(graph),
        contentType: "application/json",
      });
    } catch (error) {
      graphFailure = error;
      await testInfo.attach("fixture-auth-graph-error", {
        body: "Local read-only topology evidence unavailable",
        contentType: "text/plain",
      });
    }
  }
  await testInfo.attach("fixture-journal", {
    body: JSON.stringify(mock?.events ?? [], null, 2),
    contentType: "application/json",
  });
  await testInfo.attach("protocol-journal", {
    body: JSON.stringify(
      clients.map((value) => value.transcript),
      null,
      2,
    ),
    contentType: "application/json",
  });
  const revoked = await Promise.allSettled(
    [...authorizations].map((value) => value.revoke()),
  );
  authorizations.clear();
  const closed = await Promise.allSettled(
    clients.splice(0).map((client) => client.close()),
  );
  // Evidence and grant/session closure precede dashboard-only fixture cleanup.
  // Keep scenario failures primary; report teardown failures separately.
  const scenarioFailed = testInfo.status !== "passed";
  const cleanup = {
    grantFailures: revoked.filter((result) => result.status === "rejected")
      .length,
    sessionFailures: closed.filter((result) => result.status === "rejected")
      .length,
    projectDeleteFailures: 0,
    mockCloseFailed: false,
  };
  if (dashboard?.createdProjects.length > 0) {
    for (const project of [...dashboard.createdProjects].reverse()) {
      try {
        dashboard.projectURL = project.url;
        // removeProject verifies membership in this instance's ownership ledger.
        await dashboard.removeProject();
      } catch {
        cleanup.projectDeleteFailures += 1;
      }
    }
  }
  try {
    await mock?.close();
  } catch {
    cleanup.mockCloseFailed = true;
  }
  const cleanupFailed =
    cleanup.grantFailures > 0 ||
    cleanup.sessionFailures > 0 ||
    cleanup.projectDeleteFailures > 0 ||
    cleanup.mockCloseFailed;
  if (cleanupFailed) {
    try {
      await testInfo.attach("fixture-cleanup-failure", {
        body: JSON.stringify(cleanup),
        contentType: "application/json",
      });
    } catch {
      // This fixed annotation cannot leak resource names or cleanup errors.
      testInfo.annotations.push({
        type: "fixture-cleanup-failure",
        description: "Cleanup diagnostics attachment unavailable",
      });
    }
  }
  if (!scenarioFailed && graphFailure !== undefined) throw graphFailure;
  if (!scenarioFailed && cleanupFailed)
    throw new Error(
      "FIXTURE_CLEANUP_FAILURE: see separate cleanup diagnostics",
    );
});

function client(endpoint: string, headers?: Record<string, string>) {
  const value = new FixtureMcpClient({
    endpoint,
    headers,
    onProtocolFailure: privateProtocolFailureRecorder(test.info()),
    allowInsecureLoopback: true,
    trustedOrigins: [
      new URL(dashboard.site).origin,
      new URL(endpoint).origin,
      mock.origin,
    ],
  });
  clients.push(value);
  return value;
}

async function authorize(
  page: Page,
  value: FixtureMcpClient,
  observe?: () => Promise<void>,
) {
  const flow = await value.beginAuthorization();
  try {
    await approveAuthorization(page, flow.authorizationUrl, observe);
    await flow.complete();
    authorizations.add(value);
  } catch (error) {
    await flow.cancel();
    throw error;
  }
  await value.initialize();
}

async function authorizeTypedResource(
  page: Page,
  value: FixtureMcpClient,
  expected: { resource: string; clientId: string; scopes?: string[] },
) {
  const offset = mock.events.length;
  let failure: unknown;
  try {
    await authorize(page, value);
  } catch (error) {
    failure = error;
  }
  const grants = mock.events
    .slice(offset)
    .filter((event) => event.kind === "authorize");
  // Do not relabel a browser/setup failure as evidence of a resource bug.
  if (grants.length === 0 && failure !== undefined) throw failure;
  expect(
    grants.length,
    "TYPED_LOGIN_REGRESSION: this fresh resource requires a new upstream authorization",
  ).toBeGreaterThan(0);
  for (const grant of grants) {
    if (expected.scopes)
      expect(
        grant.scopes,
        "TYPED_SCOPE_REGRESSION: issuer defaults must not replace the selected server scopes",
      ).toEqual(expect.arrayContaining(expected.scopes));
    expect(
      grant,
      "TYPED_RESOURCE_REGRESSION: the shared registration must authorize the selected server, not its legacy sibling",
    ).toMatchObject({
      resource: expected.resource,
      clientId: expected.clientId,
    });
    expect(
      grant.outcome,
      "TYPED_LOGIN_REGRESSION: the provider must accept the exact resource/scopes",
    ).toBe("success");
  }
  if (failure !== undefined) throw failure;
}

async function invoke(
  value: FixtureMcpClient,
  resource: string,
  principal?: string,
) {
  const { tools } = await value.listTools();
  const echo = tools.find((tool) => tool.name.endsWith("fixture_echo"));
  expect(
    echo,
    "MCP_DISCOVERY_FAILURE: the dashboard-provisioned server must expose the fixture tool",
  ).toBeDefined();
  const marker = randomUUID();
  const eventOffset = mock.events.length;
  const result = await value.callTool(echo!.name, { marker });
  expect(result).toMatchObject({ isError: false });
  expect(JSON.stringify(result)).toContain(marker);
  const calls = mock.events
    .slice(eventOffset)
    .filter((event) => event.method === "tools/call");
  expect(
    calls.length,
    "UPSTREAM_ROUTING_FAILURE: this exact invocation must reach an upstream",
  ).toBeGreaterThan(0);
  expect(
    calls.every(
      (event) =>
        event.resource === resource &&
        event.outcome === "authorized" &&
        (!principal || event.principal === principal),
    ),
    "UPSTREAM_IDENTITY_FAILURE: this invocation must reach only its own fixture and selected identity",
  ).toBe(true);
}

test("enforced: no-auth upstream still requires a real downstream user session", async ({
  page,
}) => {
  const server = await dashboard.remote(
    `fixture-noauth-${randomUUID().slice(0, 8)}`,
    mock.profiles.noAuth,
  );
  const value = client(server.endpoint);
  await authorize(page, value);
  await invoke(value, mock.profiles.noAuth, "anonymous");
});

test("enforced: static header configured in dashboard reaches upstream", async ({
  page,
}) => {
  const server = await dashboard.remote(
    `fixture-static-${randomUUID().slice(0, 8)}`,
    mock.profiles.static,
  );
  await dashboard.staticHeader();
  const value = client(server.endpoint);
  await authorize(page, value);
  await invoke(value, mock.profiles.static, "static-user");
  expect(
    mock.events
      .filter((event) => event.method === "tools/call")
      .every((event) => event.headerNames?.includes("x-fixture-key")),
  ).toBe(true);
});

test("enforced: pass-through header is configured in dashboard and supplied only by the MCP caller", async ({
  page,
}) => {
  const server = await dashboard.remote(
    `fixture-forwarded-${randomUUID().slice(0, 8)}`,
    mock.profiles.passThrough,
  );
  await dashboard.forwardedHeader();
  const value = client(server.endpoint, {
    "X-Client-Fixture": "fixture-user-secret",
  });
  await authorize(page, value);
  await invoke(value, mock.profiles.passThrough, "pass-through-user");
  const calls = mock.events.filter(
    (event) =>
      event.method === "tools/call" &&
      event.resource === mock.profiles.passThrough,
  );
  expect(
    calls.every((event) => event.headerNames?.includes("x-fixture-user")),
  ).toBe(true);
});

test("enforced: personal OAuth and an auxiliary static header coexist", async ({
  page,
}) => {
  const offset = mock.events.length;
  // Observe the actual UI commit, without issuing a separate API request.
  const [commit, server] = await Promise.all([
    page
      .waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).pathname ===
            "/rpc/remoteSessions.commitServerIdentityConfiguration",
      )
      .then(async (response) => ({
        ok: response.ok(),
        body: (await response.json()) as {
          status?: unknown;
          registration_method?: unknown;
          client?: { id?: unknown; client_id?: unknown };
        } | null,
      }))
      .catch(() => ({ ok: false, body: null })),
    dashboard.remote(
      `fixture-oauth-header-${randomUUID().slice(0, 8)}`,
      mock.profiles.oauthAlpha,
      "user",
    ),
  ]);
  const registrations = mock.events
    .slice(offset)
    .filter((event) => event.kind === "register");
  expect(registrations.length === 1, "C72: exactly one new registration").toBe(
    true,
  );
  expect(
    registrations[0]!.provider === "alpha" &&
      typeof registrations[0]!.clientId === "string" &&
      registrations[0]!.clientId.length > 0,
    `C72: registration must belong to the expected provider`,
  ).toBe(true);
  const registrationRowId = commit.body?.client?.id;
  expect(
    commit.ok &&
      commit.body?.status === "registered" &&
      commit.body?.registration_method === "dcr" &&
      commit.body?.client?.client_id === registrations[0]!.clientId &&
      typeof registrationRowId === "string" &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        registrationRowId,
      ),
    `C72: UI commit must identify the exact recorded DCR registration`,
  ).toBe(true);
  if (typeof registrationRowId !== "string")
    throw new Error(`C72: UI commit did not provide a registration row`);
  await dashboard.personalStaticHeader(
    registrations[0]!.clientId!,
    registrationRowId,
  );
  const value = client(server.endpoint);
  await authorize(page, value);
  await invoke(value, mock.profiles.oauthAlpha, "alpha-user");
  const calls = mock.events.filter(
    (event) =>
      event.method === "tools/call" &&
      event.resource === mock.profiles.oauthAlpha,
  );
  expect(
    calls.some(
      (event) =>
        event.headerNames?.includes("authorization") &&
        event.headerNames?.includes("x-fixture-key"),
    ),
  ).toBe(true);
});

test("enforced: regular user OAuth, fresh upstream expiry, refresh and revocation", async ({
  page,
}) => {
  const server = await dashboard.remote(
    `fixture-oauth-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthAlpha,
    "user",
  );
  const value = client(server.endpoint);
  await authorize(page, value);
  await invoke(value, mock.profiles.oauthAlpha);
  expect(
    mock.events.some(
      (event) => event.method === "tools/call" && event.provider === "alpha",
    ),
  ).toBe(true);
  await value.refresh();
  await expect
    .poll(
      async () => {
        await invoke(value, mock.profiles.oauthAlpha);
        return mock.events.some(
          (event) =>
            event.kind === "refresh" &&
            event.provider === "alpha" &&
            event.outcome === "success",
        );
      },
      {
        message:
          "UPSTREAM_REFRESH_FAILURE: nominal advertised token expiry must refresh the upstream grant",
        timeout: 20_000,
      },
    )
    .toBe(true);
  mock.revokeTokens();
  const before = mock.events.filter(
    (event) => event.method === "tools/call" && event.outcome === "authorized",
  ).length;
  await expect(
    value.listTools(),
    "A revoked grant must produce an auth refusal, not an unrelated test assertion",
  ).rejects.toThrow(/HTTP (401|403)/);
  expect(
    mock.events.filter(
      (event) =>
        event.method === "tools/call" && event.outcome === "authorized",
    ),
  ).toHaveLength(before);
  await value.revoke();
  await expect(value.refresh()).rejects.toThrow();
});

test("enforced: shared registration keeps distinct resources without an issuer bridge", async ({
  page,
}) => {
  // Observed graph: one OAuth registration reused by multiple typed offers.
  // A working old grant must not hide failure to authorize a different resource.
  const source = await dashboard.remote(
    `fixture-shared-resource-source-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthAlpha,
    "user",
  );
  const credentialName = await dashboard.personalCredentialName();
  const first = client(source.endpoint);
  await authorize(page, first);
  await invoke(first, mock.profiles.oauthAlpha, "alpha-user");
  const registration = mock.events.find(
    (event) =>
      event.kind === "authorize" && event.resource === mock.profiles.oauthAlpha,
  )?.clientId;
  expect(registration).toBeTruthy();

  // Manual setup does not attach a sign-in to this separate issuer. The only
  // OAuth offer below is the exact existing typed credential, not a new DCR client.
  const target = await dashboard.remote(
    `fixture-shared-resource-target-${randomUUID().slice(0, 8)}`,
    mock.profiles.multiple,
    "none",
    true,
  );
  await dashboard.attachExistingOffer(credentialName, source.name, target.name);
  const offset = mock.events.length;
  const second = client(target.endpoint);
  await authorizeTypedResource(page, second, {
    resource: mock.profiles.multiple,
    clientId: registration!,
  });
  const requests = mock.events
    .slice(offset)
    .filter((event) => event.kind === "authorize");
  expect(
    requests,
    "SHARED_RESOURCE_REGRESSION: a fresh grant for the target must use its exact resource and the shared registration",
  ).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        clientId: registration,
        resource: mock.profiles.multiple,
      }),
    ]),
  );
  expect(
    requests.every((event) => event.resource === mock.profiles.multiple),
    "SHARED_RESOURCE_REGRESSION: do not stamp the source resource on the target login",
  ).toBe(true);
  await invoke(second, mock.profiles.multiple, "alpha-user");
  await second.refresh();
  await invoke(second, mock.profiles.multiple, "alpha-user");
  await invoke(first, mock.profiles.oauthAlpha, "alpha-user");
});

test("enforced: reused registration requests target resource scopes instead of issuer defaults", async ({
  page,
}) => {
  const source = await dashboard.remote(
    `fixture-scope-source-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthAlpha,
    "user",
  );
  const credentialName = await dashboard.personalCredentialName();
  const first = client(source.endpoint);
  await authorize(page, first);
  await invoke(first, mock.profiles.oauthAlpha, "alpha-user");
  const registration = mock.events.find(
    (event) =>
      event.kind === "authorize" && event.resource === mock.profiles.oauthAlpha,
  )?.clientId;
  expect(registration).toBeTruthy();
  const target = await dashboard.remote(
    `fixture-scope-target-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthScoped,
    "none",
    true,
  );
  await dashboard.attachExistingOffer(credentialName, source.name, target.name);
  const second = client(target.endpoint);
  await authorizeTypedResource(page, second, {
    resource: mock.profiles.oauthScoped,
    clientId: registration!,
    scopes: ["fixture:scoped"],
  });
  await invoke(second, mock.profiles.oauthScoped, "alpha-user");
  const offset = mock.events.length;
  await second.refresh();
  // The nominal expiry path is covered separately; this asserts the downstream
  // refresh does not lose this selection or change the target resource/scope.
  await invoke(second, mock.profiles.oauthScoped, "alpha-user");
  expect(
    mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call")
      .every(
        (event) =>
          event.resource === mock.profiles.oauthScoped &&
          event.outcome === "authorized",
      ),
  ).toBe(true);
  await invoke(first, mock.profiles.oauthAlpha, "alpha-user");
});

test("enforced: organization gateway retains two unrelated OAuth providers", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const alpha = await dashboard.remote(
    `fixture-alpha-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthAlpha,
    "user",
  );
  const beta = await dashboard.remote(
    `fixture-beta-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthBeta,
    "user",
    true,
  );
  // Establish both upstream grants through their real consent UI first.
  // A gateway membership failure must not be explained by unconfigured callers.
  for (const [server, resource, principal] of [
    [alpha, mock.profiles.oauthAlpha, "alpha-user"],
    [beta, mock.profiles.oauthBeta, "beta-user"],
  ] as const) {
    const direct = client(server.endpoint);
    await authorize(page, direct);
    await invoke(direct, resource, principal);
  }
  const names = [alpha.name, beta.name];
  const slugs = [alpha, beta].map((server) =>
    new URL(server.serverURL).pathname.split("/").at(-1)!,
  );
  const graphSalt = randomUUID().replaceAll("-", "");
  const checkpoint = async (phase: string) => {
    const graph = await captureAuthGraph(
      dashboard.ownedProjects.map((project) => project.url),
      graphSalt,
    );
    await test.info().attach(`fixture-auth-graph-${phase}`, {
      body: JSON.stringify(graph),
      contentType: "application/json",
    });
    return sourceTopology(graph, names);
  };
  const before = await checkpoint("before-stored-control");
  const stored = await new GatewayActions({ page, dashboard }).create(
    "fixture-control-stored",
    names,
  );
  for (const phase of ["stored-control", "automatic-organization"] as const) {
    try {
      const endpoint =
        phase === "stored-control"
          ? stored.endpoint
          : await dashboard.organizationGateway();
      const value = client(endpoint);
      await authorize(page, value, consentObserver(page, slugs, phase));
      const echoes = await gatewayEchoes(
        value,
        slugs,
        inventoryObserver(phase),
      );
      for (const [index, tool] of echoes.entries()) {
        const offset = mock.events.length;
        const result = await tool.call({ marker: "gateway-isolation" });
        expect(result).toMatchObject({ isError: false });
        const calls = mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call");
        const expected =
          index === 0 ? mock.profiles.oauthAlpha : mock.profiles.oauthBeta;
        expect(calls.length).toBeGreaterThan(0);
        expect(
          calls.every(
            (event) =>
              event.resource === expected && event.outcome === "authorized",
          ),
          "GATEWAY_CREDENTIAL_LEAK: a rejected wrong-member attempt must not be hidden by a successful retry",
        ).toBe(true);
        for (const call of calls) {
          expect(call).toMatchObject({
            provider: index === 0 ? "alpha" : "beta",
            principal: index === 0 ? "alpha-user" : "beta-user",
          });
        }
      }
    } finally {
      expect(
        await checkpoint(`after-${phase}`),
        "Gateway controls must preserve both source bindings",
      ).toEqual(before);
    }
  }
  for (const provider of ["alpha", "beta"]) {
    expect(
      mock.events.some(
        (event) =>
          event.method === "tools/call" &&
          event.provider === provider &&
          event.outcome === "authorized",
      ),
      `GATEWAY_ISOLATION_FAILURE: ${provider} must receive its own grant`,
    ).toBe(true);
  }
});

test("enforced: one server offers personal OAuth and a shared credential", async ({
  page,
}) => {
  const server = await dashboard.remote(
    `fixture-multiple-${randomUUID().slice(0, 8)}`,
    mock.profiles.multiple,
    "user",
  );
  const personal = client(server.endpoint);
  await authorize(page, personal);
  await invoke(personal, mock.profiles.multiple, "alpha-user");
  await page.goto(`${server.serverURL}/settings`);
  const sharedOffer = `fixture-shared-${randomUUID().slice(0, 8)}`;
  await dashboard.addStaticOffer(sharedOffer);
  const value = client(server.endpoint);
  const flow = await value.beginAuthorization();
  try {
    await page.goto(flow.authorizationUrl);
    await expect(
      page.getByRole("radio", { name: /fixture-shared/ }),
      "MULTI_OFFER_REGRESSION: consent must expose the shared offer, not silently choose it",
    ).toBeVisible();
    await page.getByRole("radio", { name: /fixture-shared/ }).check();
    await page
      .getByRole("button", { name: "Give access", exact: true })
      .click();
    await flow.complete();
    authorizations.add(value);
  } catch (error) {
    await flow.cancel();
    throw error;
  }
  await value.initialize();
  await invoke(value, mock.profiles.multiple, "static-user");
  await value.refresh();
  await invoke(value, mock.profiles.multiple, "static-user");
  await invoke(personal, mock.profiles.multiple, "alpha-user");
  const key = await dashboard.issueAgentKey(
    server.name,
    sharedOffer,
    mock.profiles.multiple,
  );
  const agent = new FixtureMcpClient({
    endpoint: server.endpoint,
    bearerToken: key,
    allowInsecureLoopback: true,
    trustedOrigins: [new URL(server.endpoint).origin],
  });
  clients.push(agent);
  await agent.initialize();
  await invoke(agent, mock.profiles.multiple, "static-user");
});
