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

export async function expectAllowed(page: Page, marker: string): Promise<void> {
  await expect(page.getByText(marker, { exact: true }).first()).toBeVisible();
  await expect(page.getByText(ACCESS_DENIED)).toHaveCount(0);
}

export async function expectDenied(page: Page): Promise<void> {
  await expect(page.getByText(ACCESS_DENIED).first()).toBeVisible();
}
