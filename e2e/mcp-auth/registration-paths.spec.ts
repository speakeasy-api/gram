import { randomUUID } from "node:crypto";
import { test, expect } from "./fixtures.ts";
import {
  assertGrant,
  createOAuthRegistration,
  setupCard,
} from "./auth-setup-actions.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import { ServiceAccountActions } from "./service-account-actions.ts";

test.describe.configure({ mode: "default", retries: 0 });
test.setTimeout(180_000);

test("G18 enforced: typed registration picker labels, ownership eligibility, and creation-to-connection journey", async ({
  auth,
}) => {
  const registration = await createOAuthRegistration(auth);
  const accounts = new ServiceAccountActions(auth);
  const selfName = await test.step(
    "G18 UI: create uniquely named service-account registration",
    () => accounts.registration(),
    { timeout: 40_000 },
  );
  const resource = auth.mock.profiles.oauthAlpha;
  auth.mock.setResourceScopes(resource, registration.scopes);
  const server = await test.step(
    "G18 UI: create target and inspect both typed registration pickers",
    async () => {
      const server = await auth.dashboard.remote(
        `auth-e2e-picker-${randomUUID().slice(0, 8)}`,
        resource,
        "none",
        true,
      );
      await auth.page
        .getByRole("button", { name: "Add credential", exact: true })
        .click();
      const sheet = auth.page.getByRole("dialog", {
        name: "Add a credential",
        exact: true,
      });
      const personal = sheet.locator("#add-credential-remote_session");
      await personal.focus();
      await personal.press("Space");
      await expect(personal).toBeChecked();
      await sheet
        .getByRole("combobox", { name: "Sign-in", exact: true })
        .click();
      // Match the exact registration identity in the option description, not
      // the composite accessible name containing its issuer label.
      const registrationOption = (name: string) => {
        const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        return auth.page.getByRole("option").filter({
          has: auth.page.locator(".text-muted-foreground").filter({
            hasText: new RegExp(`(?:^| · )${escaped}$`),
          }),
        });
      };
      await expect(registrationOption(registration.name)).toHaveCount(1);
      await expect(registrationOption(selfName)).toHaveCount(0);
      await auth.page.keyboard.press("Escape");
      const self = sheet.getByRole("radio", { name: /^Service account/ });
      await self.focus();
      await self.press("Space");
      await expect(self).toBeChecked();
      await sheet
        .getByRole("combobox", { name: "Sign-in", exact: true })
        .click();
      await expect(registrationOption(selfName)).toHaveCount(1);
      await expect(registrationOption(registration.name)).toHaveCount(0);
      await auth.page.keyboard.press("Escape");
      await sheet.getByRole("button", { name: "Cancel", exact: true }).click();
      await expect(sheet).not.toBeVisible();
      await addPersonalOffer(auth.page, registration.name, registration.name);
      return server;
    },
    { timeout: 45_000 },
  );
  await test.step(
    "G18 runtime: newly created personal registration authorizes exact resource and scopes",
    async () => {
      const caller = auth.client(server.endpoint);
      await auth.authorize(caller);
      assertGrant(auth, registration.clientId, resource, registration.scopes);
      await auth.invoke(caller, resource, "alpha-user");
    },
    { timeout: 40_000 },
  );
});

test("G19 enforced: OAuth registration rotation preserves scopes and attachments with a new client identity", async ({
  auth,
}) => {
  const { registration, server, resource } = await setupCard(auth);
  await test.step(
    "G19 prerequisite: old OAuth registration has a working scoped connection",
    async () => {
      const caller = auth.client(server.endpoint);
      await auth.authorize(caller);
      assertGrant(auth, registration.clientId, resource, registration.scopes);
      await auth.invoke(caller, resource, "alpha-user");
    },
    { timeout: 40_000 },
  );
  const replacementId = await test.step(
    "G19 rotation reached: UI Rotate client confirmation re-registers OAuth client",
    async () => {
      await auth.page.goto(registration.url);
      await auth.page
        .getByRole("button", { name: "Rotate client", exact: true })
        .click();
      const dialog = auth.page.getByRole("dialog", {
        name: `Rotate client "${registration.name}"?`,
        exact: true,
      });
      await expect(dialog).toContainText(server.name);
      const offset = auth.mock.events.length;
      await dialog
        .getByRole("button", { name: "Rotate client", exact: true })
        .click();
      await expect(dialog).not.toBeVisible();
      const registrations = auth.mock.events
        .slice(offset)
        .filter((event) => event.kind === "register");
      expect(registrations).toHaveLength(1);
      expect(registrations[0]).toMatchObject({
        provider: "alpha",
        outcome: "success",
      });
      const replacement = registrations[0]!.clientId!;
      expect(replacement).toBeTruthy();
      expect(replacement).not.toBe(registration.clientId);
      await expect(
        auth.page.getByTitle("Rename client", { exact: true }),
      ).toContainText(registration.name);
      return replacement;
    },
    { timeout: 30_000 },
  );
  await test.step(
    "G19 reconnect reached: retained attachment uses replacement identity and original scopes",
    async () => {
      const offset = auth.mock.events.length;
      const caller = auth.client(server.endpoint);
      await auth.authorize(caller);
      assertGrant(auth, replacementId, resource, registration.scopes, offset);
      await auth.invoke(caller, resource, "alpha-user");
      const calls = auth.mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      expect(calls.length).toBeGreaterThan(0);
      for (const event of calls)
        expect(event).toMatchObject({
          provider: "alpha",
          clientId: replacementId,
          resource,
          principal: "alpha-user",
          outcome: "authorized",
        });
    },
    { timeout: 40_000 },
  );
});
