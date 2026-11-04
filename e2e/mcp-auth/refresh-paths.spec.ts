import { test, expect, type AuthScenario } from "./fixtures.ts";

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
    await test.step("Retry the same caller grant after restoring the issuer", async () => {
      await auth.invoke(client, resource, "alpha-user");
      expect(refreshes(auth, offset).length).toBeGreaterThan(0);
      expect(
        auth.mock.events
          .slice(offset)
          .filter((event) => event.kind === "authorize"),
      ).toHaveLength(0);
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
  auth.mock.setAdvertisedLifetime(0);
  try {
    const { client, resource } = await connected(
      auth,
      "fixture-g14-zero-lifetime",
    );
    const offset = auth.mock.events.length;
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
      await auth.authorize(client);
      await auth.invoke(client, resource, "alpha-user");
    });
  } finally {
    auth.mock.setAdvertisedLifetime(null);
  }
});
