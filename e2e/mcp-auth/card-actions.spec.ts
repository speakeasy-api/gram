import { test, expect } from "./fixtures.ts";
import { assertGrant, openCard, setupCard } from "./auth-setup-actions.ts";

test.describe.configure({ mode: "default", retries: 0 });
test.setTimeout(180_000);

for (const connection of ["new", "existing"] as const) {
  for (const action of ["Refresh", "Verify", "Disconnect"] as const) {
    test(`G4 enforced: ${connection} consent card ${action} preserves exact OAuth identity${action === "Disconnect" ? " and reconnects after revocation" : ""}`, async ({
      auth,
    }) => {
      const { registration, server, resource } = await setupCard(auth);
      if (connection === "existing")
        await test.step(
          "G4 prerequisite: initial typed grant and authorized tool call",
          async () => {
            await auth.authorize(auth.client(server.endpoint));
            assertGrant(
              auth,
              registration.clientId,
              resource,
              registration.scopes,
            );
          },
          { timeout: 40_000 },
        );
      await test.step(
        `G4 consent: open card and exercise ${action}`,
        async () => {
          const { card, flow } = await openCard(
            auth,
            server.endpoint,
            registration.name,
          );
          try {
            if (connection === "new") {
              const connect = card.getByRole("button", {
                name: "Connect",
                exact: true,
              });
              if (await connect.isVisible()) {
                await connect.click();
                await auth.page
                  .getByRole("button", {
                    name: "Approve mock access",
                    exact: true,
                  })
                  .click();
              }
              assertGrant(
                auth,
                registration.clientId,
                resource,
                registration.scopes,
              );
            }
            await expect(card.locator("[data-card-status]")).toContainText(
              "Service connected",
            );
            const offset = auth.mock.events.length;
            const actionButton =
              action === "Refresh"
                ? card.locator('button[data-refresh-link][value="refresh"]')
                : card.getByRole("button", {
                    name:
                      action === "Disconnect"
                        ? `Disconnect ${registration.name}`
                        : action,
                    exact: true,
                  });
            await expect(actionButton).toBeVisible();
            await actionButton.click();
            if (action === "Disconnect") {
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
                  clientId: registration.clientId,
                  outcome: "success",
                });
              }
              const reconnectOffset = auth.mock.events.length;
              await card
                .getByRole("button", { name: "Connect", exact: true })
                .click();
              await auth.page
                .getByRole("button", {
                  name: "Approve mock access",
                  exact: true,
                })
                .click();
              await expect(card.locator("[data-card-status]")).toContainText(
                "Service connected",
              );
              assertGrant(
                auth,
                registration.clientId,
                resource,
                registration.scopes,
                reconnectOffset,
              );
            } else if (action === "Refresh") {
              await expect
                .poll(
                  () =>
                    auth.mock.events
                      .slice(offset)
                      .filter((event) => event.kind === "refresh").length,
                )
                .toBe(1);
              const refresh = auth.mock.events
                .slice(offset)
                .find((event) => event.kind === "refresh")!;
              expect(refresh).toMatchObject({
                provider: "alpha",
                clientId: registration.clientId,
                resource,
                outcome: "success",
              });
              expect(refresh.scopes).toEqual(registration.scopes);
              await expect(card.locator("[data-card-status]")).toContainText(
                "Service connected",
              );
            } else {
              await expect(
                card.locator('[data-validation="valid"]'),
              ).toBeVisible();
              await expect
                .poll(
                  () =>
                    auth.mock.events
                      .slice(offset)
                      .filter(
                        (event) =>
                          event.kind === "mcp" &&
                          event.outcome === "authorized",
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
                  clientId: registration.clientId,
                  resource,
                  principal: "alpha-user",
                });
              }
            }
            expect(
              auth.mock.events
                .slice(offset)
                .filter((event) => event.kind === "register"),
            ).toHaveLength(0);
          } finally {
            await flow.cancel();
          }
        },
        { timeout: 40_000 },
      );
      await test.step(
        "G4 post-action: fresh downstream authorization still invokes the selected resource",
        async () => {
          const caller = auth.client(server.endpoint);
          await auth.authorize(caller);
          await auth.invoke(caller, resource, "alpha-user");
        },
        { timeout: 40_000 },
      );
    });
  }
}

test("G5 enforced: typed consent card reconnect preserves requested scopes and OAuth identity", async ({
  auth,
}) => {
  const { registration, server, resource } = await setupCard(auth);
  await test.step(
    "G5 prerequisite: obtain exact resource-scoped initial grant",
    async () => {
      const caller = auth.client(server.endpoint);
      await auth.authorize(caller);
      assertGrant(auth, registration.clientId, resource, registration.scopes);
      await auth.invoke(caller, resource, "alpha-user");
    },
    { timeout: 40_000 },
  );
  const { card, flow } = await openCard(
    auth,
    server.endpoint,
    registration.name,
  );
  try {
    await test.step(
      "G5 reconnect: local disconnect permits a fresh exact-scope authorization",
      async () => {
        await card
          .getByRole("button", {
            name: `Disconnect ${registration.name}`,
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
        assertGrant(
          auth,
          registration.clientId,
          resource,
          registration.scopes,
          offset,
        );
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
      const caller = auth.client(server.endpoint);
      await auth.authorize(caller);
      await auth.invoke(caller, resource, "alpha-user");
      const calls = auth.mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      expect(calls.length).toBeGreaterThan(0);
      for (const event of calls)
        expect(event).toMatchObject({
          provider: "alpha",
          clientId: registration.clientId,
          resource: resource,
          principal: "alpha-user",
          outcome: "authorized",
        });
    },
    { timeout: 35_000 },
  );
});
