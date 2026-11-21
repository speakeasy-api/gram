import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";

export const readScopes = ["fixture:read"];

/** Named registrations avoid matching retained organization fixtures. */
export async function createOAuthRegistration(
  auth: AuthScenario,
  scopes = readScopes,
) {
  const { page, dashboard, mock } = auth;
  const name = `auth-e2e-oauth-${randomUUID().slice(0, 8)}`;
  return test.step(
    "UI: create named authorization server and each-user DCR registration",
    async () => {
      await page.goto(`${dashboard.projectURL}/remote-identity-providers`);
      await page
        .locator(".gram-elements")
        .getByRole("button", { name: "New Authorization Server", exact: true })
        .click();
      const issuer = page.getByRole("dialog");
      await issuer
        .getByPlaceholder("https://login.example.com", { exact: true })
        .fill(mock.providers.alpha.issuer);
      await issuer
        .getByRole("button", { name: "Discover", exact: true })
        .click();
      await expect(
        issuer.getByPlaceholder("https://login.example.com/register", {
          exact: true,
        }),
      ).toHaveValue(`${mock.providers.alpha.issuer}/register`);
      await issuer
        .getByPlaceholder("my-authorization-server", { exact: true })
        .fill(name);
      await issuer
        .getByPlaceholder("My Authorization Server", { exact: true })
        .fill(name);
      await issuer.getByRole("button", { name: "Create", exact: true }).click();
      await expect(issuer).not.toBeVisible();
      await page.getByRole("tab", { name: "Clients", exact: true }).click();
      await page
        .getByRole("button", { name: "New Client", exact: true })
        .click();
      const sheet = page.getByRole("dialog", {
        name: "New Client",
        exact: true,
      });
      await expect(
        sheet.getByRole("radio", { name: /^Each user/ }),
      ).toBeChecked();
      await expect(
        sheet.getByRole("radio", { name: /^Service account/ }),
      ).toBeEnabled();
      await expect(
        sheet
          .getByRole("combobox")
          .filter({ hasText: "Dynamic Client Registration (DCR)" }),
      ).toHaveCount(1);
      await sheet
        .getByPlaceholder("read, write, openid", { exact: true })
        .fill(scopes.join(", "));
      const offset = mock.events.length;
      await sheet.getByRole("button", { name: "Create", exact: true }).click();
      await expect(sheet).not.toBeVisible();
      const registrations = mock.events
        .slice(offset)
        .filter((event) => event.kind === "register");
      expect(registrations).toHaveLength(1);
      expect(registrations[0]).toMatchObject({
        provider: "alpha",
        outcome: "success",
      });
      const clientId = registrations[0]!.clientId!;
      expect(clientId).toBeTruthy();
      await page
        .getByRole("link", { name: `View client ${clientId}`, exact: true })
        .click();
      await page.getByTitle("Rename client", { exact: true }).click();
      const rename = page.getByRole("textbox", { name: "Name", exact: true });
      await rename.fill(name);
      await rename.press("Enter");
      await expect(rename).not.toBeVisible();
      await expect(
        page.getByTitle("Rename client", { exact: true }),
      ).toContainText(name);
      return { name, clientId, url: page.url(), scopes };
    },
    { timeout: 45_000 },
  );
}

export async function setupCard(auth: AuthScenario) {
  const registration = await createOAuthRegistration(auth);
  const resource = auth.mock.profiles.oauthAlpha;
  auth.mock.setResourceScopes(resource, readScopes);
  const server = await test.step(
    "UI: attach exact typed personal registration",
    async () => {
      const server = await auth.dashboard.remote(
        `auth-e2e-card-${randomUUID().slice(0, 8)}`,
        resource,
        "none",
        true,
      );
      await addPersonalOffer(auth.page, registration.name, registration.name);
      return server;
    },
    { timeout: 45_000 },
  );
  return { registration, server, resource };
}

export function assertGrant(
  auth: AuthScenario,
  clientId: string,
  resource: string,
  scopes: string[],
  offset = 0,
) {
  const grants = auth.mock.events
    .slice(offset)
    .filter((event) => event.kind === "authorize" || event.kind === "token");
  expect(grants.filter((event) => event.kind === "authorize")).toHaveLength(1);
  expect(grants.filter((event) => event.kind === "token")).toHaveLength(1);
  for (const grant of grants) {
    expect(grant).toMatchObject({
      provider: "alpha",
      clientId,
      resource,
      outcome: "success",
    });
    expect([...grant.scopes!].sort()).toEqual([...scopes].sort());
  }
}

/** Stop on the real consent page, without approving the downstream grant. */
export async function openCard(
  auth: AuthScenario,
  endpoint: string,
  name: string,
) {
  test
    .info()
    .annotations.push({ type: "auth-stage", description: "consent-card:open" });
  const flow = await auth.client(endpoint).beginAuthorization();
  await auth.page.goto(flow.authorizationUrl);
  const card = auth.page
    .locator("li[data-remote-client]")
    .filter({ hasText: name });
  const approve = auth.page.getByRole("button", {
    name: "Approve mock access",
    exact: true,
  });
  await expect
    .poll(
      async () => (await card.count()) === 1 || (await approve.isVisible()),
      {
        message: "CONSENT_CARD_PHASE: card or upstream approval must render",
        timeout: 10_000,
      },
    )
    .toBe(true);
  if (await approve.isVisible()) await approve.click();
  await expect(
    card,
    "CONSENT_CARD_PHASE: expected exact named service connection",
  ).toHaveCount(1);
  return { card, flow };
}
