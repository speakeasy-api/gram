import { randomUUID } from "node:crypto";
import { expect, type AuthScenario } from "./fixtures.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import type { startEMAUpstream } from "./ema-upstream.ts";

/** Only DOM operations provision application state. The upstream owns its DCR state. */
export async function configureEMA(
  auth: AuthScenario,
  upstream: Awaited<ReturnType<typeof startEMAUpstream>>,
) {
  const { page, dashboard } = auth;
  const name = `auth-e2e-ema-${randomUUID().slice(0, 8)}`;
  await page.goto(`${dashboard.projectURL}/remote-identity-providers`);
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
  await registration.click();
  await page.getByTitle("Rename client", { exact: true }).click();
  const rename = page.getByRole("textbox", { name: "Name", exact: true });
  await rename.fill(name);
  await rename.press("Enter");
  await expect(rename).not.toBeVisible();
  const server = await dashboard.remote(name, upstream.resource, "user");
  await addPersonalOffer(page, name, name);
  const section = page.locator("#identity-chaining");
  await expect(
    section.getByRole("button", { name: "Turn on", exact: true }),
  ).toBeEnabled();
  await section.getByRole("button", { name: "Turn on", exact: true }).click();
  await expect(
    section.getByRole("button", { name: "Turn off", exact: true }),
  ).toBeVisible();
  return server;
}
