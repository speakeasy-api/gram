import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  collapseToMatchWindows,
  getMatchStrings,
  matchRanges,
  matchShownInDescription,
} from "./chatHelpers";

// Minimal RiskResult factory — only the fields the match-display helpers read
// matter; the rest satisfy the required shape.
function result(
  source: string,
  match: string | undefined,
  extra: Partial<RiskResult> = {},
): RiskResult {
  return {
    id: `${source}:${match ?? ""}`,
    chatMessageId: "m1",
    policyId: "p1",
    policyVersion: 1,
    createdAt: new Date("2026-07-06T00:00:00Z"),
    source,
    match,
    replayed: false,
    ...extra,
  };
}

describe("matchRanges", () => {
  it("finds every occurrence", () => {
    expect(matchRanges("a b a b a", ["a"])).toEqual([
      [0, 1],
      [4, 5],
      [8, 9],
    ]);
  });

  it("merges overlapping/adjacent ranges and prefers longer matches", () => {
    // "secret" and its substring "sec" both match; the union is one range.
    expect(matchRanges("my secret here", ["secret", "sec"])).toEqual([[3, 9]]);
  });

  it("ignores empty match strings and absent matches", () => {
    expect(matchRanges("hello world", ["", "nope"])).toEqual([]);
  });
});

describe("collapseToMatchWindows", () => {
  const long = (fill: string, n: number) => fill.repeat(n);

  it("returns null for short messages", () => {
    expect(
      collapseToMatchWindows("a short flagged msg", ["flagged"]),
    ).toBeNull();
  });

  it("returns null when no match anchors the window", () => {
    const text = long("x", 1000);
    expect(collapseToMatchWindows(text, ["absent"])).toBeNull();
  });

  it("keeps a bounded window around a match buried in a long message", () => {
    const secret = "SECRET_VALUE";
    const text = `${long("x", 800)}${secret}${long("y", 800)}`;
    const snippets = collapseToMatchWindows(text, [secret], 100);
    expect(snippets).not.toBeNull();
    expect(snippets).toHaveLength(1);
    const [snippet] = snippets!;
    expect(snippet!.text).toContain(secret);
    // Window is the match plus ~100 chars of context on each side, not the
    // whole 1600+ char message.
    expect(snippet!.text.length).toBeLessThan(secret.length + 300);
    expect(snippet!.elidedBefore).toBe(true);
    expect(snippet!.elidedAfter).toBe(true);
  });

  it("emits a separate window per distant match", () => {
    const text = `${long("x", 500)}AAA${long("y", 500)}BBB${long("z", 500)}`;
    const snippets = collapseToMatchWindows(text, ["AAA", "BBB"], 100);
    expect(snippets).toHaveLength(2);
    expect(snippets![0]!.text).toContain("AAA");
    expect(snippets![1]!.text).toContain("BBB");
  });

  it("merges windows for nearby matches into one", () => {
    const text = `${long("x", 700)}AAA yyy BBB${long("z", 700)}`;
    const snippets = collapseToMatchWindows(text, ["AAA", "BBB"], 100);
    expect(snippets).toHaveLength(1);
    expect(snippets![0]!.text).toContain("AAA yyy BBB");
  });

  it("does not collapse when the window already covers most of the message", () => {
    // Match sits mid-message but the context window spans nearly all of it.
    const text = `${long("x", 320)}AAA${long("y", 320)}`;
    expect(collapseToMatchWindows(text, ["AAA"], 400)).toBeNull();
  });
});

describe("matchShownInDescription", () => {
  it("is true for account_identity, whose match already appears in the finding description", () => {
    expect(
      matchShownInDescription(result("account_identity", "jane@gmail.com")),
    ).toBe(true);
  });

  it("is false for content findings whose match must be surfaced separately", () => {
    expect(matchShownInDescription(result("gitleaks", "AKIAEXAMPLE"))).toBe(
      false,
    );
    expect(matchShownInDescription(result("presidio", "x"))).toBe(false);
  });
});

describe("getMatchStrings", () => {
  it("returns distinct, non-empty matches, longest first", () => {
    expect(
      getMatchStrings([
        result("gitleaks", "short"),
        result("gitleaks", "a-longer-secret"),
        result("gitleaks", "short"), // duplicate collapses
        result("presidio", ""), // empty ignored
      ]),
    ).toEqual(["a-longer-secret", "short"]);
  });

  it("excludes account_identity matches so the authenticated email is never highlighted or shown as an out-of-text flagged value", () => {
    expect(
      getMatchStrings([result("account_identity", "jane@gmail.com")]),
    ).toEqual([]);
  });

  it("drops account_identity while keeping real content matches in a mixed message", () => {
    expect(
      getMatchStrings([
        result("account_identity", "jane@gmail.com"),
        result("gitleaks", "AKIAEXAMPLE"),
      ]),
    ).toEqual(["AKIAEXAMPLE"]);
  });

  it("returns [] for empty or undefined input", () => {
    expect(getMatchStrings([])).toEqual([]);
    expect(getMatchStrings(undefined)).toEqual([]);
  });
});
