import { test as base, type Browser, type Page } from "@playwright/test";
import path from "node:path";

import { ensureRunDir } from "./run.ts";

// One login per worker. Dev-idp is credential-less: a click on Log in
// provisions a user and lands on a project. The org and project slugs are
// read back from that URL rather than assumed, so the suite works in any
// seeded worktree. The browser state is saved once and every test in the
// worker starts from it.

export type Session = {
  orgSlug: string;
  projectSlug: string;
  storageState: string;
};

export async function login(
  page: Page,
): Promise<{ orgSlug: string; projectSlug: string }> {
  await page.goto("/");
  const button = page.getByRole("button", { name: "Log in" });
  if (await button.isVisible({ timeout: 10_000 }).catch(() => false)) {
    await button.click();
  }
  await page.waitForURL(/\/[^/]+\/projects\/[^/]+/, { timeout: 2 * 60_000 });
  const match = new URL(page.url()).pathname.match(
    /^\/([^/]+)\/projects\/([^/]+)/,
  );
  if (!match)
    throw new Error(`landed on ${page.url()}, which is not a project route`);
  return { orgSlug: match[1]!, projectSlug: match[2]! };
}

export const test = base.extend<Record<never, never>, { session: Session }>({
  session: [
    async ({ browser }: { browser: Browser }, use, workerInfo) => {
      const context = await browser.newContext({
        baseURL: workerInfo.project.use.baseURL,
        ignoreHTTPSErrors: true,
      });
      const page = await context.newPage();
      const slugs = await login(page);
      const storageState = path.join(
        ensureRunDir(),
        `state-w${workerInfo.workerIndex}.json`,
      );
      await context.storageState({ path: storageState });
      await context.close();
      await use({ ...slugs, storageState });
    },
    { scope: "worker" },
  ],
  storageState: async ({ session }, use) => {
    await use(session.storageState);
  },
});

export { expect } from "@playwright/test";
