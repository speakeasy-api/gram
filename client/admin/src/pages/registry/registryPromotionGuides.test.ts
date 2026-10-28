import { expect, it } from "vitest";

import { slugGuideHint } from "./registryPromotionGuides";

const setupGuides = [
  { slug: "slack", title: "Slack" },
  { slug: "box", title: "Box" },
];

it("matches exact slugs only", () => {
  const candidate = { setupGuides, setupGuidesVersion: "v0.3.9" };
  expect(slugGuideHint(candidate, "box")).toEqual({
    kind: "existing",
    title: "Box",
    url: "https://github.com/speakeasy-api/mcp-setup-docs/tree/main/guides/box",
  });
  expect(slugGuideHint(candidate, "Box")).toEqual({
    kind: "none",
    version: "v0.3.9",
  });
  expect(slugGuideHint(candidate, "box-enterprise")).toEqual({
    kind: "none",
    version: "v0.3.9",
  });
});

it("distinguishes the guide that documents the endpoint", () => {
  const candidate = { setupGuides, endpointGuideSlug: "slack" };
  expect(slugGuideHint(candidate, "slack")?.kind).toBe("endpoint");
  expect(slugGuideHint(candidate, "box")?.kind).toBe("existing");
});

it("gives no hint without a guide list or a slug", () => {
  expect(slugGuideHint({}, "slack")).toBeNull();
  expect(slugGuideHint({ setupGuides }, "")).toBeNull();
});
