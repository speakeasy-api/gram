import { test, expect, type AuthScenario } from "./fixtures.ts";
import {
  addStaticOffer,
  addCallerOffer,
  disableCredential,
  detachCredential,
  rotateStaticCredential,
} from "./credential-actions.ts";
import type { FixtureMcpClient } from "./client.ts";

test.describe.configure({ mode: "default" });
const staticName = "fixture-agent-primary";
const otherName = "fixture-agent-independent";
const callerName = "fixture-caller-method";
const supplied = { "X-Client-Fixture": "fixture-user-secret" };

async function denial(auth: AuthScenario, action: () => Promise<unknown>) {
  test
    .info()
    .annotations.push({ type: "auth-stage", description: "runtime-denial" });
  const offset = auth.mock.events.length;
  const result = await action().then(
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
          event.outcome === "authorized" && event.method === "tools/call",
      ),
  ).toHaveLength(0);
}

async function isolatedInvoke(
  auth: AuthScenario,
  client: FixtureMcpClient,
  principal: string,
  channel: string,
) {
  const offset = auth.mock.events.length;
  await auth.invoke(client, auth.mock.profiles.multiMethod, principal);
  const calls = auth.mock.events
    .slice(offset)
    .filter((event) => event.method === "tools/call");
  for (const event of calls) {
    expect(
      event.outcome,
      "CREDENTIAL_LEAK: the multi-method upstream rejects mixed credential channels",
    ).toBe("authorized");
    expect(
      event.headerNames
        ?.filter((name) =>
          ["authorization", "x-fixture-key", "x-fixture-user"].includes(
            name.toLowerCase(),
          ),
        )
        .map((name) => name.toLowerCase()),
    ).toEqual([channel]);
  }
}

test("enforced methods: personal static and caller offers explicitly select one channel without credential leakage", async ({
  auth,
}) => {
  const { dashboard, page, mock } = auth;
  const server = await dashboard.remote(
    "fixture-three-channels",
    mock.profiles.multiMethod,
    "user",
  );
  const personalName = await dashboard.personalCredentialName();
  const bootstrap = auth.client(server.endpoint);
  await auth.authorize(bootstrap);
  await auth.invoke(bootstrap, mock.profiles.multiMethod, "alpha-user");
  await page.goto(`${server.serverURL}/settings`);
  await addStaticOffer(page, staticName, "fixture-static-secret");
  await addCallerOffer(page, callerName);
  const sessions: Array<[FixtureMcpClient, string, string]> = [];
  for (const [choice, principal, channel] of [
    [personalName, "alpha-user", "authorization"],
    [staticName, "static-user", "x-fixture-key"],
    [callerName, "pass-through-user", "x-fixture-user"],
  ] as const) {
    // Even non-caller selections receive the available caller header. It must
    // not leak upstream unless that method was explicitly selected.
    const client = auth.client(server.endpoint, supplied);
    await auth.authorize(client, choice);
    sessions.push([client, principal, channel]);
    for (const [existing, identity, expectedChannel] of sessions)
      await isolatedInvoke(auth, existing, identity, expectedChannel);
  }
  for (const [client, principal, channel] of sessions) {
    await client.refresh();
    await isolatedInvoke(auth, client, principal, channel);
  }
});

test("enforced methods: caller method shared across two sources never borrows another caller's header or personal login", async ({
  auth,
}) => {
  const { dashboard, page, mock } = auth;
  const a = await dashboard.remote(
    "fixture-caller-a",
    mock.profiles.multiMethod,
    "user",
  );
  const personalA = auth.client(a.endpoint);
  await auth.authorize(personalA);
  await page.goto(`${a.serverURL}/settings`);
  await addCallerOffer(page, callerName);
  const b = await dashboard.remote(
    "fixture-caller-b",
    mock.profiles.multiMethod,
    "user",
  );
  const personalB = auth.client(b.endpoint);
  await auth.authorize(personalB);
  await page.goto(`${b.serverURL}/settings`);
  await dashboard.attachExistingOffer(callerName, a.name, b.name);
  const withHeader = auth.client(a.endpoint, supplied);
  await auth.authorize(withHeader, callerName);
  await isolatedInvoke(auth, withHeader, "pass-through-user", "x-fixture-user");
  const missing = auth.client(b.endpoint);
  // Consent must succeed independently of tool execution; initialization may
  // reject missing material, but UI setup failures are never accepted as denial.
  const flow = await missing.beginAuthorization();
  let granted = false;
  const { tools: targetTools } = await personalB.listTools();
  const targetTool = targetTools.find((tool) =>
    tool.name.endsWith("fixture_echo"),
  );
  expect(targetTool).toBeDefined();
  try {
    await page.goto(flow.authorizationUrl);
    const option = page
      .locator("[data-upstream-auth-option]")
      .filter({ has: page.getByText(callerName, { exact: true }) })
      .getByRole("radio");
    await option.check();
    await page
      .getByRole("button", { name: "Give access", exact: true })
      .click();
    await flow.complete();
    granted = true;
    await denial(auth, async () => {
      await missing.initialize();
      return missing.callTool(targetTool!.name, {});
    });
    await isolatedInvoke(
      auth,
      withHeader,
      "pass-through-user",
      "x-fixture-user",
    );
    await auth.invoke(personalA, mock.profiles.multiMethod, "alpha-user");
    await auth.invoke(personalB, mock.profiles.multiMethod, "alpha-user");
    const bWithHeader = auth.client(b.endpoint, supplied);
    await auth.authorize(bWithHeader, callerName);
    await isolatedInvoke(
      auth,
      bWithHeader,
      "pass-through-user",
      "x-fixture-user",
    );
  } finally {
    await flow.cancel();
    if (granted) await missing.revoke();
  }
});

async function issue(
  auth: AuthScenario,
  server: { name: string; endpoint: string },
  choice: string,
  cleanup: string[],
) {
  const key = await auth.dashboard.issueAgentKey(
    server.name,
    choice,
    auth.mock.profiles.multiMethod,
  );
  const client = auth.client(server.endpoint, {
    Authorization: `Bearer ${key}`,
  });
  await auth.dashboard.finishAgentSetup();
  await auth.page
    .getByRole("link", { name: "Credentials", exact: true })
    .click();
  await expect(
    auth.page.getByRole("button", { name: /^Revoke API key / }),
  ).toHaveCount(1);
  cleanup.push(auth.page.url());
  await client.initialize();
  return client;
}

for (const mutation of ["disable", "detach", "rotate"] as const) {
  test(`enforced agents: two independent keys select distinct static offers and ${mutation} affects only the selected offer`, async ({
    auth,
  }) => {
    const { dashboard, page, mock } = auth;
    const server = await dashboard.remote(
      `fixture-agent-${mutation}`,
      mock.profiles.multiMethod,
      "user",
    );
    const personal = auth.client(server.endpoint);
    await auth.authorize(personal);
    await page.goto(`${server.serverURL}/settings`);
    await addStaticOffer(page, staticName, "fixture-static-secret");
    await addStaticOffer(page, otherName, "fixture-static-alt-secret");
    const cleanup: string[] = [];
    let primaryFailure: unknown;
    try {
      const first = await issue(auth, server, staticName, cleanup);
      const second = await issue(auth, server, otherName, cleanup);
      await isolatedInvoke(auth, first, "static-user", "x-fixture-key");
      await isolatedInvoke(auth, second, "static-alt-user", "x-fixture-key");
      const { tools } = await first.listTools();
      const tool = tools.find((tool) =>
        tool.name.endsWith("fixture_echo"),
      )!.name;
      await page.goto(`${server.serverURL}/settings`);
      if (mutation === "disable") await disableCredential(page, staticName);
      else if (mutation === "detach") await detachCredential(page, staticName);
      else
        await rotateStaticCredential(
          page,
          staticName,
          "fixture-static-alt-secret",
        );
      if (mutation === "rotate")
        await isolatedInvoke(auth, first, "static-alt-user", "x-fixture-key");
      else await denial(auth, () => first.callTool(tool, {}));
      await isolatedInvoke(auth, second, "static-alt-user", "x-fixture-key");
      if (mutation === "rotate") {
        await page.goto(`${server.serverURL}/settings`);
        await rotateStaticCredential(page, staticName, "fixture-static-secret");
        await isolatedInvoke(auth, first, "static-user", "x-fixture-key");
        await isolatedInvoke(auth, second, "static-alt-user", "x-fixture-key");
      }
      await auth.invoke(personal, mock.profiles.multiMethod, "alpha-user");
    } catch (error) {
      primaryFailure = error;
    } finally {
      const cleanupFailures: unknown[] = [];
      for (const url of cleanup) {
        try {
          await page.goto(url);
          await page.getByRole("button", { name: /^Revoke API key / }).click();
          const dialog = page.getByRole("dialog", {
            name: "Revoke API key?",
            exact: true,
          });
          await dialog
            .getByRole("button", { name: "Confirm revoke", exact: true })
            .click();
          await expect(dialog).not.toBeVisible();
        } catch (error) {
          cleanupFailures.push(error);
        }
      }
      if (cleanupFailures.length > 0) {
        await test.info().attach("agent-cleanup-error", {
          body: `${cleanupFailures.length} fixture-issued keys could not be revoked through the UI`,
          contentType: "text/plain",
        });
        if (primaryFailure === undefined)
          throw new Error(
            "UI_AGENT_CLEANUP_BLOCKER: fixture-issued key revocation failed",
          );
      }
    }
    if (primaryFailure !== undefined) throw primaryFailure;
  });
}
