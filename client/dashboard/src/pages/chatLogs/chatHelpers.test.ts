import { describe, expect, it } from "vitest";
import { collapseToMatchWindows, matchRanges } from "./chatHelpers";

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
