import { expect, it } from "vitest";
import { isRelativePath } from "./origin";

it("accepts only paths on the dashboard's origin", () => {
  expect(isRelativePath("/access-hub/claude-tag.svg")).toBe(true);
  expect(isRelativePath("https://evil.example.com/x.svg")).toBe(false);
  expect(isRelativePath("//evil.example.com/x.svg")).toBe(false);
  expect(isRelativePath("/\\evil.example.com/x.svg")).toBe(false);
  expect(isRelativePath("/a\\b.svg")).toBe(false);
  expect(isRelativePath("/\t/evil.example.com/x.svg")).toBe(false);
  expect(isRelativePath("/\n/evil.example.com/x.svg")).toBe(false);
});
