import { expect, test } from "@playwright/test";

const fixture = "http://127.0.0.1:4179/test/trial-calendar.html";
const calendarChunk = "**/assets/calendar-*.js";

test("loads the real calendar only on demand and preserves keyboard focus", async ({
  page,
}) => {
  const download = Promise.withResolvers<void>();
  let requests = 0;
  await page.route(calendarChunk, async (route) => {
    requests++;
    await download.promise;
    await route.continue();
  });
  await page.goto(fixture);
  await page.getByRole("button", { name: "Change end date" }).click();
  const field = page.getByLabel("Ends on", { exact: true });
  await expect(field).toBeVisible();
  expect(requests).toBe(0);
  await field.press("Enter");
  await expect(page.getByRole("status")).toHaveText("Loading calendar…");
  expect(requests).toBe(1);
  await expect(page.getByRole("grid")).toHaveCount(0);
  download.resolve();
  await expect(page.getByRole("grid")).toBeVisible();
  const selected = page.locator("td[data-selected='true'] button");
  await expect(selected).toBeFocused();
  await selected.press("ArrowRight");
  await expect(selected).not.toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("grid")).toHaveCount(0);
  await expect(field).toBeFocused();
  await field.press("Enter");
  await expect(page.getByRole("grid")).toBeVisible();
  expect(requests).toBe(1);
  await page.keyboard.press("Escape");
  await expect(field).toBeFocused();
  await expect(
    page.getByRole("dialog", { name: "Change trial end date" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Date saved");
  await expect(
    page.getByRole("button", { name: "Change end date" }),
  ).toBeFocused();
});

test("keeps a failed calendar download inside the dialog and recovers after reload", async ({
  page,
}) => {
  await page.route(calendarChunk, (route) => route.abort());
  await page.goto(fixture);
  await page.getByRole("button", { name: "Change end date" }).click();
  await page.getByLabel("Ends on", { exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText(
    "Could not load the calendar.",
  );
  await expect(
    page.getByRole("dialog", { name: "Change trial end date" }),
  ).toBeVisible();
  await page.unroute(calendarChunk);
  await page.getByRole("button", { name: "Reload page" }).click();
  await page.getByRole("button", { name: "Change end date" }).click();
  await page.getByLabel("Ends on", { exact: true }).click();
  await expect(page.getByRole("grid")).toBeVisible();
});
