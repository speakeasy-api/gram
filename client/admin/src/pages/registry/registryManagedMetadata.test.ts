import { expect, it } from "vitest";
import {
  splitManagedMetadata,
  mergeManagedMetadata,
  managedMetadataIssues,
} from "./registryManagedMetadata";
const record =
  '{"server":{"name":"example.test/demo","icons":[{"src":"upstream"}]},"unknown":9007199254740993,"_meta":{"com.speakeasy.ai/icons":{"default":{"src":"logo","mimeType":"image/svg+xml","sizes":["any"]},"future":9007199254740995},"com.speakeasy.ai/registry":{"published":false,"future":9007199254740997},"com.speakeasy.ai/catalog":{"name":"editable"},"com.speakeasy.ai/okta":{"oinNames":[]},"upstream":{"n":9007199254740999}}}';
it("hides only managed namespaces and merges their exact unknown values", () => {
  const { text, managed } = splitManagedMetadata(record);
  expect(text).not.toContain("com.speakeasy.ai/icons");
  expect(text).not.toContain("com.speakeasy.ai/registry");
  for (const value of [
    "com.speakeasy.ai/catalog",
    "com.speakeasy.ai/okta",
    "upstream",
    "9007199254740993",
    "9007199254740999",
  ])
    expect(text).toContain(value);
  const merged = mergeManagedMetadata(text, managed);
  for (const value of [
    "9007199254740993",
    "9007199254740995",
    "9007199254740997",
    "9007199254740999",
  ])
    expect(merged).toContain(value);
});
it("retains managed slots when editable _meta is removed", () => {
  const { managed } = splitManagedMetadata(record);
  const merged = mergeManagedMetadata(
    '{"server":{},"future":9007199254740993}',
    managed,
  );
  expect(merged).toContain("com.speakeasy.ai/icons");
  expect(merged).toContain("com.speakeasy.ai/registry");
  expect(merged).toContain("9007199254740993");
  expect(merged).not.toContain("com.speakeasy.ai/catalog");
});
it.each(["com.speakeasy.ai/icons", "com.speakeasy.ai/registry"])(
  "rejects reintroduced %s rather than dropping edits",
  (key) => {
    const text = `{"_meta":{"${key}":null}}`;
    expect(managedMetadataIssues(text)[0]?.message).toContain("Remove");
    expect(() => mergeManagedMetadata(text, "{}")).toThrow();
  },
);
it("leaves new records without managed metadata unchanged", () => {
  const text = '{"server":{},"unknown":9007199254740993}';
  expect(splitManagedMetadata(text)).toEqual({ text, managed: "{}" });
  expect(mergeManagedMetadata(text, "{}")).toBe(text);
});

it("rejects ambiguous duplicate _meta objects so merging cannot hide managed slots", () => {
  const text = '{"_meta":{},"_meta":{}}';
  expect(managedMetadataIssues(text)[0]?.message).toContain("duplicate");
  expect(() =>
    mergeManagedMetadata(text, splitManagedMetadata(record).managed),
  ).toThrow();
});

it("rejects a non-object _meta instead of replacing editable content", () => {
  expect(managedMetadataIssues('{"_meta":null}')[0]?.message).toContain(
    "Use an object",
  );
});
