import { describe, expect, it } from "vitest";
import { DEMO_LANDING_PATH, demoProjectPageHref } from "./demo";

describe("demoProjectPageHref", () => {
  it("links to the same page in the demo project", () => {
    expect(
      demoProjectPageHref("/example/projects/my-project/costs", "my-project"),
    ).toBe("https://app.getgram.ai/acme-demo/projects/default/costs");
  });

  it("falls back to the demo project landing page", () => {
    expect(demoProjectPageHref("/example/logs")).toBe(
      `https://app.getgram.ai${DEMO_LANDING_PATH}`,
    );
  });
});
