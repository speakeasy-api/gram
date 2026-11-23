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

  it.each(["https://ai.speakeasy.com", "https://app.getgram.ai"])(
    "stays on %s, a host of the demo's deployment, so the host-only session is kept",
    (origin) => {
      const href = demoProjectPageHref("/example/logs", undefined, origin, {
        firstPartyHosts: ["app.getgram.ai", "ai.speakeasy.com"],
      });
      expect(href).toBe(
        `/explore-demo?redirect=${encodeURIComponent(DEMO_LANDING_PATH)}`,
      );
    },
  );

  it.each<[string, string, [string, ...string[]]]>([
    [
      "dev",
      "https://dev.getgram.ai",
      ["dev.getgram.ai", "dev.ai.speakeasy.com"],
    ],
    ["another partition", "https://ai.eu.example.com", ["ai.eu.example.com"]],
  ])("links %s to the demo's deployment", (_name, origin, hosts) => {
    const href = demoProjectPageHref("/example/logs", undefined, origin, {
      firstPartyHosts: hosts,
    });
    expect(href).toBe(
      `https://app.getgram.ai/explore-demo?redirect=${encodeURIComponent(DEMO_LANDING_PATH)}`,
    );
  });
});
