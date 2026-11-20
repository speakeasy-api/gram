import { expect, test } from "@playwright/test";

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 700 },
]) {
  test(`long admin pages keep the shell in place at ${viewport.width}px`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    await page.goto("http://127.0.0.1:4179/test/admin-layout.html");
    await expect(page.getByText("Section 30", { exact: true })).toBeAttached();
    const content = page.locator("main .overflow-auto");
    // Native elastic overscroll is OS-dependent and unavailable in headless
    // Chrome. Check the browser-applied boundary as well as actual scrolling.
    await expect(content).toHaveCSS("overscroll-behavior-y", "contain");
    const header = page.locator("main > header");
    const before = await header.boundingBox();
    await page.mouse.move(viewport.width - 50, viewport.height / 2);
    await page.mouse.wheel(0, 10000);
    await expect(
      page.getByText("Section 30", { exact: true }),
    ).toBeInViewport();
    await expect.poll(() => header.boundingBox()).toEqual(before);
    expect(
      await page.evaluate(() => ({
        scroll: document.scrollingElement?.scrollTop,
        overflow: document.documentElement.scrollHeight - window.innerHeight,
      })),
    ).toEqual({ scroll: 0, overflow: 0 });
  });
}
