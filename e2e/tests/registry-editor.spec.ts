import { expect, test } from "@playwright/test";

// Run against the admin Vite dev server; the fixture mounts the real editor and
// JSON worker, not the sheet's textarea mock. No API or stored records are used.
const adminURL = process.env["GRAM_ADMIN_EDITOR_TEST_URL"];
test.skip(!adminURL, "Set GRAM_ADMIN_EDITOR_TEST_URL to the admin Vite origin");

test("JSON worker never fetches a record's external schema", async ({
  page,
}) => {
  const remoteRequests: string[] = [];
  await page.route("https://registry-schema.invalid/**", async (route) => {
    remoteRequests.push(route.request().url());
    await route.fulfill({ json: { type: "string" } });
  });
  await page.goto(`${adminURL}/test/registry-editor.html`);
  await expect(page.getByRole("button", { name: "Format JSON" })).toBeEnabled();
  // Wait for actual worker diagnostics, not just mounting the editor.
  await expect(page.locator("body")).toHaveAttribute(
    "data-diagnostics",
    /^[1-9]\d*$/,
  );
  expect(remoteRequests).toEqual([]);

  // Prove the route observer can see worker-originated requests. A regression
  // enabling schema requests fails the first assertion, not a mocked fetch spy.
  await page.goto(`${adminURL}/test/registry-editor.html?remote`);
  await expect.poll(() => remoteRequests.length).toBeGreaterThan(0);
});
