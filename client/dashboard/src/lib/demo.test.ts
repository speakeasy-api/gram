import { describe, expect, it } from "vitest";
import { DEMO_LANDING_PATH, demoProjectPageHref } from "./demo";

describe("demoProjectPageHref", () => {
  it("routes through /explore-demo with a redirect to the equivalent demo page", () => {
    const href = demoProjectPageHref(
      "/example/projects/my-project/costs",
      "my-project",
    );
    expect(href).toBe(
      "https://ai.speakeasy.com/explore-demo?redirect=%2Facme-demo%2Fprojects%2Fdefault%2Fcosts",
    );
  });

  it("falls back to the demo project landing page when no page path is matched", () => {
    const href = demoProjectPageHref("/example/logs");
    expect(href).toBe(
      `https://ai.speakeasy.com/explore-demo?redirect=${encodeURIComponent(DEMO_LANDING_PATH)}`,
    );
  });

  it.each(["https://ai.speakeasy.com", "https://app.getgram.ai"])(
    "stays on %s so the host-only session is kept",
    (currentOrigin) => {
      const href = demoProjectPageHref(
        "/example/logs",
        undefined,
        currentOrigin,
      );
      expect(href).toBe(
        `/explore-demo?redirect=${encodeURIComponent(DEMO_LANDING_PATH)}`,
      );
    },
  );
});
