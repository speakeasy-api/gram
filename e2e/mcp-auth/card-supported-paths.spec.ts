import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";

// This baseline retains the issuer bridge created by the normal User Identity
// journey. Enforcement remains enabled by fixtures.ts; this is not legacy mode.
const scopes = ["fixture:read", "openid"];

test.describe.configure({ mode: "default", retries: 0 });
test.setTimeout(150_000);

async function supportedConnection(auth: AuthScenario) {
  return test.step(
    "Supported prerequisite: UI User Identity server, enabled typed offer and issuer bridge",
    async () => {
      const resource = auth.mock.profiles.oauthAlpha;
      const server = await auth.dashboard.remote(
        `auth-e2e-supported-${randomUUID().slice(0, 8)}`,
        resource,
        "user",
      );
      const credentialName = await auth.dashboard.personalCredentialName();
      expect(credentialName).not.toBe("");
      const graph = await auth.graph();
      const servers = graph.servers.filter(
        (entry) => entry.name === server.name,
      );
      expect(servers).toHaveLength(1);
      const offers = servers[0]!.offers.filter(
        (offer) =>
          offer.type === "remote_session" &&
          offer.enabled &&
          !offer.deleted &&
          !offer.bindingDeleted,
      );
      expect(offers).toHaveLength(1);
      expect(offers[0]).toMatchObject({
        registrationDeleted: false,
        issuerDeleted: false,
        issuerBridge: "present",
      });
      expect(offers[0]!.registration).toBeTruthy();
      expect(offers[0]!.issuer).toBeTruthy();
      const registrations = auth.mock.events.filter(
        (event) => event.kind === "register",
      );
      expect(registrations).toHaveLength(1);
      expect(registrations[0]).toMatchObject({
        provider: "alpha",
        outcome: "success",
      });
      const clientId = registrations[0]!.clientId!;
      expect(clientId).toBeTruthy();
      return { server, resource, clientId, credentialName };
    },
    { timeout: 50_000 },
  );
}

function assertGrant(
  auth: AuthScenario,
  clientId: string,
  resource: string,
  offset: number,
) {
  const events = auth.mock.events
    .slice(offset)
    .filter((event) => event.kind === "authorize" || event.kind === "token");
  expect(events.filter((event) => event.kind === "authorize")).toHaveLength(1);
  expect(events.filter((event) => event.kind === "token")).toHaveLength(1);
  for (const event of events) {
    expect(event).toMatchObject({
      provider: "alpha",
      clientId,
      resource,
      outcome: "success",
    });
    expect([...(event.scopes ?? [])].sort()).toEqual([...scopes].sort());
  }
}

async function connectedCard(
  auth: AuthScenario,
  setup: Awaited<ReturnType<typeof supportedConnection>>,
) {
  await test.step(
    "Supported initial grant: exact registration, resource, scopes and working tool call",
    async () => {
      const offset = auth.mock.events.length;
      const caller = auth.client(setup.server.endpoint);
      await auth.authorize(caller);
      assertGrant(auth, setup.clientId, setup.resource, offset);
      await auth.invoke(caller, setup.resource, "alpha-user");
    },
    { timeout: 35_000 },
  );
  return test.step(
    "Supported consent: reopen connected card and read its actual displayed label",
    async () => {
      test.info().annotations.push({
        type: "auth-stage",
        description: "supported-consent-card:open",
      });
      const flow = await auth
        .client(setup.server.endpoint)
        .beginAuthorization();
      try {
        await auth.page.goto(flow.authorizationUrl);
        // This isolated endpoint has exactly one typed personal offer. Its
        // consent display name need not equal the dashboard credential name.
        const cards = auth.page.locator(
          "li[data-remote-client][data-remote-display]",
        );
        await expect(cards).toHaveCount(1);
        const displayName = await cards.getAttribute("data-remote-display");
        expect(displayName).toBeTruthy();
        const card = cards.filter({ hasText: displayName! });
        await expect(card).toHaveCount(1);
        await expect(card.locator("[data-card-status]")).toContainText(
          "Service connected",
        );
        await test.info().attach("supported-card-labels", {
          body: JSON.stringify({
            credential: setup.credentialName,
            card: displayName,
          }),
          contentType: "application/json",
        });
        return { card, flow, displayName };
      } catch (error) {
        await flow.cancel();
        throw error;
      }
    },
    { timeout: 20_000 },
  );
}

for (const action of ["Refresh", "Verify", "Disconnect"] as const) {
  test(`G4 supported typed card ${action}${action === "Disconnect" ? " and reconnect" : ""} with persisted enforcement and issuer bridge`, async ({
    auth,
  }) => {
    const setup = await supportedConnection(auth);
    const { card, flow, displayName } = await connectedCard(auth, setup);
    try {
      await test.step(
        `G4 supported action: click real card ${action} and verify upstream evidence`,
        async () => {
          test.info().annotations.push({
            type: "auth-stage",
            description: `supported-consent-card:connected:${action}`,
          });
          const offset = auth.mock.events.length;
          const actionButton =
            action === "Refresh"
              ? card.locator('button[data-refresh-link][value="refresh"]')
              : card.getByRole("button", {
                  name:
                    action === "Disconnect"
                      ? `Disconnect ${displayName}`
                      : action,
                  exact: true,
                });
          await expect(actionButton).toBeVisible();
          await actionButton.click();
          if (action === "Refresh") {
            await expect
              .poll(
                () =>
                  auth.mock.events
                    .slice(offset)
                    .filter((event) => event.kind === "refresh").length,
              )
              .toBe(1);
            const refreshed = auth.mock.events
              .slice(offset)
              .find((event) => event.kind === "refresh")!;
            expect(refreshed).toMatchObject({
              provider: "alpha",
              clientId: setup.clientId,
              resource: setup.resource,
              outcome: "success",
            });
            expect([...(refreshed.scopes ?? [])].sort()).toEqual(
              [...scopes].sort(),
            );
            await expect(card.locator("[data-card-status]")).toContainText(
              "Service connected",
            );
          } else if (action === "Verify") {
            await expect
              .poll(
                () =>
                  auth.mock.events
                    .slice(offset)
                    .filter(
                      (event) =>
                        event.kind === "mcp" && event.outcome === "authorized",
                    ).length,
              )
              .toBeGreaterThan(0);
            for (const event of auth.mock.events
              .slice(offset)
              .filter(
                (event) =>
                  event.kind === "mcp" && event.outcome === "authorized",
              )) {
              expect(event).toMatchObject({
                provider: "alpha",
                clientId: setup.clientId,
                resource: setup.resource,
                principal: "alpha-user",
              });
            }
            await expect(
              card.locator('[data-validation="valid"]'),
            ).toBeVisible();
          } else {
            await expect(
              card.getByRole("button", { name: "Connect", exact: true }),
            ).toBeVisible();
            await expect
              .poll(
                () =>
                  auth.mock.events
                    .slice(offset)
                    .filter((event) => event.kind === "revoke").length,
              )
              .toBeGreaterThan(0);
            for (const event of auth.mock.events
              .slice(offset)
              .filter((event) => event.kind === "revoke")) {
              expect(event).toMatchObject({
                provider: "alpha",
                clientId: setup.clientId,
                outcome: "success",
              });
            }
            const reconnectOffset = auth.mock.events.length;
            await card
              .getByRole("button", { name: "Connect", exact: true })
              .click();
            await auth.page
              .getByRole("button", { name: "Approve mock access", exact: true })
              .click();
            await expect(card.locator("[data-card-status]")).toContainText(
              "Service connected",
            );
            assertGrant(auth, setup.clientId, setup.resource, reconnectOffset);
          }
          expect(
            auth.mock.events
              .slice(offset)
              .filter((event) => event.kind === "register"),
          ).toHaveLength(0);
          if (action !== "Disconnect") {
            expect(
              auth.mock.events
                .slice(offset)
                .filter(
                  (event) =>
                    event.kind === "authorize" || event.kind === "token",
                ),
            ).toHaveLength(0);
          }
        },
        { timeout: 35_000 },
      );
    } finally {
      await flow.cancel();
    }
    await test.step(
      "G4 supported post-action: new downstream grant uses unchanged upstream identity",
      async () => {
        const offset = auth.mock.events.length;
        const caller = auth.client(setup.server.endpoint);
        await auth.authorize(caller);
        await auth.invoke(caller, setup.resource, "alpha-user");
        const calls = auth.mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call");
        expect(calls.length).toBeGreaterThan(0);
        for (const event of calls)
          expect(event).toMatchObject({
            clientId: setup.clientId,
            provider: "alpha",
            resource: setup.resource,
            principal: "alpha-user",
            outcome: "authorized",
          });
      },
      { timeout: 35_000 },
    );
  });
}

test("G5 supported typed card reconnect preserves requested scopes and OAuth identity with issuer bridge", async ({
  auth,
}) => {
  const setup = await supportedConnection(auth);
  const { card, flow, displayName } = await connectedCard(auth, setup);
  try {
    await test.step(
      "G5 reconnect: local disconnect permits a fresh exact-scope authorization",
      async () => {
        await card
          .getByRole("button", {
            name: `Disconnect ${displayName}`,
            exact: true,
          })
          .click();
        const connect = card.getByRole("button", {
          name: "Connect",
          exact: true,
        });
        await expect(connect).toBeVisible();
        // Provider revocation is checked separately by G4; its best-effort
        // outcome must not gate this requested-scope reauthorization check.
        const offset = auth.mock.events.length;
        await connect.click();
        await auth.page
          .getByRole("button", { name: "Approve mock access", exact: true })
          .click();
        await expect(card.locator("[data-card-status]")).toContainText(
          "Service connected",
        );
        assertGrant(auth, setup.clientId, setup.resource, offset);
        expect(
          auth.mock.events
            .slice(offset)
            .filter((event) => event.kind === "register"),
        ).toHaveLength(0);
      },
      { timeout: 35_000 },
    );
  } finally {
    await flow.cancel();
  }
  await test.step(
    "G5 post-reconnect: real tool call retains resource and client identity",
    async () => {
      const offset = auth.mock.events.length;
      const caller = auth.client(setup.server.endpoint);
      await auth.authorize(caller);
      await auth.invoke(caller, setup.resource, "alpha-user");
      const calls = auth.mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      expect(calls.length).toBeGreaterThan(0);
      for (const event of calls)
        expect(event).toMatchObject({
          provider: "alpha",
          clientId: setup.clientId,
          resource: setup.resource,
          principal: "alpha-user",
          outcome: "authorized",
        });
    },
    { timeout: 35_000 },
  );
});
