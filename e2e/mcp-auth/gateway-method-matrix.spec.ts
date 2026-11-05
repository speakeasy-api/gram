import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { gatewayEchoes } from "./gateway.ts";
import {
  sourceTopology,
  inventoryObserver,
  consentObserver,
} from "./gateway-control-diagnostics.ts";
import {
  addStaticOffer,
  disableCredential,
  detachCredential,
  rotateStaticCredential,
} from "./credential-actions.ts";

// Independent cases: an earlier discovery failure must not skip a later mutation scenario.
test.describe.configure({ mode: "default" });
test.setTimeout(600_000);
let shared = "";
let alternate = "";
test.beforeEach(() => {
  shared = `matrix-shared-static-${randomUUID().slice(0, 8)}`;
  alternate = `matrix-alternate-static-${randomUUID().slice(0, 8)}`;
});
type Echo = Awaited<ReturnType<typeof gatewayEchoes>>[number];

// Snapshot aliases share the fixture salt; a replacement is not the original grant.
async function personalGraph(auth: AuthScenario, stage: string) {
  const graph = await auth.graph();
  await test.info().attach(`gateway-personal-graph-${stage}`, {
    body: JSON.stringify(graph),
    contentType: "application/json",
  });
  return graph;
}

async function originalPersonal(
  auth: AuthScenario,
  name: string,
  stage: string,
) {
  const graph = await personalGraph(auth, stage);
  const servers = graph.servers.filter((server) => server.name === name);
  expect(servers, "FIXTURE_MODEL_MISMATCH: exact personal source").toHaveLength(
    1,
  );
  const source = servers[0]!;
  const offers = source.offers.filter(
    (offer) =>
      offer.type === "remote_session" &&
      offer.enabled &&
      !offer.deleted &&
      !offer.bindingDeleted,
  );
  expect(
    offers,
    "FIXTURE_MODEL_MISMATCH: one original personal binding",
  ).toHaveLength(1);
  return { server: source.server, client: offers[0]!.client };
}

async function topology(auth: AuthScenario) {
  return test.step("UI setup and direct personal/static positive controls", async () => {
    const { dashboard, page, mock } = auth;
    const resource = mock.profiles.multiMethod;
    const a = await dashboard.remote(
      `matrix-a-${randomUUID().slice(0, 8)}`,
      resource,
      "user",
    );
    const personalA = auth.client(a.endpoint);
    await auth.authorize(personalA);
    await auth.invoke(personalA, resource, "alpha-user");
    const originalA = await originalPersonal(auth, a.name, "a-login");
    await page.goto(`${a.serverURL}/settings`);
    await addStaticOffer(page, shared, "fixture-static-secret");
    await addStaticOffer(page, alternate, "fixture-static-alt-secret");
    await personalGraph(auth, "a-static");
    const b = await dashboard.remote(
      `matrix-b-${randomUUID().slice(0, 8)}`,
      resource,
      "user",
    );
    await personalGraph(auth, "b-save-before-login");
    const personalB = auth.client(b.endpoint);
    await auth.authorize(personalB);
    await auth.invoke(personalB, resource, "alpha-user");
    const originalB = await originalPersonal(auth, b.name, "b-login");
    await page.goto(`${b.serverURL}/settings`);
    // UI Used on verification proves one credential object, not matching clones.
    await dashboard.attachExistingOffer(shared, a.name, b.name);
    await personalGraph(auth, "shared-attachment");
    const staticA = auth.client(a.endpoint);
    const staticB = auth.client(b.endpoint);
    const alternateA = auth.client(a.endpoint);
    await auth.authorize(staticA, shared);
    await auth.authorize(staticB, shared);
    await auth.authorize(alternateA, alternate);
    await auth.invoke(staticA, resource, "static-user");
    await auth.invoke(staticB, resource, "static-user");
    await auth.invoke(alternateA, resource, "static-alt-user");
    const slug = (server: typeof a) =>
      new URL(server.serverURL).pathname.split("/").at(-1)!;
    return {
      a,
      b,
      resource,
      personalA,
      personalB,
      originalA,
      originalB,
      staticA,
      staticB,
      alternateA,
      aSlug: slug(a),
      bSlug: slug(b),
    };
  });
}

async function gateway(
  auth: AuthScenario,
  endpoint: string,
  choice: string,
  slugs: string[],
) {
  return test.step(`Gateway consent and discovery: ${choice}`, async () => {
    const client = auth.client(endpoint);
    await auth.authorize(
      client,
      choice,
      consentObserver(auth.page, slugs, `typed-${choice}`),
    );
    // A gateway discovery failure is a precondition failure, not evidence that a
    // subsequent credential mutation was enforced. Mutation steps run only later.
    const tools = await gatewayEchoes(
      client,
      slugs,
      inventoryObserver(`typed-${choice}`),
    );
    return { client, tools };
  });
}

async function invoke(
  auth: AuthScenario,
  echo: Echo,
  principal: "static-user" | "static-alt-user",
) {
  const marker = randomUUID();
  const offset = auth.mock.events.length;
  const result = await echo.call({ marker });
  expect(result).toMatchObject({ isError: false });
  expect(JSON.stringify(result)).toContain(marker);
  const calls = auth.mock.events
    .slice(offset)
    .filter((event) => event.method === "tools/call");
  expect(
    calls.length,
    "GATEWAY_ROUTING_FAILURE: the qualified member must execute upstream",
  ).toBeGreaterThan(0);
  for (const event of calls) {
    expect(
      event,
      "GATEWAY_CREDENTIAL_LEAK: every attempt must use only the selected material",
    ).toMatchObject({
      resource: auth.mock.profiles.multiMethod,
      principal,
      outcome: "authorized",
    });
    expect(event.provider).toBeUndefined();
  }
}

async function denied(auth: AuthScenario, echo: Echo) {
  const offset = auth.mock.events.length;
  // Reuse the previously authorized qualified call, not discovery disappearance.
  const outcome = await echo.call({ marker: "must-not-execute" }).then(
    (result) => ({ kind: "result" as const, result }),
    (error: unknown) => ({ kind: "error" as const, error: String(error) }),
  );
  if (outcome.kind === "result")
    expect(outcome.result).toMatchObject({ isError: true });
  else
    expect(
      outcome.error,
      "Require authorization rejection, not timeout/network failure",
    ).toMatch(
      /HTTP (401|403)|unauthoriz|forbidden|credential|access.denied|reconnect/i,
    );
  expect(
    auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call"),
    "GATEWAY_ENFORCEMENT_FAILURE: detached/disabled material must not reach any upstream",
  ).toEqual([]);
}

async function personalControls(
  auth: AuthScenario,
  t: Awaited<ReturnType<typeof topology>>,
  refresh = false,
) {
  for (const [client, original] of [
    [t.personalA, t.originalA],
    [t.personalB, t.originalB],
  ] as const) {
    const transcriptOffset = client.transcript.length;
    const upstreamOffset = auth.mock.events.length;
    let phase: "refresh" | "invoke" = refresh ? "refresh" : "invoke";
    try {
      if (refresh) await client.refresh();
      phase = "invoke";
      await auth.invoke(client, t.resource, "alpha-user");
    } catch (error) {
      const events = client.transcript.slice(transcriptOffset);
      const last = events.at(-1);
      // Only the exact transport refusal may continue. Assertion errors, failed
      // discovery/configuration, and identity/header failures remain hard.
      const listRefusal =
        phase === "invoke" &&
        error instanceof Error &&
        (error.message === "tools/list failed (HTTP 401)" ||
          error.message === "tools/list failed (HTTP 403)") &&
        last?.operation === "tools/list" &&
        (last.status === 401 || last.status === 403) &&
        error.message === `tools/list failed (HTTP ${last.status})`;
      const refreshRefusal =
        phase === "refresh" &&
        error instanceof Error &&
        error.message === "token failed (HTTP 400)" &&
        last?.operation === "token" &&
        last.status === 400 &&
        last.protocolCode === "invalid_grant" &&
        (last.reason === "upstream_connection_missing_or_expired" ||
          last.reason === "session_authorization_invalid");
      if (!listRefusal && !refreshRefusal) throw error;
      expect(
        auth.mock.events
          .slice(upstreamOffset)
          .filter((event) => event.method === "tools/call"),
        "PERSONAL_REFUSAL_LEAK: a refused personal control must not execute upstream",
      ).toEqual([]);
      const graph = await personalGraph(
        auth,
        `refused-${original.server}-${phase}`,
      );
      const sources = graph.servers.filter(
        (server) => server.server === original.server,
      );
      expect(
        sources,
        "FIXTURE_MODEL_MISMATCH: original personal source exists",
      ).toHaveLength(1);
      const bindings = sources[0]!.offers.filter(
        (offer) => offer.client === original.client,
      );
      const failedAvailability =
        bindings.length === 0 ||
        bindings.every((offer) => offer.deleted || offer.bindingDeleted);
      if (!failedAvailability) throw error;
      test.info().annotations.push({
        type: refreshRefusal
          ? "personal-invoke-blocked"
          : "personal-availability-failed",
        description: `${original.server}/${original.client}: original binding lost; ${phase} refused; personal control NOT passed${refreshRefusal ? "; invocation blocked by refresh refusal" : ""}`,
      });
      // Keep the test RED, but measure the other personal/alternate controls
      // and shared-static mutations without reauthorizing or replacing grants.
      expect
        .soft(
          failedAvailability,
          `PERSONAL_BINDING_LOSS_REGRESSION: unaffected original personal grant ${original.server}/${original.client} MUST remain available`,
        )
        .toBe(false);
    }
  }
  await auth.invoke(t.alternateA, t.resource, "static-alt-user");
}

for (const kind of ["project", "stored"] as const) {
  test(`enforced gateway methods: one shared credential on two sources through ${kind} gateway`, async ({
    auth,
  }) => {
    const t = await topology(auth);
    const ui = new GatewayActions(auth);
    if (kind === "project") {
      await test.step("Same-source stored gateway control before automatic typed-static discovery", async () => {
        const names = [t.a.name, t.b.name];
        const before = sourceTopology(await auth.graph(), names);
        const stored = await ui.create(
          `matrix-project-control-${randomUUID().slice(0, 8)}`,
          names,
        );
        const control = await gateway(auth, stored.endpoint, shared, [
          t.aSlug,
          t.bSlug,
        ]);
        for (const echo of control.tools)
          await invoke(auth, echo, "static-user");
        expect(
          sourceTopology(await auth.graph(), names),
          "Stored control must not change source identity or bindings",
        ).toEqual(before);
      });
    }
    const endpoint =
      kind === "project"
        ? await ui.projectEndpoint()
        : (
            await ui.create(`matrix-shared-${randomUUID().slice(0, 8)}`, [
              t.a.name,
              t.b.name,
            ])
          ).endpoint;
    const caller = await gateway(auth, endpoint, shared, [t.aSlug, t.bSlug]);
    for (const echo of caller.tools) await invoke(auth, echo, "static-user");
    await test.step("Existing direct and gateway selections survive refresh", async () => {
      await caller.client.refresh();
      for (const echo of caller.tools) await invoke(auth, echo, "static-user");
      for (const client of [t.staticA, t.staticB]) {
        await client.refresh();
        await auth.invoke(client, t.resource, "static-user");
      }
      await personalControls(auth, t, true);
    });
  });
}

async function overlapping(auth: AuthScenario) {
  const t = await topology(auth);
  const ui = new GatewayActions(auth);
  const first = await ui.create(`matrix-g1-${randomUUID().slice(0, 8)}`, [
    t.a.name,
    t.b.name,
  ]);
  const second = await ui.create(`matrix-g2-${randomUUID().slice(0, 8)}`, [
    t.a.name,
  ]);
  const g1 = await gateway(auth, first.endpoint, shared, [t.aSlug, t.bSlug]);
  const g2 = await gateway(auth, second.endpoint, shared, [t.aSlug]);
  const other = await gateway(auth, second.endpoint, alternate, [t.aSlug]);
  await test.step("All gateway selections work before mutation", async () => {
    for (const echo of [...g1.tools, ...g2.tools])
      await invoke(auth, echo, "static-user");
    await invoke(auth, other.tools[0]!, "static-alt-user");
    await personalControls(auth, t);
  });
  return { t, g1, g2, other };
}

test("enforced gateway methods: overlapping stored gateways retain distinct named offers and direct personal grants through refresh", async ({
  auth,
}) => {
  const { t, g1, g2, other } = await overlapping(auth);
  for (const caller of [g1, g2, other]) await caller.client.refresh();
  for (const echo of [...g1.tools, ...g2.tools])
    await invoke(auth, echo, "static-user");
  await invoke(auth, other.tools[0]!, "static-alt-user");
  await personalControls(auth, t, true);
});

for (const operation of ["detach", "disable", "rotate"] as const) {
  test(`enforced gateway methods: ${operation} chosen shared offer respects scope across overlapping stored gateways`, async ({
    auth,
  }) => {
    const { t, g1, g2, other } = await overlapping(auth);
    await test.step(`UI mutation: ${operation} shared offer on A`, async () => {
      await auth.page.goto(`${t.a.serverURL}/settings`);
      if (operation === "detach") await detachCredential(auth.page, shared);
      else if (operation === "disable")
        await disableCredential(auth.page, shared, true);
      else
        await rotateStaticCredential(
          auth.page,
          shared,
          "fixture-static-alt-secret",
          true,
        );
    });
    await test.step(`Runtime: ${operation} scope, cached calls, and unrelated selection`, async () => {
      const a1 = g1.tools[0]!;
      const b1 = g1.tools[1]!;
      const a2 = g2.tools[0]!;
      if (operation === "rotate") {
        for (const echo of [a1, b1, a2])
          await invoke(auth, echo, "static-alt-user");
        for (const client of [t.staticA, t.staticB])
          await auth.invoke(client, t.resource, "static-alt-user");
        for (const caller of [g1, g2]) await caller.client.refresh();
        for (const echo of [a1, b1, a2])
          await invoke(auth, echo, "static-alt-user");
      } else {
        await denied(auth, a1);
        await denied(auth, a2);
        if (operation === "disable") await denied(auth, b1);
        else {
          // Detachment removes only the A edge. The same object remains on B.
          await invoke(auth, b1, "static-user");
          await auth.invoke(t.staticB, t.resource, "static-user");
        }
      }
      await invoke(auth, other.tools[0]!, "static-alt-user");
      await other.client.refresh();
      await invoke(auth, other.tools[0]!, "static-alt-user");
      await personalControls(auth, t, true);
    });
  });
}
