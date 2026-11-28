import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import type { FixtureMcpClient } from "./client.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import { addStaticOffer, disableCredential } from "./credential-actions.ts";

test.describe.configure({ mode: "default" });
let betaName = "";
let staticName = "";
test.beforeEach(() => {
  betaName = `fixture-beta-personal-offer-${randomUUID().slice(0, 8)}`;
  staticName = `fixture-dual-static-offer-${randomUUID().slice(0, 8)}`;
});

async function setup(auth: AuthScenario) {
  const { dashboard, page, mock } = auth;
  const resource = mock.profiles.dualProvider;
  const a = await dashboard.remote(
    `fixture-two-providers-a-${randomUUID().slice(0, 8)}`,
    resource,
    "user",
  );
  const alphaName = await dashboard.personalCredentialName();
  const alpha = auth.client(a.endpoint);
  await auth.authorize(alpha);
  await auth.invoke(alpha, resource, "alpha-user");

  // A different provider creates the real beta registration without inducing
  // the separately covered same-provider sibling mirror mutation.
  const beforeBeta = mock.events.length;
  const b = await dashboard.remote(
    `fixture-beta-registration-source-${randomUUID().slice(0, 8)}`,
    mock.profiles.oauthBeta,
    "user",
  );
  const registrations = mock.events
    .slice(beforeBeta)
    .filter((event) => event.kind === "register" && event.provider === "beta");
  expect(
    registrations,
    "UI_REGISTRATION_BLOCKER: source B must create one exact beta registration",
  ).toHaveLength(1);
  const betaRegistration = registrations[0]!.clientId;
  expect(betaRegistration).toBeTruthy();
  const originalBeta = auth.client(b.endpoint);
  await auth.authorize(originalBeta);
  await auth.invoke(originalBeta, mock.profiles.oauthBeta, "beta-user");
  await auth.invoke(alpha, resource, "alpha-user");

  await page.goto(`${a.serverURL}/settings`);
  await addPersonalOffer(page, betaName, betaRegistration!);
  // Retain the original alpha mirror: adding an alternative must not replace
  // or turn it off, unlike the auxiliary-header single-personal recipe.
  await expect(
    page.getByRole("switch", { name: `Turn off ${alphaName}`, exact: true }),
  ).toBeChecked();
  await addStaticOffer(page, staticName, "fixture-static-secret");
  return {
    a,
    b,
    resource,
    alphaName,
    alpha,
    originalBeta,
    betaRegistration: betaRegistration!,
  };
}

async function authorizeBeta(
  auth: AuthScenario,
  client: FixtureMcpClient,
  registration: string,
  requireNewResourceGrant: boolean,
) {
  const offset = auth.mock.events.length;
  let failure: unknown;
  try {
    await auth.authorize(client, betaName);
  } catch (error) {
    failure = error;
  }
  const authorizations = auth.mock.events
    .slice(offset)
    .filter((event) => event.kind === "authorize");
  await test.info().attach("named-beta-authorization-evidence", {
    body: JSON.stringify(
      {
        expected: {
          provider: "beta",
          clientId: registration,
          resource: auth.mock.profiles.dualProvider,
          scopes: ["fixture:read"],
        },
        authorizations,
        consentOrInitializationFailed: failure !== undefined,
      },
      null,
      2,
    ),
    contentType: "application/json",
  });
  test.info().annotations.push({
    type: "auth-provider-evidence",
    description: JSON.stringify({
      choice: betaName,
      authorizationAttempts: authorizations.length,
      consentOrInitializationFailed: failure !== undefined,
    }),
  });
  // With no upstream authorization evidence, retain the original UI/runtime
  // error. A selector timeout alone does not establish a provider-routing bug.
  if (authorizations.length === 0 && failure !== undefined) throw failure;
  if (requireNewResourceGrant)
    expect(
      authorizations.length,
      "RESOURCE_GRANT_REGRESSION: beta's B grant cannot substitute for the dual resource grant",
    ).toBeGreaterThan(0);
  for (const event of authorizations) {
    expect(event.provider, "PERSONAL_PROVIDER_SELECTION_REGRESSION").toBe(
      "beta",
    );
    expect(event.clientId, "PERSONAL_REGISTRATION_SELECTION_REGRESSION").toBe(
      registration,
    );
    expect(event.resource, "PERSONAL_RESOURCE_SELECTION_REGRESSION").toBe(
      auth.mock.profiles.dualProvider,
    );
    expect(event.scopes, "PERSONAL_SCOPE_SELECTION_REGRESSION").toEqual([
      "fixture:read",
    ]);
    expect(event.outcome, "UPSTREAM_AUTHORIZATION_FAILURE").toBe("success");
  }
  if (failure !== undefined) throw failure;
}

async function checkAll(
  auth: AuthScenario,
  sessions: Array<[FixtureMcpClient, string, string]>,
) {
  for (const [client, resource, principal] of sessions) {
    const offset = auth.mock.events.length;
    await auth.invoke(client, resource, principal);
    const calls = auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call");
    for (const event of calls) {
      expect(
        event.headerNames
          ?.filter((name) =>
            ["authorization", "x-fixture-key", "x-fixture-user"].includes(
              name.toLowerCase(),
            ),
          )
          .map((name) => name.toLowerCase()),
      ).toEqual([
        principal === "static-user" ? "x-fixture-key" : "authorization",
      ]);
    }
  }
}

test("enforced multiple OAuth: alpha personal beta personal and static choices preserve old fresh and refreshed sessions", async ({
  auth,
}) => {
  const t = await setup(auth);
  const sessions: Array<[FixtureMcpClient, string, string]> = [
    [t.alpha, t.resource, "alpha-user"],
    [t.originalBeta, auth.mock.profiles.oauthBeta, "beta-user"],
  ];
  const beta = auth.client(t.a.endpoint);
  await authorizeBeta(auth, beta, t.betaRegistration, true);
  sessions.push([beta, t.resource, "beta-user"]);
  await checkAll(auth, sessions);
  const staticClient = auth.client(t.a.endpoint);
  await auth.authorize(staticClient, staticName);
  sessions.push([staticClient, t.resource, "static-user"]);
  await checkAll(auth, sessions);
  const freshAlpha = auth.client(t.a.endpoint);
  await auth.authorize(freshAlpha, t.alphaName);
  sessions.push([freshAlpha, t.resource, "alpha-user"]);
  const freshBeta = auth.client(t.a.endpoint);
  await authorizeBeta(auth, freshBeta, t.betaRegistration, false);
  sessions.push([freshBeta, t.resource, "beta-user"]);
  const freshStatic = auth.client(t.a.endpoint);
  await auth.authorize(freshStatic, staticName);
  sessions.push([freshStatic, t.resource, "static-user"]);
  for (const [client] of sessions) {
    await client.refresh();
    await checkAll(auth, sessions);
  }
});

test("enforced multiple OAuth: same administrator beta personal offer attaches to two dual-provider sources without replacing alpha or B", async ({
  auth,
}) => {
  // Create the manual target before any personal mirror. It needs no default
  // OAuth offer; the exact existing beta offer is attached later via the UI.
  const c = await auth.dashboard.remote(
    `fixture-two-providers-c-${randomUUID().slice(0, 8)}`,
    auth.mock.profiles.dualProvider,
    "none",
  );
  await addStaticOffer(
    auth.page,
    "fixture-target-static",
    "fixture-static-secret",
  );
  const t = await setup(auth);
  const betaA = auth.client(t.a.endpoint);
  await authorizeBeta(auth, betaA, t.betaRegistration, true);
  await auth.page.goto(`${c.serverURL}/settings`);
  // attachExistingOffer verifies the same credential's Used on list contains
  // exactly A and C. A second registration or credential clone cannot satisfy it.
  await auth.dashboard.attachExistingOffer(betaName, t.a.name, c.name);
  const betaC = auth.client(c.endpoint);
  await authorizeBeta(auth, betaC, t.betaRegistration, false);
  const staticC = auth.client(c.endpoint);
  await auth.authorize(staticC, "fixture-target-static");
  const alphaA = auth.client(t.a.endpoint);
  await auth.authorize(alphaA, t.alphaName);
  const sessions: Array<[FixtureMcpClient, string, string]> = [
    [t.alpha, t.resource, "alpha-user"],
    [alphaA, t.resource, "alpha-user"],
    [betaA, t.resource, "beta-user"],
    [betaC, t.resource, "beta-user"],
    [staticC, t.resource, "static-user"],
    [t.originalBeta, auth.mock.profiles.oauthBeta, "beta-user"],
  ];
  await checkAll(auth, sessions);
  for (const client of [betaA, betaC, t.originalBeta]) {
    await client.refresh();
    await checkAll(auth, sessions);
  }
});

test("enforced multiple OAuth: disabling selected beta offer denies its session while alpha static and beta source B remain valid", async ({
  auth,
}) => {
  const t = await setup(auth);
  const beta = auth.client(t.a.endpoint);
  await authorizeBeta(auth, beta, t.betaRegistration, true);
  await auth.invoke(beta, t.resource, "beta-user");
  const staticClient = auth.client(t.a.endpoint);
  await auth.authorize(staticClient, staticName);
  const { tools } = await beta.listTools();
  const echo = tools.find((tool) => tool.name.endsWith("fixture_echo"));
  expect(echo).toBeDefined();
  await auth.page.goto(`${t.a.serverURL}/settings`);
  await disableCredential(auth.page, betaName);
  test.info().annotations.push({
    type: "auth-stage",
    description: "runtime-deny-beta-only",
  });
  const offset = auth.mock.events.length;
  const result = await beta.callTool(echo!.name, {}).then(
    (value) => ({ value }),
    (error) => ({ error: String(error) }),
  );
  if ("error" in result)
    expect(result.error).toMatch(
      /401|403|unauthoriz|forbidden|credential|access.denied|returned JSON-RPC error/i,
    );
  else expect(result.value).toMatchObject({ isError: true });
  expect(
    auth.mock.events
      .slice(offset)
      .filter(
        (event) =>
          event.method === "tools/call" && event.outcome === "authorized",
      ),
  ).toHaveLength(0);
  const sessions: Array<[FixtureMcpClient, string, string]> = [
    [t.alpha, t.resource, "alpha-user"],
    [staticClient, t.resource, "static-user"],
    [t.originalBeta, auth.mock.profiles.oauthBeta, "beta-user"],
  ];
  await checkAll(auth, sessions);
  for (const [client] of sessions) await client.refresh();
  await checkAll(auth, sessions);
});
