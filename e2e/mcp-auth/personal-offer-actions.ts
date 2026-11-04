import { test, expect, type Page } from "@playwright/test";

/** Add an administrator-owned personal offer referencing a UI-created registration. */
export async function addPersonalOffer(
  page: Page,
  name: string,
  registrationClientId: string,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:add-exact-personal-registration",
  });
  await page
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  const sheet = page.getByRole("dialog", {
    name: "Add a credential",
    exact: true,
  });
  const kind = sheet.locator("#add-credential-remote_session");
  await kind.focus();
  await kind.press("Space");
  await expect(kind).toBeChecked();
  await sheet.getByLabel("Name", { exact: true }).fill(name);
  const signIn = sheet.getByRole("combobox", { name: "Sign-in", exact: true });
  await signIn.click();
  // DCR registrations without an administrator display name are rendered as
  // their public client_id. Never select the first provider-looking option.
  const registration = page.getByRole("option", {
    name: registrationClientId,
    exact: true,
  });
  await expect(
    registration,
    "UI_REGISTRATION_BLOCKER: exact recorded DCR registration must be offered",
  ).toHaveCount(1);
  await registration.click();
  await expect(signIn).toHaveText(registrationClientId);
  await sheet
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  await expect(sheet).not.toBeVisible();
  await expect(
    page
      .getByRole("button")
      .filter({ has: page.getByText(name, { exact: true }) }),
  ).toHaveCount(1);
  await expect(
    page.getByRole("switch", { name: `Turn off ${name}`, exact: true }),
  ).toBeChecked();
}
