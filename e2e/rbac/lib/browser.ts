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

const RENDER_TIMEOUT = 20_000;

// Runs a gate assertion and, when it fails, says where the page ended up and
// what it showed, since a gate that renders something unexpected is the
// failure worth reading.
async function withPageContext(
  page: Page,
  assertion: () => Promise<void>,
): Promise<void> {
  try {
    await assertion();
  } catch (err) {
    const text = await page
      .locator("body")
      .innerText({ timeout: 2_000 })
      .catch(() => "<no body>");
    throw new Error(
      `${(err as Error).message}\n\nPage: ${page.url()}\nShowing: ${text.replace(/\s+/g, " ").slice(0, 400)}`,
    );
  }
}

export async function expectAllowed(page: Page, marker: string): Promise<void> {
  await withPageContext(page, async () => {
    await expect(page.getByText(marker, { exact: true }).first()).toBeVisible({
      timeout: RENDER_TIMEOUT,
    });
    await expect(page.getByText(ACCESS_DENIED)).toHaveCount(0);
  });
}

export async function expectDenied(page: Page): Promise<void> {
  await withPageContext(page, async () => {
    await expect(page.getByText(ACCESS_DENIED).first()).toBeVisible({
      timeout: RENDER_TIMEOUT,
    });
  });
}
