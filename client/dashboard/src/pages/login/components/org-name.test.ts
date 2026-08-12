import { describe, expect, it } from "vitest";

import {
  MAX_ORG_NAME_LENGTH,
  normalizeOrgName,
  ORG_NAME_INVALID_MESSAGE,
  ORG_NAME_REQUIRED_MESSAGE,
  ORG_NAME_TOO_LONG_MESSAGE,
  ORG_NAME_TOO_SHORT_MESSAGE,
  validateOrgName,
} from "./org-name";

describe("validateOrgName", () => {
  it.each([
    "Acme Inc",
    "acme-corp_2",
    "Acme, Inc.",
    "Bob's Bakery",
    "Bob\u2019s Bakery",
    "Acme & Sons",
    "Acme!",
    "Acme Inc (US)",
    'Acme "Quality" Goods',
    "Acme/Zenith Holdings",
    "Café Zoë",
    "Grünwald Straße",
    "顶尖科技",
    "Акме",
    "アクメ株式会社",
    "شركة أكمي",
    "Acme 🚀",
    "ab",
    "a-b",
  ])("accepts %j", (input) => {
    expect(validateOrgName(input)).toBeUndefined();
  });

  it.each(["", "   ", "\u00a0\u3000"])("reports %j as required", (input) => {
    expect(validateOrgName(input)).toBe(ORG_NAME_REQUIRED_MESSAGE);
  });

  // Punctuation, symbols and emoji carry no name, and a lone initial is not
  // enough to identify an organization by.
  it.each(["A", "A-", "-----", "___", "- _ -", "€ £ ¥", "🚀🚀"])(
    "rejects %j for having too few letters or numbers",
    (input) => {
      expect(validateOrgName(input)).toBe(ORG_NAME_TOO_SHORT_MESSAGE);
    },
  );

  it.each([
    ["a bidi override", "Acme\u202eInc"],
    ["a zero-width space", "Acme\u200bInc"],
    ["a private-use code point", "Acme\uf8ffInc"],
  ])("rejects %s", (_label, input) => {
    expect(validateOrgName(input)).toBe(ORG_NAME_INVALID_MESSAGE);
  });

  // Both sides allow these: Indic, Arabic and Persian orthography needs them to
  // render correct glyphs, even though they are formatting codes.
  it("accepts a zero-width joiner between letters", () => {
    expect(validateOrgName("अ\u200dब")).toBeUndefined();
  });

  it("accepts a name at the length limit", () => {
    expect(validateOrgName("a".repeat(MAX_ORG_NAME_LENGTH))).toBeUndefined();
  });

  it("rejects a name past the length limit", () => {
    expect(validateOrgName("a".repeat(MAX_ORG_NAME_LENGTH + 1))).toBe(
      ORG_NAME_TOO_LONG_MESSAGE,
    );
  });

  // The server counts characters, so a name outside the Basic Multilingual
  // Plane must not be cut off at half the length by UTF-16 unit counting.
  it("counts code points, not UTF-16 units, against the limit", () => {
    const name = "𝕏".repeat(MAX_ORG_NAME_LENGTH);
    expect(name.length).toBe(MAX_ORG_NAME_LENGTH * 2);
    expect(validateOrgName(name)).toBeUndefined();
    expect(validateOrgName(name + "𝕏")).toBe(ORG_NAME_TOO_LONG_MESSAGE);
  });

  // A non-Latin name spends multiple bytes per character. The server counts
  // runes, so a name this long is accepted on both sides.
  it("accepts a non-Latin name at the length limit", () => {
    expect(validateOrgName("字".repeat(MAX_ORG_NAME_LENGTH))).toBeUndefined();
  });
});

describe("normalizeOrgName", () => {
  it.each([
    ["  Acme Inc  ", "Acme Inc"],
    ["Acme   Inc", "Acme Inc"],
    ["Acme\u00a0Inc", "Acme Inc"],
    ["字节\u3000跳动", "字节 跳动"],
    ["Acme\nInc", "Acme Inc"],
    ["Acme\tInc", "Acme Inc"],
  ])("normalizes %j to %j", (input, want) => {
    expect(normalizeOrgName(input)).toBe(want);
  });
});
