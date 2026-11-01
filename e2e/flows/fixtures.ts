import { test as base, expect, type Page } from "@playwright/test";

// Shared by every flow.spec.ts. Login runs once per worker through dev-idp
// (credential-less locally) and the session is reused through storage state.

export const serverURL = mustEnv("GRAM_SERVER_URL");

function mustEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(
      `${name} is not set. Run flows through \`mise run test:flows\`, which loads the worktree's mise env.`,
    );
  }
  return value;
}

// A suffix for data a flow creates, so a rerun never collides with an earlier
// run against the same stack.
export function runId(): string {
  return Date.now().toString(36);
}

export type Project = { orgSlug: string; projectSlug: string };

export const test = base.extend<
  { project: Project },
  { storageStatePath: string }
>({
  storageStatePath: [
    async ({ browser }, use, workerInfo) => {
      const path = `${workerInfo.project.outputDir}/.auth-${workerInfo.workerIndex}.json`;
      // browser.newPage does not inherit the config's `use`, so pass it on.
      const page = await browser.newPage({
        baseURL: workerInfo.project.use.baseURL,
        ignoreHTTPSErrors: true,
      });
      await login(page);
      await page.context().storageState({ path });
      await page.close();
      await use(path);
    },
    { scope: "worker" },
  ],
  storageState: async ({ storageStatePath }, use) => use(storageStatePath),
  // The first project linked from the org home the dashboard lands on.
  // Project pages live at `/<org>/projects/<project>`.
  project: async ({ page }, use) => {
    await page.goto("/");
    const link = page.locator('a[href*="/projects/"]').first();
    const href = (await link.getAttribute("href"))!;
    const [, orgSlug, , projectSlug] = new URL(href, page.url()).pathname.split(
      "/",
    );
    await use({ orgSlug: orgSlug!, projectSlug: projectSlug! });
  },
});

async function login(page: Page) {
  // The redirect to /login happens client-side, so wait for the button rather
  // than reading the URL straight after goto.
  await page.goto("/");
  await page.getByRole("button", { name: "Log in" }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/login"), {
    timeout: 60_000,
  });
  // The callback lands on `/` before the app routes to the org home.
  await page.waitForURL((url) => url.pathname !== "/", { timeout: 60_000 });
}

// Call a management API method as the logged-in user. Flows use this for their
// `Then` checks: the UI saying "done" is not proof, the API is.
export async function rpc<T = unknown>(
  page: Page,
  project: Project,
  method: string,
  init: { query?: Record<string, string>; body?: unknown } = {},
): Promise<T> {
  const url = new URL(`/rpc/${method}`, serverURL);
  for (const [k, v] of Object.entries(init.query ?? {})) {
    url.searchParams.set(k, v);
  }
  const headers = { "gram-project": project.projectSlug };
  const res =
    init.body === undefined
      ? await page.request.get(url.toString(), { headers })
      : await page.request.post(url.toString(), { headers, data: init.body });
  expect(res.status(), `${method} returned ${res.status()}`).toBeLessThan(300);
  return (await res.json()) as T;
}

export { expect };
