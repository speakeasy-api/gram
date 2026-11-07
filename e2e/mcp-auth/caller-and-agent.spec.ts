import { randomUUID } from "node:crypto";
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
let staticName = "";
let otherName = "";
let callerName = "";
test.beforeEach(() => {
  staticName = `fixture-agent-primary-${randomUUID().slice(0, 8)}`;
  otherName = `fixture-agent-independent-${randomUUID().slice(0, 8)}`;
  callerName = `fixture-caller-method-${randomUUID().slice(0, 8)}`;
});
const supplied = { "X-Client-Fixture": "fixture-user-secret" };

async function denial(
  auth: AuthScenario,
  action: () => Promise<unknown>,
  missingHeaderClient?: FixtureMcpClient,
) {
  test
    .info()
    .annotations.push({ type: "auth-stage", description: "runtime-denial" });
  const offset = auth.mock.events.length;
  const protocolOffset = missingHeaderClient?.transcript.length;
  const result = await action().then(
    (value) => ({ value }),
    (error) => ({ error: String(error) }),
  );
  const upstream = auth.mock.events.slice(offset);
  // Check forwarding even when the rejection itself has an unexpected shape.
  expect(
    upstream.filter(
      (event) =>
        event.outcome === "authorized" && event.method === "tools/call",
    ),
  ).toHaveLength(0);
  if (
    missingHeaderClient &&
    "error" in result &&
    result.error === "Error: initialize failed (HTTP 400)"
  ) {
    // Only the explicit missing-header control may classify this admission
    // rejection; a generic HTTP 400 is not an authentication denial.
    expect(missingHeaderClient.transcript.slice(protocolOffset)).toEqual([
      {
        operation: "initialize",
        status: 400,
        sessionSent: false,
        protocolCode: -32600,
      },
    ]);
    expect(upstream).toHaveLength(0);
  } else if ("error" in result)
    expect(result.error).toMatch(
      /401|403|unauthoriz|forbidden|credential|access.denied|returned JSON-RPC error/i,
    );
  else expect(result.value).toMatchObject({ isError: true });
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
    `fixture-three-channels-${randomUUID().slice(0, 8)}`,
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
    `fixture-caller-a-${randomUUID().slice(0, 8)}`,
    mock.profiles.multiMethod,
    "user",
  );
  const personalA = auth.client(a.endpoint);
  await auth.authorize(personalA);
  await page.goto(`${a.serverURL}/settings`);
  await addCallerOffer(page, callerName);
  const b = await dashboard.remote(
    `fixture-caller-b-${randomUUID().slice(0, 8)}`,
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
  const { tools: targetTools } = await personalB.listTools();
  const targetTool = targetTools.find((tool) =>
    tool.name.endsWith("fixture_echo"),
  );
  expect(targetTool).toBeDefined();
  await auth.authorize(missing, callerName, undefined, { initialize: false });
  try {
    await denial(
      auth,
      async () => {
        await missing.initialize();
        return missing.callTool(targetTool!.name, {});
      },
      missing,
    );
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
    await auth.revoke(missing);
  }
});

type IssuedAgentCleanup = {
  credentialsURL: string;
  overviewHref: string;
  name: string;
  revokeLabel: string;
};

async function issue(
  auth: AuthScenario,
  server: { name: string; endpoint: string },
  choice: string,
  cleanup: IssuedAgentCleanup[],
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
  const overviewHref = await auth.page
    .getByRole("link", { name: "Overview", exact: true })
    .getAttribute("href");
  if (!overviewHref)
    throw new Error("UI_AGENT_CLEANUP_BLOCKER: overview link must be rendered");
  const agentLink = auth.page
    .getByRole("link")
    .and(auth.page.locator(`a[href="${overviewHref}"]`))
    .filter({ hasNotText: /^Overview$/ });
  await expect(agentLink).toHaveCount(1);
  const name = (await agentLink.innerText()).trim();
  const revokeLabel = await auth.page
    .getByRole("button", { name: /^Revoke API key / })
    .getAttribute("aria-label");
  if (!name || !revokeLabel)
    throw new Error(
      "UI_AGENT_CLEANUP_BLOCKER: exact agent and key must be recorded",
    );
  cleanup.push({
    credentialsURL: auth.page.url(),
    overviewHref,
    name,
    revokeLabel,
  });
  await client.initialize();
  return client;
}

for (const mutation of ["disable", "detach", "rotate"] as const) {
  test(`enforced agents: two independent keys select distinct static offers and ${mutation} affects only the selected offer`, async ({
    auth,
  }) => {
    const { dashboard, page, mock } = auth;
    const server = await dashboard.remote(
      `fixture-agent-${mutation}-${randomUUID().slice(0, 8)}`,
      mock.profiles.multiMethod,
      "user",
    );
    const personal = auth.client(server.endpoint);
    await auth.authorize(personal);
    await page.goto(`${server.serverURL}/settings`);
    await addStaticOffer(page, staticName, "fixture-static-secret");
    await addStaticOffer(page, otherName, "fixture-static-alt-secret");
    const cleanup: IssuedAgentCleanup[] = [];
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
      for (const issued of cleanup) {
        try {
          // Stay in the mounted dashboard: full document loads can leave the
          // development shell empty before the next exact-key cleanup.
          // Server settings has a detail-only sidebar, not the project groups.
          const back = page.getByRole("link", {
            name: "Back to all servers",
            exact: true,
          });
          if (await back.isVisible()) await back.click();
          const expand = page.getByRole("button", {
            name: "Expand Identity",
            exact: true,
          });
          const collapse = page.getByRole("button", {
            name: "Collapse Identity",
            exact: true,
          });
          await expect(expand.or(collapse)).toBeVisible();
          if (await expand.isVisible()) await expand.click();
          const agentsHref = `${new URL(dashboard.projectURL).pathname}/identities/agents`;
          // The sidebar link includes a Preview badge in its accessible name.
          // Pin the rendered title and exact route, not the composite label.
          const agents = page
            .getByRole("link")
            .and(
              page.locator(
                `li[data-sidebar="menu-item"] ul a[href="${agentsHref}"]`,
              ),
            )
            .filter({ has: page.getByText("Agents", { exact: true }) });
          await expect(agents).toHaveCount(1);
          await agents.click();
          await page
            .getByPlaceholder("Search agents…", { exact: true })
            .fill(issued.name);
          // The roster also has a View action with this same destination.
          // Select the exact recorded agent name, not either matching link.
          const agent = page
            .getByRole("link", { name: issued.name, exact: true })
            .and(page.locator(`a[href="${issued.overviewHref}"]`));
          await expect(agent).toHaveCount(1);
          await agent.click();
          await page
            .getByRole("link", { name: "Credentials", exact: true })
            .click();
          await expect(page).toHaveURL(issued.credentialsURL);
          const revoke = page.getByRole("button", {
            name: issued.revokeLabel,
            exact: true,
          });
          await expect(revoke).toHaveCount(1);
          await revoke.click();
          const dialog = page.getByRole("dialog", {
            name: "Revoke API key?",
            exact: true,
          });
          const [response] = await Promise.all([
            page.waitForResponse(
              (response) =>
                response.request().method() === "DELETE" &&
                new URL(response.url()).pathname === "/rpc/keys.revoke",
            ),
            dialog
              .getByRole("button", { name: "Confirm revoke", exact: true })
              .click(),
          ]);
          expect(
            response.status() === 200,
            "UI_AGENT_CLEANUP_BLOCKER: revocation must succeed",
          ).toBe(true);
          await expect(dialog).not.toBeVisible();
          await expect(revoke).toHaveCount(0);
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
