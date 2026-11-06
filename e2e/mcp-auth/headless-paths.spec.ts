import { randomUUID } from "node:crypto";
import { test, expect } from "./fixtures.ts";
import { assertGrant } from "./auth-setup-actions.ts";
import {
  createWorkloadAgent,
  exchangeWorkload,
  startWorkloadIssuer,
} from "./workload-issuer.ts";

test.use({ trace: "off", screenshot: "off", video: "off" });

test("G10 headless: personal-only offer requires setup; disconnecting its connection fails the workload closed", async ({
  auth,
}) => {
  test.setTimeout(240_000);
  const issuer = await startWorkloadIssuer();
  const { dashboard, mock } = auth;
  try {
    const server = await dashboard.remote(
      `fixture-headless-personal-${randomUUID().slice(0, 8)}`,
      mock.profiles.multiMethod,
      "user",
    );
    // The workload resolves its assigned owner's personal connection. Establish
    // it through real consent so the wizard can create the assignment.
    const owner = auth.client(server.endpoint);
    await auth.authorize(owner);
    await auth.invoke(owner, mock.profiles.multiMethod, "alpha-user");
    await createWorkloadAgent(auth, issuer, server);
    const metadata = await auth.client(server.endpoint).discover();
    // Protocol-only mock control: revoke at the upstream, not in application state.
    mock.revokeTokens();
    await expect(owner.listTools()).rejects.toThrow(/HTTP (401|403)/);
    await test.step("Personal-only headless grant returns setup_required without upstream execution", async () => {
      const offset = mock.events.length;
      test.info().annotations.push({
        type: "auth-stage",
        description: "workload-personal-setup-required-exchange",
      });
      const denied = await exchangeWorkload(issuer, metadata, true);
      expect(denied.status).toBe(400);
      expect(denied.error).toBe("setup_required");
      expect(denied.access_token === undefined).toBe(true);
      expect(
        mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call"),
      ).toHaveLength(0);
    });
    test.info().annotations.push({
      type: "auth-stage",
      description: "workload-personal-setup-required-proved",
    });
    await test.step("Owner reconnects the personal credential through actual browser consent", async () => {
      const initialGrants = mock.events.filter(
        (event) => event.kind === "token",
      );
      expect(initialGrants).toHaveLength(1);
      const initialGrant = initialGrants[0]!;
      expect(initialGrant.clientId).toBeTruthy();
      expect(initialGrant.scopes).toBeDefined();
      const offset = mock.events.length;
      const flow = await owner.beginAuthorization();
      try {
        await auth.page.goto(flow.authorizationUrl);
        const card = auth.page.locator("li[data-remote-client]");
        await expect(card).toHaveCount(1);
        // Approving downstream consent alone can reuse the revoked upstream
        // connection. Explicitly replace it through the real card controls.
        const disconnect = card.locator('button[value="disconnect"]');
        const connect = card.getByRole("button", {
          name: /^(Connect|Reconnect)$/,
        });
        await expect
          .poll(
            async () =>
              (await disconnect.isVisible()) || (await connect.isVisible()),
          )
          .toBe(true);
        if (await disconnect.isVisible()) await disconnect.click();
        await expect(connect).toBeVisible();
        await connect.click();
        await auth.page
          .getByRole("button", { name: "Approve mock access", exact: true })
          .click();
        await expect(card.locator("[data-card-status]")).toContainText(
          "Service connected",
        );
        assertGrant(
          auth,
          initialGrant.clientId!,
          mock.profiles.multiMethod,
          initialGrant.scopes!,
          offset,
        );
        expect(
          mock.events
            .slice(offset)
            .filter((event) => event.kind === "register"),
        ).toHaveLength(0);
        await auth.page
          .getByRole("button", { name: "Give access", exact: true })
          .click();
        await flow.complete();
        auth.trackGrant(owner);
      } catch (error) {
        await flow.cancel();
        throw error;
      }
      await owner.initialize();
      await auth.invoke(owner, mock.profiles.multiMethod, "alpha-user");
      const calls = mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      expect(calls.length).toBeGreaterThan(0);
      for (const event of calls) {
        expect(event).toMatchObject({
          clientId: initialGrant.clientId,
          resource: mock.profiles.multiMethod,
          principal: "alpha-user",
          outcome: "authorized",
        });
      }
    });
    await test.step("Disconnect deleted the attached session: the workload fails closed until it is attached again", async () => {
      test.info().annotations.push({
        type: "auth-stage",
        description: "workload-attachment-deleted-exchange",
      });
      const offset = mock.events.length;
      const token = await exchangeWorkload(issuer, metadata, true);
      expect(token.status).toBe(400);
      expect(token.errorCode).toBe("setup_required");
      expect(
        mock.events.slice(offset).filter((event) => event.kind === "mcp"),
      ).toEqual([]);
    });
  } finally {
    try {
      await test.info().attach("workload-protocol-journal", {
        body: JSON.stringify({
          jwksReads: issuer.jwksReads,
          exchanges: issuer.exchanges,
        }),
        contentType: "application/json",
      });
    } finally {
      await issuer.close();
    }
  }
});
