import { expect, it } from "vitest";
import {
  readCatalogIcons,
  setCatalogIcon,
  type CatalogIcon,
} from "./registryIcons";
const icon: CatalogIcon = {
  src: "https://assets.example/logo.svg",
  mimeType: "image/svg+xml",
  sizes: ["any"],
};
it("adds, replaces and removes individual slots, cleaning an empty namespace", () => {
  const raw =
    '{"server":{"icons":[{"src":"upstream"}]},"_meta":{"keep":9007199254740993},"n":1.2300}';
  const primary = setCatalogIcon(raw, "default", icon);
  const both = setCatalogIcon(primary, "dark", icon);
  expect(readCatalogIcons(both)).toEqual({ default: icon, dark: icon });
  expect(
    readCatalogIcons(
      setCatalogIcon(both, "default", {
        ...icon,
        src: "https://assets.example/new.svg",
      }),
    ).default?.src,
  ).toContain("new.svg");
  const dark = setCatalogIcon(both, "default", null);
  expect(readCatalogIcons(dark)).toEqual({ dark: icon });
  const empty = setCatalogIcon(dark, "dark", null);
  expect(empty).not.toContain("com.speakeasy.ai/icons");
  expect(empty).toContain("9007199254740993");
  expect(empty).toContain("1.2300");
  expect(JSON.parse(empty).server.icons).toEqual([{ src: "upstream" }]);
  expect(setCatalogIcon(raw, "dark", null)).toBe(raw);
});
it.each([
  "{",
  '{"server":{},}',
  "[]",
  '{"_meta":null}',
  '{"_meta":{},"_meta":{}}',
  '{"_meta":{"com.speakeasy.ai/icons":[]}}',
])("rejects unsafe draft %s", (text) => {
  expect(() => setCatalogIcon(text, "default", icon)).toThrow(/Fix the JSON/);
});
it("reads empty and malformed documents without inventing icons", () => {
  expect(readCatalogIcons("{}")).toEqual({});
  expect(readCatalogIcons("{")).toEqual({});
});
