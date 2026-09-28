import { expect, test } from "@playwright/test";

// Built fixture mounts real Monaco and its JSON worker; no API or stored records.
const adminURL = "http://127.0.0.1:4179";

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
