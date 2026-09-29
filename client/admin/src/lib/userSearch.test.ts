// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  parseUserSearch,
  serializeUserSearch,
  type UserSearchTerm,
} from "./userSearch";

type Case = {
  query: string;
  terms?: Pick<UserSearchTerm, "field" | "value">[];
  error?: string;
};
const cases: Case[] = JSON.parse(
  readFileSync(
    new URL(
      "../../../../server/internal/admin/testdata/user_search.json",
      import.meta.url,
    ),
    "utf8",
  ),
);
const withoutOffsets = (terms: UserSearchTerm[]) =>
  terms.map(({ field, value }) => ({ field, value }));

describe("shared search conformance", () => {
  for (const [index, test] of cases.entries()) {
    it(`case ${index}: ${test.query.slice(0, 40)}`, () => {
      const parsed = parseUserSearch(test.query);
      if (test.error) {
        expect(parsed.ok).toBe(false);
        if (!parsed.ok) {
          expect(parsed.message).toBe(test.error);
          expect(parsed.start).toBeGreaterThanOrEqual(0);
          expect(parsed.end).toBeGreaterThanOrEqual(parsed.start);
          expect(parsed.end).toBeLessThanOrEqual(test.query.length);
        }
      } else {
        expect(parsed.ok).toBe(true);
        if (parsed.ok) {
          expect(withoutOffsets(parsed.terms)).toEqual(test.terms);
          const serialized = serializeUserSearch(parsed.terms);
          expect(
            new TextEncoder().encode(serialized).length,
          ).toBeLessThanOrEqual(new TextEncoder().encode(test.query).length);
          const roundTrip = parseUserSearch(serialized);
          expect(roundTrip.ok).toBe(true);
          if (roundTrip.ok) {
            expect(withoutOffsets(roundTrip.terms)).toEqual(test.terms);
            expect(serializeUserSearch(roundTrip.terms)).toBe(serialized);
          }
        }
      }
    });
  }
});

it("retains UTF-16 source spans, including prefixes and quotes", () => {
  expect(parseUserSearch('  😀 name:"Zoë 😀"\torg:東京 ')).toEqual({
    ok: true,
    terms: [
      { field: "any", value: "😀", start: 2, end: 4 },
      { field: "name", value: "Zoë 😀", start: 5, end: 18 },
      { field: "org", value: "東京", start: 19, end: 25 },
    ],
  });
  expect(parseUserSearch('😀 org:"x"tail')).toEqual({
    ok: false,
    message: "separate search terms with whitespace; quote the whole value",
    start: 3,
    end: 14,
  });
});

it("serializes canonically without losing literal syntax", () => {
  expect(
    serializeUserSearch([
      { field: "any", value: "team:foo" },
      { field: "name", value: "Alex" },
      { field: "org", value: "Example Studio" },
      { field: "any", value: 'a"b\\c' },
      { field: "any", value: "OR" },
      { field: "any", value: "-a" },
    ]),
  ).toBe('"team:foo" name:Alex org:"Example Studio" "a\\"b\\c" "OR" "-a"');
});

it("keeps the measured backslash-heavy query at 1707 bytes", () => {
  const query = Array(7)
    .fill('"' + "\\a".repeat(120) + ' "')
    .join(" ");
  expect(new TextEncoder().encode(query).length).toBe(1707);
  const parsed = parseUserSearch(query);
  expect(parsed.ok).toBe(true);
  if (parsed.ok) expect(serializeUserSearch(parsed.terms)).toBe(query);
});

it.each([1, 2, 3, 4])(
  "minimally escapes a run of %i backslashes in quotes",
  (count) => {
    for (const suffix of ["a", '"', ""]) {
      const value = "space " + "\\".repeat(count) + suffix;
      const encodedRun = "\\".repeat(
        suffix === "a" ? count * 2 - 1 : count * 2,
      );
      const encodedSuffix = suffix === '"' ? '\\"' : suffix;
      expect(serializeUserSearch([{ field: "any", value }])).toBe(
        '"space ' + encodedRun + encodedSuffix + '"',
      );
    }
  },
);
