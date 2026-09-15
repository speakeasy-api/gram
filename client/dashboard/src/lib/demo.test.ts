import { describe, expect, it } from "vitest";
import { DEMO_LANDING_PATH, demoProjectPageHref } from "./demo";

describe("demoProjectPageHref", () => {
  it("routes through /explore-demo with a redirect to the equivalent demo page", () => {
    const href = demoProjectPageHref(
      "/example/projects/my-project/costs",
      "my-project",
    );
    expect(href).toBe(
      "https://app.getgram.ai/explore-demo?redirect=%2Facme-demo%2Fprojects%2Fdefault%2Fcosts",
    );
  });

  it("falls back to the demo project landing page when no page path is matched", () => {
    const href = demoProjectPageHref("/example/logs");
    expect(href).toBe(
      `https://app.getgram.ai/explore-demo?redirect=${encodeURIComponent(DEMO_LANDING_PATH)}`,
    );
  });
});
