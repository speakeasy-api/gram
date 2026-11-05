import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import type { startEMAUpstream } from "./ema-upstream.ts";

/** Only DOM operations provision application state. The upstream owns its DCR state. */
export async function configureEMA(
  auth: AuthScenario,
  upstream: Awaited<ReturnType<typeof startEMAUpstream>>,
  onChainingEnabled: (serverURL: string) => void,
) {
  const { page, dashboard } = auth;
  const name = `auth-e2e-ema-${randomUUID().slice(0, 8)}`;
  await dashboard.projectRoute("remote-identity-providers");
  await page
    .locator(".gram-elements")
    .getByRole("button", { name: "New Authorization Server", exact: true })
    .click();
  const issuer = page.getByRole("dialog");
  await issuer
    .getByPlaceholder("https://login.example.com", { exact: true })
    .fill(upstream.origin);
  await issuer.getByRole("button", { name: "Discover", exact: true }).click();
  await expect(
    issuer.getByPlaceholder("https://login.example.com/register", {
      exact: true,
    }),
  ).toHaveValue(`${upstream.origin}/register`);
  await issuer
    .getByPlaceholder("my-authorization-server", { exact: true })
    .fill(name);
  await issuer
    .getByPlaceholder("My Authorization Server", { exact: true })
    .fill(name);
  await issuer.getByRole("button", { name: "Create", exact: true }).click();
  await expect(issuer).not.toBeVisible();
  await page.getByRole("tab", { name: "Clients", exact: true }).click();
  await page.getByRole("button", { name: "New Client", exact: true }).click();
  const client = page.getByRole("dialog", { name: "New Client", exact: true });
  await expect(client.getByRole("radio", { name: /^Each user/ })).toBeChecked();
  await client
    .getByPlaceholder("read, write, openid", { exact: true })
    .fill(upstream.scope);
  await client.getByRole("button", { name: "Create", exact: true }).click();
  await expect(client).not.toBeVisible();
  // This issuer was created in this test, so it has exactly one registration.
  const registration = page.getByRole("link", { name: /^View client / });
  await expect(registration).toHaveCount(1);
  const registrationClientId = (await registration.getAttribute(
    "aria-label",
  ))!.slice("View client ".length);
  expect(registrationClientId).toMatch(/^ema-[0-9a-f-]+$/);
  await registration.click();
  await page.getByTitle("Rename client", { exact: true }).click();
  const rename = page.getByRole("textbox", { name: "Name", exact: true });
  await rename.fill(name);
  await rename.press("Enter");
  await expect(rename).not.toBeVisible();
  const server = await dashboard.remote(name, upstream.resource, "user");
  // The discovery-created personal credential already uses the server name.
  // Give this separately registered offer a distinct name so consent selects
  // the intended client rather than either of two identically named offers.
  const personalName = `${name}-personal`;
  await addPersonalOffer(page, personalName, name);
  const section = page.locator("#identity-chaining");
  // Discovery also creates a registration. Pin chaining to the exact client
  // provisioned above rather than the first client for this shared issuer.
  const picker = section.getByRole("combobox");
  await picker.click();
  await page
    .getByRole("option", { name: registrationClientId, exact: true })
    .click();
  await expect(picker).toHaveText(registrationClientId);
  const turnOn = section.getByRole("button", { name: "Turn on", exact: true });
  const grants = section.getByRole("checkbox", {
    name: /^Declare that client /,
  });
  await expect
    .poll(async () => (await turnOn.isEnabled()) || (await grants.isVisible()))
    .toBe(true);
  if (await grants.isVisible()) await grants.check();
  try {
    await expect(turnOn).toBeEnabled();
  } catch (error) {
    // Read only rendered state. Emit fixed product-copy markers, never human
    // profile data, issuer URLs, registration IDs, or the raw section text.
    const text = await section.innerText();
    const markers = [
      "No identity provider can vouch for people on this server.",
      "Needs setup",
      "Needs a manual step",
      "Couldn’t check the client",
      "Couldn’t tell",
      "Temporary failure",
      "Not supported",
      "Unknown",
      "doesn’t support the kind of Enterprise Managed Authorization",
      "An organization admin must confirm its grants first.",
      "This service doesn't accept",
      "No identity providers",
      "Needs sign-in enforcement for the organization",
      "Not working right now",
      "Needs a verified domain",
      "Not set up",
      "People don’t log in to this server with it",
      "Can’t vouch for people who log in with",
      "needs a confidential client",
      "couldn’t",
      "could not",
      "scope",
      "unavailable",
      "not available",
      "grants",
      "accept",
      "vouch",
    ].filter((marker) => text.includes(marker));
    await test.info().attach("ema-enable-state", {
      body: JSON.stringify({
        clientPinned: (await picker.innerText()) === registrationClientId,
        grantConfirmationVisible: await grants.isVisible(),
        grantConfirmationChecked:
          (await grants.count()) === 1 ? await grants.isChecked() : null,
        markers,
      }),
      contentType: "application/json",
    });
    throw error;
  }
  // Arm cleanup before the click: a timeout may occur after the write.
  onChainingEnabled(server.serverURL);
  await turnOn.click();
  await expect(
    section.getByRole("button", { name: "Turn off", exact: true }),
  ).toBeVisible();
  return { ...server, personalName };
}

/** The server refuses to delete a project that still has chaining bindings. */
export async function disableEMA(auth: AuthScenario, serverURL: string) {
  const { page } = auth;
  await page.goto(`${serverURL}/settings`);
  const section = page.locator("#identity-chaining");
  const turnOff = section.getByRole("button", {
    name: "Turn off",
    exact: true,
  });
  const turnOn = section.getByRole("button", { name: "Turn on", exact: true });
  await expect(turnOff.or(turnOn)).toBeVisible();
  if (await turnOff.isVisible()) {
    await turnOff.click();
    await page
      .getByRole("dialog", {
        name: "Turn off Enterprise Managed Authorization?",
        exact: true,
      })
      .getByRole("button", { name: "Turn off", exact: true })
      .click();
  }
  await expect(turnOn).toBeVisible();
}
