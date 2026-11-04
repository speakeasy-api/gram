import { expect, test } from "@playwright/test";
import { ApiClient } from "../lib/api.ts";
import {
  ACCESS_DENIED,
  expectAllowed,
  expectDenied,
  pageAs,
} from "../lib/browser.ts";
import { credentialFor, lazyWorld } from "../lib/personas.ts";

// Multi-step dashboard journeys where authorization decides what happens next,
// asserted on both what the page shows and what the API answered.

const world = lazyWorld();

test("logging in through the dashboard lands on the organization", async ({
  browser,
}) => {
  // No stored session: the identity provider signs in its default user, the
  // seeded organization admin.
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await context.newPage();
  await page.goto("/login");
  await page.getByRole("button", { name: "Log in" }).click();
  await page.waitForURL((url) => !url.pathname.startsWith("/login"), {
    timeout: 120_000,
  });
  await expect(page.getByRole("button", { name: "Log in" })).toHaveCount(0);
  await expect(page.getByText(ACCESS_DENIED)).toHaveCount(0);
  await context.close();
});

test("a role change takes effect in the dashboard", async ({
  browser,
  request,
}) => {
  const admin = new ApiClient(request, credentialFor(world, "admin"));
  const reader = world.users.reader;
  const readerRole = world.roleIds["rbac-e2e-reader"]!;
  const mcpPage = `/speakeasy/projects/${world.projectSlug}/mcp`;
  const assign = (roleId: string) =>
    admin.ok("PUT", "/rpc/access.updateMemberRoles", {
      project: null,
      body: { user_id: reader.userId, role_ids: [roleId] },
    });

  let page = await pageAs(browser, "reader");
  await page.goto(mcpPage);
  await expectDenied(page);
  await page.context().close();

  try {
    await assign(world.roleIds["member"]!);
    page = await pageAs(browser, "reader");
    await page.goto(mcpPage);
    await expectAllowed(page, "MCP Servers");
    await page.context().close();
  } finally {
    await assign(readerRole);
  }

  page = await pageAs(browser, "reader");
  await page.goto(mcpPage);
  await expectDenied(page);
  await page.context().close();
});

test.describe("a project write block", () => {
  for (const target of ["default", "sandbox"] as const) {
    const blockedHere = target === "default";
    test(`${blockedHere ? "refuses" : "allows"} saving ${target} project settings`, async ({
      browser,
      request,
    }) => {
      const slug =
        target === "default" ? world.projectSlug : world.sandboxProjectSlug;
      const admin = new ApiClient(request, credentialFor(world, "admin"));
      const before = await admin.ok<{ project: { name: string } }>(
        "GET",
        "/rpc/projects.get",
        {
          project: null,
          query: { slug },
        },
      );
      const originalName = before.project.name;

      const page = await pageAs(browser, "blocked");
      try {
        await page.goto(`/speakeasy/projects/${slug}/settings`);
        await expectAllowed(page, "Project Settings");
        const name = page.locator('input[name="name"]');
        await expect(name).toHaveValue(originalName);
        await name.fill(`${originalName} e2e`);

        const save = page.getByRole("button", { name: "Save" });
        if (blockedHere) {
          // The component gate honours the block: hovering Save explains the
          // missing permission, and the button cannot send an update.
          const updates: string[] = [];
          page.on("request", (r) => {
            if (r.url().includes("/rpc/projects.update")) updates.push(r.url());
          });
          await save.hover({ force: true });
          await expect(
            page
              .getByText("You don't have permission to perform this action.")
              .first(),
          ).toBeVisible();
          await save.click({ timeout: 3_000 }).catch(() => {});
          await page.waitForTimeout(1_000);
          expect(updates).toEqual([]);
        } else {
          const update = page.waitForResponse((r) =>
            r.url().includes("/rpc/projects.update"),
          );
          await save.click();
          expect((await update).status()).toBe(200);
        }

        const after = await admin.ok<{ project: { name: string } }>(
          "GET",
          "/rpc/projects.get",
          {
            project: null,
            query: { slug },
          },
        );
        expect(after.project.name).toBe(
          blockedHere ? originalName : `${originalName} e2e`,
        );
      } finally {
        await page.context().close();
        await admin.call("POST", "/rpc/projects.update", {
          project: slug,
          body: { name: originalName },
        });
      }
    });
  }
});

test("an admin creates an API key from the dashboard", async ({
  browser,
  request,
}) => {
  const admin = new ApiClient(request, credentialFor(world, "admin"));
  const name = `rbac-e2e-ui-key-${Date.now().toString(36)}`;
  const page = await pageAs(browser, "admin");
  try {
    await page.goto("/speakeasy/api-keys");
    await expectAllowed(page, "API Keys");
    await page.getByRole("button", { name: /New API Key/i }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("textbox").first().fill(name);
    const created = page.waitForResponse((r) =>
      r.url().includes("/rpc/keys.create"),
    );
    await dialog.getByRole("button", { name: /create/i }).click();
    expect((await created).status()).toBe(200);
  } finally {
    await page.context().close();
    const keys = await admin.ok<{ keys: { id: string; name: string }[] }>(
      "GET",
      "/rpc/keys.list",
      {
        project: null,
      },
    );
    for (const k of keys.keys.filter((k) => k.name === name)) {
      await admin.call("DELETE", "/rpc/keys.revoke", {
        project: null,
        query: { id: k.id },
      });
    }
  }
});
