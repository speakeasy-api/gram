import { test, expect, type AuthScenario } from "./fixtures.ts";
import { assertGrant } from "./auth-setup-actions.ts";

// Keep all provisioning in the dashboard and all fault injection in the issuer.
// These cases do not install routes, seed sessions, or change the browser store.
test.use({ trace: "off", screenshot: "off", video: "off" });

async function connected(auth: AuthScenario, name: string) {
  const resource = auth.mock.profiles.oauthAlpha;
  const server =
    await test.step("Create and consent to a new UI-provisioned server", async () =>
      auth.dashboard.remote(name, resource, "user"));
  const client = auth.client(server.endpoint);
  await auth.authorize(client);
  await auth.invoke(client, resource, "alpha-user");
  const { tools } = await client.listTools();
  const echo = tools.find((tool) => tool.name.endsWith("fixture_echo"));
  expect(echo).toBeDefined();
  return { client, resource, echo: echo!.name };
}

function refreshes(auth: AuthScenario, offset: number) {
  return auth.mock.events
    .slice(offset)
    .filter(
      (event) =>
        event.kind === "refresh" &&
        event.provider === "alpha" &&
        event.outcome === "success",
    );
}

// Let the advertised lifetime elapse without making a request that could refresh
// the grant. This is foreground refresh coverage, not background keepalive.
async function elapse() {
  await new Promise((resolve) => setTimeout(resolve, 2_200));
}

test.describe("short-lived upstream grants", () => {
  test.use({ mockOptions: { accessTokenLifetimeSeconds: 2 } });

  test("G12: reversible issuer outage is unavailable, never anonymous, then recovers", async ({
    auth,
  }) => {
    const { client, resource } = await connected(auth, "fixture-g12-outage");
    const offset = auth.mock.events.length;
    auth.mock.setTokenFailure("alpha", {
      status: 503,
      error: "temporarily_unavailable",
    });
    try {
      await test.step("Let the upstream token expire, then observe the outage refusal", async () => {
        await elapse();
        await expect(client.listTools()).rejects.toThrow(/HTTP 503/);
        expect(client.transcript.at(-1)).toMatchObject({
          operation: "tools/list",
          status: 503,
        });
        expect(
          auth.mock.events
            .slice(offset)
            .filter(
              (event) => event.kind === "mcp" && event.outcome === "authorized",
            ),
        ).toHaveLength(0);
        expect(
          auth.mock.events
            .slice(offset)
            .some((event) => event.principal === "anonymous"),
        ).toBe(false);
      });
    } finally {
      auth.mock.setTokenFailure("alpha", null);
    }
    await test.step("Retry the same caller grant once at the advertised recovery deadline", async () => {
      const outage = client.transcript.at(-1);
      const seconds = outage?.retryAfterSeconds;
      const receivedAt = outage?.responseReceivedAtMs;
      if (
        outage?.operation !== "tools/list" ||
        outage.status !== 503 ||
        seconds === undefined ||
        !Number.isInteger(seconds) ||
        seconds < 1 ||
        seconds > 300 ||
        receivedAt === undefined ||
        !Number.isFinite(receivedAt) ||
        receivedAt < 0 ||
        receivedAt > performance.now()
      ) {
        throw new Error(
          "G12_RECOVERY_DEADLINE_MISSING_OR_INVALID: expected outage 503 with Retry-After in 1..300 seconds and monotonic receipt time",
        );
      }
      const deadline = receivedAt + seconds * 1000;
      // One advertised wait, not a retry loop or a probe of restored availability.
      await new Promise((resolve) =>
        setTimeout(
          resolve,
          Math.max(0, Math.ceil(deadline - performance.now())) + 1,
        ),
      );
      expect(
        performance.now(),
        "G12_RECOVERY_DEADLINE_EARLY: the single recovery attempt must not precede Retry-After",
      ).toBeGreaterThanOrEqual(deadline);
      const recoveryOffset = auth.mock.events.length;
      await auth.invoke(client, resource, "alpha-user");
      const recovered = refreshes(auth, recoveryOffset);
      expect(
        recovered.length,
        "G12_RECOVERY_REFRESH_MISSING: the single recovery invocation must perform a fresh successful refresh",
      ).toBeGreaterThan(0);
      for (const refresh of recovered) {
        expect(refresh).toMatchObject({
          provider: "alpha",
          resource,
          outcome: "success",
        });
        expect([...(refresh.scopes ?? [])].sort()).toEqual(
          ["fixture:read", "openid"].sort(),
        );
      }
      // invoke asserts the exact resource and alpha-user principal on real tool forwarding.
      expect(
        auth.mock.events
          .slice(offset)
          .filter((event) => event.kind === "authorize"),
      ).toHaveLength(0);
      expect(
        auth.mock.events
          .slice(offset)
          .some((event) => event.principal === "anonymous"),
      ).toBe(false);
    });
  });

  test("G13: disconnect after refresh rotation does not strand the surviving caller", async ({
    auth,
  }) => {
    const { client, resource, echo } = await connected(
      auth,
      "fixture-g13-disconnect",
    );
    await elapse();
    const offset = auth.mock.events.length;
    const pause = auth.mock.pauseNextRefresh("alpha");
    const controller = new AbortController();
    // Attach a rejection handler immediately; the cancellation is deliberate.
    const pending = client
      .callTool(echo, { marker: "g13-disconnected" }, controller.signal)
      .then(
        () => "completed",
        () => "disconnected",
      );
    try {
      await test.step("Pause after consuming the old refresh token and disconnect HTTP caller", async () => {
        let started = false;
        void pause.started.then(() => {
          started = true;
        });
        await expect.poll(() => started, { timeout: 8_000 }).toBe(true);
        controller.abort();
        expect(await pending).toBe("disconnected");
      });
    } finally {
      controller.abort();
      pause.release();
      await pending;
    }
    await test.step("Use the same downstream grant after the rotating response is released", async () => {
      await auth.invoke(client, resource, "alpha-user");
      expect(refreshes(auth, offset).length).toBeGreaterThan(0);
      expect(
        auth.mock.events
          .slice(offset)
          .filter((event) => event.kind === "authorize"),
      ).toHaveLength(0);
      expect(
        auth.mock.events
          .slice(offset)
          .some((event) => event.principal === "anonymous"),
      ).toBe(false);
    });
  });
});

test("G14: expires_in zero is unreported lifetime, not immediate expiration", async ({
  auth,
}) => {
  // remotesessions/token_response.go:249–267 treats zero as unreported, not expired.
  auth.mock.setAdvertisedLifetime(0);
  const offset = auth.mock.events.length;
  try {
    const { client, resource } = await connected(
      auth,
      "fixture-g14-zero-lifetime",
    );
    await test.step("Use a zero-lifetime response repeatedly without a refresh loop", async () => {
      await auth.invoke(client, resource, "alpha-user");
      await auth.invoke(client, resource, "alpha-user");
      expect(refreshes(auth, offset)).toHaveLength(0);
    });
    await test.step("A real revocation still refuses access despite unreported expiry", async () => {
      auth.mock.revokeTokens();
      await expect(client.listTools()).rejects.toThrow(/HTTP (401|403)/);
      expect(
        auth.mock.events
          .slice(offset)
          .some((event) => event.principal === "anonymous"),
      ).toBe(false);
    });
    await test.step("Reconnect through browser consent after the real revocation", async () => {
      const initialGrants = auth.mock.events
        .slice(offset)
        .filter((event) => event.kind === "token");
      expect(initialGrants).toHaveLength(1);
      const initialGrant = initialGrants[0]!;
      expect(initialGrant.clientId).toBeTruthy();
      expect(initialGrant.scopes).toBeDefined();
      const reconnectOffset = auth.mock.events.length;
      const flow = await client.beginAuthorization();
      try {
        await auth.page.goto(flow.authorizationUrl);
        const card = auth.page.locator("li[data-remote-client]");
        await expect(card).toHaveCount(1);
        // Downstream consent alone can reuse the revoked upstream connection.
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
          resource,
          initialGrant.scopes!,
          reconnectOffset,
        );
        expect(
          auth.mock.events
            .slice(reconnectOffset)
            .filter((event) => event.kind === "register"),
        ).toHaveLength(0);
        await auth.page
          .getByRole("button", { name: "Give access", exact: true })
          .click();
        await flow.complete();
        auth.trackGrant(client);
      } catch (error) {
        await flow.cancel();
        throw error;
      }
      await client.initialize();
      await auth.invoke(client, resource, "alpha-user");
    });
  } finally {
    auth.mock.setAdvertisedLifetime(null);
  }
});
