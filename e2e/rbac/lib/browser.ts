import type { Browser, Page } from "@playwright/test";
import { expect } from "@playwright/test";
import { storageStateFile, type UserPersona } from "./personas.ts";

// A dashboard page signed in as a persona, in a context of its own, from the
// session the global setup stored. Close its context when done.
export async function pageAs(
  browser: Browser,
  persona: UserPersona,
): Promise<Page> {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    storageState: storageStateFile(persona),
  });
  return context.newPage();
}

// The copy every page-level permission gate renders.
export const ACCESS_DENIED = "You don't have permission to view this page.";

// A cold dashboard on a CI runner can take well over the default expect
// timeout to render a route the first time, so gates wait longer.
const RENDER_TIMEOUT = 45_000;

export async function expectAllowed(page: Page, marker: string): Promise<void> {
  await expect(
    page.getByText(marker, { exact: true }).first(),
    `"${marker}" on ${page.url()}`,
  ).toBeVisible({ timeout: RENDER_TIMEOUT });
  await expect(page.getByText(ACCESS_DENIED)).toHaveCount(0);
}

export async function expectDenied(page: Page): Promise<void> {
  await expect(
    page.getByText(ACCESS_DENIED).first(),
    `access denied on ${page.url()}`,
  ).toBeVisible({ timeout: RENDER_TIMEOUT });
}
