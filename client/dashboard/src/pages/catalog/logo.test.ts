import { describe, expect, it } from "vitest";
import { resolveCatalogLogo } from "./logo";
const server = {
  registrySpecifier: "app.linear/linear",
  iconUrl: "legacy",
  meta: {
    "com.speakeasy.ai/icons": {
      default: { src: "default" },
      dark: { src: "dark" },
    },
  },
};
describe("resolveCatalogLogo", () => {
  it("uses curated variants without legacy inversion", () => {
    expect(resolveCatalogLogo(server, "light")).toEqual({
      src: "default",
      className: "",
    });
    expect(resolveCatalogLogo(server, "dark")).toEqual({
      src: "dark",
      className: "",
    });
  });
  it("falls back from dark to default, then legacy, then placeholder", () => {
    expect(
      resolveCatalogLogo(
        {
          ...server,
          meta: { "com.speakeasy.ai/icons": { default: { src: "default" } } },
        },
        "dark",
      ).src,
    ).toBe("default");
    expect(resolveCatalogLogo({ ...server, meta: {} }, "dark")).toEqual({
      src: "legacy",
      className: "dark:invert",
    });
    expect(
      resolveCatalogLogo({ registrySpecifier: "other" }, "dark").src,
    ).toBeUndefined();
  });
  it("ignores dark-only icons in light mode and malformed metadata", () => {
    expect(
      resolveCatalogLogo(
        {
          ...server,
          meta: { "com.speakeasy.ai/icons": { dark: { src: "dark" } } },
        },
        "light",
      ).src,
    ).toBe("legacy");
    expect(
      resolveCatalogLogo(
        {
          ...server,
          meta: { "com.speakeasy.ai/icons": { default: { src: 42 } } },
        },
        "light",
      ).src,
    ).toBe("legacy");
  });
});
