import { expect, it } from "vitest";
import {
  formatRegistryJson,
  registryIssueOffsets,
  serverValidationIssues,
} from "./registryJsonEditor";

it("formats whitespace only, preserving opaque tokens and duplicate keys", () => {
  const raw =
    '{"n":9007199254740993,"n":1.2300,"e":1E+99,"s":"\\u0061","unknown":{"x":-0}}';
  const formatted = formatRegistryJson(raw)!;
  expect(formatted).toContain('\n  "n": 9007199254740993,');
  expect(formatted.replace(/\s/g, "")).toBe(raw);
});
it.each(["{", '{"a":1,}', "{/* comment */}", "null false"])(
  "preserves invalid paste %s",
  (raw) => {
    expect(formatRegistryJson(raw)).toBeNull();
  },
);
it("maps escaped pointer keys, arrays, missing properties and root", () => {
  const raw = '{"a/b":{"~key":[12,{}]}}';
  const issues = [
    { path: "/a~1b/~0key/0", message: "type" },
    { path: "/a~1b/~0key/1/missing", message: "required" },
    { path: "", message: "root" },
  ];
  const offsets = registryIssueOffsets(raw, issues);
  expect(offsets.map((r) => raw.slice(r.offset, r.offset + r.length))).toEqual([
    "12",
    "{}",
    raw,
  ]);
});
it("keeps unknown error text and newlines; adapts the established semicolon format", () => {
  expect(
    serverValidationIssues(
      "/server/name: required; /server/remotes/0/url: invalid\nformat",
    ),
  ).toEqual([
    { path: "/server/name", message: "required" },
    { path: "/server/remotes/0/url", message: "invalid\nformat" },
  ]);
  expect(serverValidationIssues("generic failure")).toEqual([
    { path: "", message: "generic failure" },
  ]);
});
