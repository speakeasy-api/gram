// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  MIN_FUZZY,
  STOP_WORDS,
  fuzzyScore,
  prefilter,
  rank,
  tokens,
} from "./ranker";
import type { LauncherCandidate } from "./candidates/types";

function candidate(
  title: string,
  overrides: Partial<LauncherCandidate> = {},
): LauncherCandidate {
  return {
    id: `id:${title}`,
    kind: "page",
    title,
    detail: "Page",
    keywords: [],
    verbs: ["open"],
    group: "Pages",
    run: () => undefined,
    ...overrides,
  };
}

function ids(rows: Array<{ candidate: LauncherCandidate }>): string[] {
  return rows.map((row) => row.candidate.id);
}

function titleBonus(title: string): number {
  return 0.02 * Math.max(0, 1 - title.length / 40);
}

describe("tokens", () => {
  it("lowercases, splits on non-alphanumerics and drops empties", () => {
    expect(tokens("  Slack-MCP (disabled)! ")).toEqual([
      "slack",
      "mcp",
      "disabled",
    ]);
    expect(tokens("")).toEqual([]);
  });

  it("keeps non-ASCII letters as tokens and folds accents", () => {
    expect(tokens("Тестовый помощник")).toEqual(["тестовый", "помощник"]);
    expect(tokens("通知 サーバー")).toEqual(["通知", "サーバー"]);
    expect(tokens("Zoë's Café")).toEqual(["zoe", "s", "cafe"]);
  });

  it("folds a decomposed Latin accent the same as a composed one", () => {
    expect(tokens("Zoe\u0308's Cafe\u0301")).toEqual(["zoe", "s", "cafe"]);
  });

  // Vowel signs in Devanagari are combining marks with no composed form, so
  // they must count as word characters or every word splits at each one.
  it("keeps combining marks on non-Latin scripts inside their word", () => {
    expect(tokens("हिन्दी सर्वर")).toEqual(["हिन्दी", "सर्वर"]);
    expect(tokens("हिन्दी".normalize("NFD"))).toEqual(["हिन्दी"]);
  });
});

describe("STOP_WORDS", () => {
  it("includes the launcher fillers plus dashboard fillers, but not mcp", () => {
    for (const word of ["open", "the", "server", "turn", "set", "switch"]) {
      expect(STOP_WORDS.has(word)).toBe(true);
    }
    expect(STOP_WORDS.has("mcp")).toBe(false);
  });
});

describe("fuzzyScore", () => {
  const slackMcp = candidate("Slack MCP");

  it("strips stop words so 'open the slack mcp' keeps slack and mcp", () => {
    const stripped = fuzzyScore("open the slack mcp", slackMcp);
    expect(stripped).toBeCloseTo(fuzzyScore("slack mcp", slackMcp), 10);
    // Both tokens match exactly; nothing is left unmatched.
    expect(stripped).toBeCloseTo(1 + titleBonus("Slack MCP"), 10);
  });

  it("falls back to every token when only stop words were typed", () => {
    expect(fuzzyScore("open", candidate("Open Requests"))).toBeGreaterThan(0.9);
  });

  it("returns 0 for an empty query", () => {
    expect(fuzzyScore("", slackMcp)).toBe(0);
    expect(fuzzyScore("   ", slackMcp)).toBe(0);
  });

  it("matches titles written in non-Latin scripts", () => {
    const cyrillic = candidate("Тестовый помощник", {
      keywords: ["assistant", "id-1"],
    });
    expect(fuzzyScore("тест", cyrillic)).toBeGreaterThan(0.8);
    expect(fuzzyScore("помощник", cyrillic)).toBeGreaterThan(0.9);
    expect(fuzzyScore("通知", candidate("通知 サーバー"))).toBeGreaterThan(0.9);
    // Accents fold on both sides, so "zoe" and "zoë" both find "Zoë".
    expect(fuzzyScore("zoe", candidate("Zoë"))).toBeGreaterThan(0.9);
    expect(fuzzyScore("zoë", candidate("Zoe"))).toBeGreaterThan(0.9);
  });

  it("does not treat mcp as filler", () => {
    expect(fuzzyScore("mcp", slackMcp)).toBeGreaterThan(0.9);
    expect(fuzzyScore("mcp", candidate("Slack Plugin"))).toBe(0);
  });

  it("orders exact > prefix > contains > subsequence", () => {
    const exact = fuzzyScore("slack", candidate("Slack"));
    const prefix = fuzzyScore("slack", candidate("Slackbot"));
    const contains = fuzzyScore("slack", candidate("Myslackthing"));
    const subsequence = fuzzyScore(
      "slack",
      candidate("Sales Lookup Access Key"),
    );
    expect(exact).toBeGreaterThan(prefix);
    expect(prefix).toBeGreaterThan(contains);
    expect(contains).toBeGreaterThan(subsequence);
    expect(subsequence).toBeGreaterThan(0);
    expect(fuzzyScore("slack", candidate("Jira"))).toBe(0);
  });

  it("matches title initials for tokens of two or more characters", () => {
    expect(fuzzyScore("sm", slackMcp)).toBeCloseTo(
      0.7 + titleBonus("Slack MCP"),
      10,
    );
  });

  it("matches keywords as terms", () => {
    expect(
      fuzzyScore("slk", candidate("Slack MCP", { keywords: ["slk"] })),
    ).toBeCloseTo(1 + titleBonus("Slack MCP"), 10);
  });

  it("halves the score when a token is unmatched", () => {
    const matched = fuzzyScore("slack", candidate("Slack"));
    const withUnmatched = fuzzyScore("slack zzzz", candidate("Slack"));
    expect(matched).toBeCloseTo(1 + titleBonus("Slack"), 10);
    // mean(1, 0) = 0.5, halved = 0.25, plus the title bonus.
    expect(withUnmatched).toBeCloseTo(0.25 + titleBonus("Slack"), 10);
  });

  it("prefers shorter titles when the match is otherwise equal", () => {
    const short = candidate("Slack");
    const long = candidate("Alpha Slack Long Title Name Here");
    expect(fuzzyScore("slack", short)).toBeGreaterThan(
      fuzzyScore("slack", long),
    );
    expect(ids(prefilter("slack", [long, short]).all)).toEqual([
      short.id,
      long.id,
    ]);
  });
});

describe("prefilter", () => {
  it("exposes the default threshold", () => {
    expect(MIN_FUZZY).toBe(0.15);
  });

  it("keeps every matching candidate in fuzzy order", () => {
    const server = candidate("Slack MCP", { kind: "mcp_server" });
    const person = candidate("Alice Slack", { kind: "person" });
    const unrelated = candidate("Jira");
    expect(ids(prefilter("slack", [person, server, unrelated]).all)).toEqual([
      server.id,
      person.id,
    ]);
  });

  it("honours a custom minimum fuzzy score", () => {
    const candidates = [
      candidate("Slack"),
      candidate("Slackbot"),
      candidate("Sales Lookup Access Key"),
    ];
    expect(prefilter("slack", candidates, { minFuzzy: 0.5 }).all).toHaveLength(
      2,
    );
  });

  it("returns nothing for an empty query", () => {
    expect(prefilter("", [candidate("Slack")]).all).toEqual([]);
  });
});

describe("rank", () => {
  it("keeps fuzzy order and opens every result", () => {
    const rows = rank(
      prefilter("slack", [candidate("Alice Slack"), candidate("Slack")]),
    );
    expect(ids(rows)).toEqual(["id:Slack", "id:Alice Slack"]);
    expect(rows.every((row) => row.verb === "open")).toBe(true);
  });
});
