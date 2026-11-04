import { test, expect, type Page } from "@playwright/test";

export async function addStaticOffer(
  page: Page,
  name: string,
  value: string,
  shareAcrossProjects = false,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:add-static-offer",
  });
  await page
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  const sheet = page.getByRole("dialog");
  const kind = sheet.getByRole("radio", { name: /Static credential/ });
  await kind.focus();
  await kind.press("Space");
  await expect(kind).toBeChecked();
  const share = sheet.getByRole("checkbox", {
    name: "Let servers in other projects use it too",
    exact: true,
  });
  await share.setChecked(shareAcrossProjects);
  await expect(share).toBeChecked({ checked: shareAcrossProjects });
  await sheet.getByLabel("Name", { exact: true }).fill(name);
  await sheet.getByLabel("Header name", { exact: true }).fill("X-Fixture-Key");
  await sheet
    .getByLabel("Value for X-Fixture-Key", { exact: true })
    .fill(value);
  await sheet
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  await expect(sheet).not.toBeVisible();
  await expect(credential(page, name)).toBeVisible();
}

function credential(page: Page, name: string) {
  return page
    .getByRole("button")
    .filter({ has: page.getByText(name, { exact: true }) });
}

export async function disableCredential(
  page: Page,
  name: string,
  shared = false,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:disable-shared-client",
  });
  await page
    .getByRole("switch", { name: `Turn off ${name}`, exact: true })
    .click();
  if (shared) {
    const confirmation = page.getByRole("dialog", {
      name: `Turn off ${name}?`,
      exact: true,
    });
    await expect(confirmation).toContainText("on any server that uses it");
    await confirmation
      .getByRole("button", { name: "Turn off", exact: true })
      .click();
    await expect(confirmation).not.toBeVisible();
  }
  await expect(
    page.getByRole("switch", { name: `Turn on ${name}`, exact: true }),
  ).not.toBeChecked();
}

export async function detachCredential(page: Page, name: string) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:detach-client",
  });
  await credential(page, name).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Remove from this server", exact: true })
    .click();
  const confirmation = page.getByRole("dialog", {
    name: `Remove ${name} from this server?`,
    exact: true,
  });
  await confirmation
    .getByRole("button", { name: "Remove", exact: true })
    .click();
  await expect(confirmation).not.toBeVisible();
  await expect(credential(page, name)).toHaveCount(0);
}

export async function rotateStaticCredential(
  page: Page,
  name: string,
  value: string,
  shared = false,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:rotate-static-material",
  });
  await credential(page, name).click();
  const sheet = page.getByRole("dialog", { name, exact: true });
  const headers = sheet.locator("section").filter({
    has: page.getByRole("heading", {
      name: "Headers sent upstream",
      exact: true,
    }),
  });
  await headers
    .getByLabel("Value for X-Fixture-Key", { exact: true })
    .fill(value);
  await headers.getByRole("button", { name: "Save", exact: true }).click();
  if (shared) {
    const confirmation = page.getByRole("dialog", {
      name: `Change what ${name} sends on every server?`,
      exact: true,
    });
    await expect(confirmation).toContainText(
      "change applies wherever it is used",
    );
    await confirmation
      .getByRole("button", { name: "Save on every server", exact: true })
      .click();
    await expect(confirmation).not.toBeVisible();
  }
  await expect(
    headers.getByRole("button", { name: "Save", exact: true }),
  ).toHaveCount(0);
  await sheet.getByRole("button", { name: "Close", exact: true }).click();
  await expect(sheet).not.toBeVisible();
}

export async function addCallerOffer(
  page: Page,
  name: string,
  shareAcrossProjects = false,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:add-caller-offer",
  });
  await page
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  const sheet = page.getByRole("dialog");
  const kind = sheet.getByRole("radio", { name: /Caller-supplied header/ });
  await kind.focus();
  await kind.press("Space");
  await expect(kind).toBeChecked();
  const share = sheet.getByRole("checkbox", {
    name: "Let servers in other projects use it too",
    exact: true,
  });
  await share.setChecked(shareAcrossProjects);
  await expect(share).toBeChecked({ checked: shareAcrossProjects });
  await sheet.getByLabel("Name", { exact: true }).fill(name);
  await sheet.getByLabel("Header name", { exact: true }).fill("X-Fixture-User");
  await sheet
    .getByLabel("Caller header for X-Fixture-User", { exact: true })
    .fill("X-Client-Fixture");
  await sheet
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  await expect(sheet).not.toBeVisible();
  await expect(credential(page, name)).toBeVisible();
}
