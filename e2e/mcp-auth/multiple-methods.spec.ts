import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import {
  addStaticOffer,
  disableCredential,
  detachCredential,
  rotateStaticCredential,
} from "./credential-actions.ts";
import type { FixtureMcpClient } from "./client.ts";

// Each topology is built in its own UI-created project, never from seeded state.
test.describe.configure({ mode: "default" });
let sharedName = "";
let alternateName = "";
test.beforeEach(() => {
  sharedName = `fixture-shared-static-${randomUUID().slice(0, 8)}`;
  alternateName = `fixture-alternate-static-${randomUUID().slice(0, 8)}`;
});

async function checkpoint(
  auth: AuthScenario,
  stage: string,
  personalNames: string[],
) {
  test.info().annotations.push({ type: "auth-stage", description: stage });
  const graph = await auth.graph();
  for (const name of personalNames) {
    const source = graph.servers.find((server) => server.name === name);
    expect(
      source,
      `FIXTURE_MODEL_MISMATCH: ${stage}: source exists`,
    ).toBeDefined();
    expect
      .soft(
        source!.offers.filter(
          (offer) =>
            offer.type === "remote_session" &&
            offer.enabled &&
            !offer.deleted &&
            !offer.bindingDeleted,
        ),
        `PERSONAL_BINDING_LOSS_REGRESSION: ${stage}: ${name} must retain its personal alternative (checkpoint, not writer attribution)`,
      )
      .toHaveLength(1);
  }
  return graph;
}

async function sharedTopology(auth: AuthScenario) {
  const { dashboard, page, mock } = auth;
  const resource = mock.profiles.multiMethod;
  const a = await dashboard.remote(
    `fixture-methods-a-${randomUUID().slice(0, 8)}`,
    resource,
    "user",
  );
  await checkpoint(auth, "A-created-before-login", [a.name]);
  const personalAName = await dashboard.personalCredentialName();
  const personalA = auth.client(a.endpoint);
  await auth.authorize(personalA);
  await checkpoint(auth, "A-logged-in", [a.name]);
  await auth.invoke(personalA, resource, "alpha-user");
  await page.goto(`${a.serverURL}/settings`);
  await addStaticOffer(page, sharedName, "fixture-static-secret");
  await checkpoint(auth, "A-static-added", [a.name]);
  const resourceB = mock.profiles.multiMethodBeta;
  const b = await dashboard.remote(
    `fixture-methods-b-${randomUUID().slice(0, 8)}`,
    resourceB,
    "user",
  );
  await checkpoint(auth, "B-saved-before-login", [a.name, b.name]);
  const personalBName = await dashboard.personalCredentialName();
  const personalB = auth.client(b.endpoint);
  await auth.authorize(personalB);
  await checkpoint(auth, "B-logged-in", [a.name, b.name]);
  await auth.invoke(personalB, resourceB, "beta-user");
  await page.goto(`${b.serverURL}/settings`);
  await dashboard.attachExistingOffer(sharedName, a.name, b.name);
  const graph = await checkpoint(auth, "shared-static-attached", [
    a.name,
    b.name,
  ]);
  const sources = [a.name, b.name].map((name) =>
    graph.servers.find((server) => server.name === name),
  );
  for (const source of sources) {
    expect(
      source,
      "FIXTURE_MODEL_MISMATCH: both shared-method sources must exist",
    ).toBeDefined();
    expect
      .soft(
        source!.offers.filter(
          (offer) =>
            offer.type === "remote_session" &&
            offer.enabled &&
            !offer.deleted &&
            !offer.bindingDeleted,
        ),
        "PERSONAL_BINDING_LOSS_REGRESSION: after shared static attachment each personal alternative must remain; earlier checkpoints locate the first observed loss",
      )
      .toHaveLength(1);
  }
  const shared = sources.map((source) =>
    source!.offers.filter(
      (offer) =>
        offer.type === "static_credential" &&
        offer.enabled &&
        !offer.deleted &&
        !offer.bindingDeleted,
    ),
  );
  for (const offers of shared) expect(offers).toHaveLength(1);
  expect(
    shared[0]![0]!.client,
    "FIXTURE_MODEL_MISMATCH: reuse the actual typed client, not a same-named copy",
  ).toBe(shared[1]![0]!.client);
  const sharedA = auth.client(a.endpoint);
  const sharedB = auth.client(b.endpoint);
  await auth.authorize(sharedA, sharedName);
  await auth.authorize(sharedB, sharedName);
  await auth.invoke(sharedA, resource, "static-user");
  await auth.invoke(sharedB, resourceB, "static-user");
  return {
    a,
    b,
    resource,
    resourceB,
    personalA,
    personalB,
    personalAName,
    personalBName,
    sharedA,
    sharedB,
  };
}

async function assertDenied(
  auth: AuthScenario,
  client: FixtureMcpClient,
  tool: string,
) {
  // Call the previously discovered tool directly: losing discovery alone is not enforcement.
  test
    .info()
    .annotations.push({ type: "auth-stage", description: "runtime-denial" });
  const offset = auth.mock.events.length;
  const result = await client
    .callTool(tool, { marker: "must-not-reach-upstream" })
    .then(
      (value) => ({ kind: "result" as const, value }),
      (error) => ({ kind: "error" as const, error: String(error) }),
    );
  if (result.kind === "result") {
    expect(JSON.stringify(result.value)).toMatch(
      /unauthoriz|forbidden|access.denied|reconnect|credential.*(?:disabled|detached|unavailable)/i,
    );
    expect(
      result.value,
      "RUNTIME_ENFORCEMENT_FAILURE: denied credential must not produce a successful tool result",
    ).toMatchObject({ isError: true });
  } else
    expect(
      result.error,
      "RUNTIME_ENFORCEMENT_FAILURE: require auth denial, not a timeout or transport outage",
    ).toMatch(/\b(?:401|403)\b/i);
  expect(
    auth.mock.events
      .slice(offset)
      .filter(
        (event) =>
          event.method === "tools/call" && event.outcome === "authorized",
      ),
    "RUNTIME_ENFORCEMENT_FAILURE: disabled or detached identity reached upstream",
  ).toHaveLength(0);
}

async function toolName(client: FixtureMcpClient) {
  const { tools } = await client.listTools();
  const tool = tools.find((tool) => tool.name.endsWith("fixture_echo"));
  expect(tool).toBeDefined();
  return tool!.name;
}

test("enforced methods: personal plus two static offers select each explicitly and retain all sessions", async ({
  auth,
}) => {
  const { dashboard, page, mock } = auth;
  const resource = mock.profiles.multiMethod;
  const server = await dashboard.remote(
    `fixture-three-offers-${randomUUID().slice(0, 8)}`,
    resource,
    "user",
  );
  const personalName = await dashboard.personalCredentialName();
  const original = auth.client(server.endpoint);
  await auth.authorize(original);
  await auth.invoke(original, resource, "alpha-user");
  await page.goto(`${server.serverURL}/settings`);
  await addStaticOffer(page, sharedName, "fixture-static-secret");
  await addStaticOffer(page, alternateName, "fixture-static-alt-secret");
  const selections: Array<[string, string]> = [
    [sharedName, "static-user"],
    [alternateName, "static-alt-user"],
    [personalName, "alpha-user"],
  ];
  const sessions: Array<[FixtureMcpClient, string]> = [
    [original, "alpha-user"],
  ];
  for (const [choice, principal] of selections) {
    const client = auth.client(server.endpoint);
    await auth.authorize(client, choice);
    sessions.push([client, principal]);
    for (const [existing, expected] of sessions)
      await auth.invoke(existing, resource, expected);
  }
  for (const [client, principal] of sessions) {
    await client.refresh();
    await auth.invoke(client, resource, principal);
  }
});

test("enforced methods: same static client on two servers keeps personal alternatives isolated", async ({
  auth,
}) => {
  const topology = await sharedTopology(auth);
  for (const [server, choice, resource, principal] of [
    [topology.a, topology.personalAName, topology.resource, "alpha-user"],
    [topology.b, topology.personalBName, topology.resourceB, "beta-user"],
  ] as const) {
    // Resolve the personal alternative on this exact source. A sibling may
    // have the same display name; it must never supply the expected identity.
    const current = await auth.graph();
    const personal = current.servers
      .find((source) => source.name === server.name)
      ?.offers.filter(
        (offer) =>
          offer.type === "remote_session" &&
          offer.name === choice &&
          offer.enabled &&
          !offer.deleted &&
          !offer.bindingDeleted,
      );
    expect(
      personal?.length === 1,
      "PERSONAL_BINDING_LOSS_REGRESSION: fresh personal selection requires its own source's live offer, never a same-named sibling client",
    ).toBe(true);
    const client = auth.client(server.endpoint);
    await auth.authorize(client, choice);
    await auth.invoke(client, resource, principal);
  }
  for (const [client, resource] of [
    [topology.sharedA, topology.resource],
    [topology.sharedB, topology.resourceB],
  ] as const) {
    await client.refresh();
    await auth.invoke(client, resource, "static-user");
  }
  await auth.invoke(topology.personalA, topology.resource, "alpha-user");
  await auth.invoke(topology.personalB, topology.resourceB, "beta-user");
});

test("enforced methods: disabling shared client denies both existing users while personal sessions continue", async ({
  auth,
}) => {
  const t = await sharedTopology(auth);
  const toolA = await toolName(t.sharedA);
  const toolB = await toolName(t.sharedB);
  await auth.page.goto(`${t.a.serverURL}/settings`);
  await disableCredential(auth.page, sharedName, true);
  await test.step("runtime: global disable denies A and B", async () => {
    await assertDenied(auth, t.sharedA, toolA);
    await assertDenied(auth, t.sharedB, toolB);
    await auth.invoke(t.personalA, t.resource, "alpha-user");
    await auth.invoke(t.personalB, t.resourceB, "beta-user");
  });
});

test("enforced methods: detaching shared client from A denies A but leaves B and personal choices usable", async ({
  auth,
}) => {
  const t = await sharedTopology(auth);
  const tool = await toolName(t.sharedA);
  await auth.page.goto(`${t.a.serverURL}/settings`);
  await detachCredential(auth.page, sharedName);
  await test.step("runtime: detach is server-local", async () => {
    await assertDenied(auth, t.sharedA, tool);
    await auth.invoke(t.sharedB, t.resourceB, "static-user");
    await t.sharedB.refresh();
    await auth.invoke(t.sharedB, t.resourceB, "static-user");
    await auth.invoke(t.personalA, t.resource, "alpha-user");
    await auth.invoke(t.personalB, t.resourceB, "beta-user");
  });
});

test("enforced methods: rotating shared stored value updates both servers without changing other selections", async ({
  auth,
}) => {
  const t = await sharedTopology(auth);
  await auth.page.goto(`${t.a.serverURL}/settings`);
  await addStaticOffer(auth.page, alternateName, "fixture-static-secret");
  const independent = auth.client(t.a.endpoint);
  await auth.authorize(independent, alternateName);
  await auth.invoke(independent, t.resource, "static-user");
  await auth.page.goto(`${t.a.serverURL}/settings`);
  await rotateStaticCredential(
    auth.page,
    sharedName,
    "fixture-static-alt-secret",
    true,
  );
  await test.step("runtime: shared rotation changes identity only for selected shared client", async () => {
    for (const [client, resource] of [
      [t.sharedA, t.resource],
      [t.sharedB, t.resourceB],
    ] as const) {
      await auth.invoke(client, resource, "static-alt-user");
      await client.refresh();
      await auth.invoke(client, resource, "static-alt-user");
    }
    await auth.invoke(independent, t.resource, "static-user");
    await auth.invoke(t.personalA, t.resource, "alpha-user");
    await auth.invoke(t.personalB, t.resourceB, "beta-user");
  });
});

for (const sameResource of [true, false]) {
  test(`enforced methods: adding same-provider ${sameResource ? "same-resource" : "different-resource"} server preserves the original personal offer`, async ({
    auth,
  }) => {
    const resourceA = auth.mock.profiles.multiMethod;
    const resourceB = sameResource
      ? resourceA
      : auth.mock.profiles.multiMethodTwin;
    const a = await auth.dashboard.remote(
      `fixture-sibling-a-${randomUUID().slice(0, 8)}`,
      resourceA,
      "user",
    );
    await checkpoint(auth, "A-created-before-login", [a.name]);
    const originalName = await auth.dashboard.personalCredentialName();
    const existing = auth.client(a.endpoint);
    await auth.authorize(existing);
    await auth.invoke(existing, resourceA, "alpha-user");
    const before = await checkpoint(auth, "A-logged-in", [a.name]);
    expect(
      before.servers
        .find((server) => server.name === a.name)
        ?.offers.filter(
          (offer) =>
            offer.type === "remote_session" &&
            !offer.deleted &&
            !offer.bindingDeleted,
        ),
    ).toHaveLength(1);
    const originalGraph = before.servers.find(
      (server) => server.name === a.name,
    )!;
    const originalOffer = originalGraph.offers.find(
      (offer) =>
        offer.type === "remote_session" &&
        !offer.deleted &&
        !offer.bindingDeleted,
    )!;
    const b = await auth.dashboard.remote(
      `fixture-sibling-b-${randomUUID().slice(0, 8)}`,
      resourceB,
      "user",
    );
    const after = await checkpoint(auth, "B-saved-before-login", [
      a.name,
      b.name,
    ]);
    const retained = after.servers.find((server) => server.name === a.name)!;
    test.info().annotations.push({ type: "gap-hypothesis", description: "G8" });
    expect
      .soft(
        retained.signInIssuer,
        "G8: B must not change A's downstream sign-in issuer",
      )
      .toBe(originalGraph.signInIssuer);
    expect
      .soft(
        retained.derivedUpstreamIssuer,
        "G8: B must not repoint A's derived upstream issuer",
      )
      .toBe(originalGraph.derivedUpstreamIssuer);
    expect
      .soft(
        retained.offers.find((offer) => offer.client === originalOffer.client),
        "G8: retain the original typed client and registration, not a replacement",
      )
      .toMatchObject({
        deleted: false,
        bindingDeleted: false,
        registration: originalOffer.registration,
        issuer: originalOffer.issuer,
      });
    expect
      .soft(
        after.servers
          .find((server) => server.name === a.name)
          ?.offers.filter(
            (offer) =>
              offer.type === "remote_session" &&
              !offer.deleted &&
              !offer.bindingDeleted,
          ),
        "SIBLING_BINDING_LOSS_REGRESSION: B setup must retain A's persisted personal binding",
      )
      .toHaveLength(1);
    const second = auth.client(b.endpoint);
    await auth.authorize(second);
    await checkpoint(auth, "B-logged-in", [a.name, b.name]);
    await auth.invoke(second, resourceB, "alpha-user");
    await auth.page.goto(`${a.serverURL}/settings`);
    await expect(
      auth.page
        .getByRole("button", { name: /Each person's own account/ })
        .filter({ has: auth.page.getByText(originalName, { exact: true }) }),
      "SIBLING_OFFER_LOSS_REGRESSION: configuring B must not remove A's existing personal credential",
    ).toBeVisible();
    await auth.invoke(existing, resourceA, "alpha-user");
    const fresh = auth.client(a.endpoint);
    await auth.authorize(fresh);
    await auth.invoke(fresh, resourceA, "alpha-user");
  });
}
