import { describe, expect, it } from "vitest";
import {
  CERTAIN_TARGET_THRESHOLD,
  MIN_FUZZY,
  PREFILTER_LIMIT,
  READY_THRESHOLD,
  STOP_WORDS,
  fuzzyScore,
  isReady,
  prefilter,
  rank,
  resolveVerb,
  tokens,
} from "./ranker";
import {
  MUTATING_VERBS,
  type Judgment,
  type LauncherCandidate,
  type Verb,
} from "./candidates/types";

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
  it("exposes the default thresholds", () => {
    expect(PREFILTER_LIMIT).toBe(13);
    expect(MIN_FUZZY).toBe(0.15);
  });

  it("keeps every match in all but excludes people from sendable", () => {
    const server = candidate("Slack MCP", { kind: "mcp_server" });
    const person = candidate("Alice Slack", { kind: "person" });
    const unrelated = candidate("Jira");
    const pre = prefilter("slack", [person, server, unrelated]);
    expect(ids(pre.all)).toEqual([server.id, person.id]);
    expect(pre.sendable.map((c) => c.id)).toEqual([server.id]);
  });

  it("keeps fuzzy-only candidates (identity-page recents) out of sendable", () => {
    const server = candidate("Slack MCP", { kind: "mcp_server" });
    const identityRecent = candidate("Alice Slack", {
      id: "recent:/acme/projects/default/identities/dXNlcjox/overview",
      kind: "recent",
      fuzzyOnly: true,
    });
    const pre = prefilter("slack", [identityRecent, server]);
    expect(ids(pre.all)).toEqual([server.id, identityRecent.id]);
    expect(pre.sendable.map((c) => c.id)).toEqual([server.id]);
  });

  it("respects the limit of 13 with 20 candidates", () => {
    const candidates = Array.from({ length: 20 }, (_, i) =>
      candidate(`Item ${String(i + 1).padStart(2, "0")}`),
    );
    const pre = prefilter("item", candidates);
    expect(pre.all).toHaveLength(20);
    expect(pre.sendable).toHaveLength(13);
    expect(pre.sendable.map((c) => c.title)).toEqual(
      candidates.slice(0, 13).map((c) => c.title),
    );
  });

  it("honours custom limit and minFuzzy", () => {
    const candidates = [
      candidate("Slack"),
      candidate("Slackbot"),
      candidate("Sales Lookup Access Key"),
    ];
    const pre = prefilter("slack", candidates, { limit: 1, minFuzzy: 0.5 });
    expect(pre.all).toHaveLength(2);
    expect(pre.sendable).toHaveLength(1);
  });

  it("returns nothing for an empty query", () => {
    const pre = prefilter("", [candidate("Slack")]);
    expect(pre.all).toEqual([]);
    expect(pre.sendable).toEqual([]);
  });
});

describe("rank", () => {
  const enabled = candidate("Slack MCP", {
    id: "mcp:1",
    kind: "mcp_server",
    detail: "MCP server · enabled",
    verbs: ["open", "disable"],
  });
  const disabled = candidate("Slack MCP (disabled)", {
    id: "mcp:2",
    kind: "mcp_server",
    detail: "MCP server · disabled",
    verbs: ["open", "enable"],
  });

  it("equals fuzzy order without a judgment", () => {
    const pre = prefilter("slack", [disabled, enabled, candidate("Slackbot")]);
    const rows = rank(pre, null);
    expect(ids(rows)).toEqual(ids(pre.all));
    rows.forEach((row) => {
      expect(row.score).toBe(row.fuzzy);
      expect(row.verb).toBe("open");
      expect(row.targetP).toBe(0);
    });
  });

  it("follows the judgment target for the 'wifi off' style pair", () => {
    const pre = prefilter("slack off", [enabled, disabled]);
    expect(ids(pre.all)).toEqual(["mcp:1", "mcp:2"]);

    const favourDisabled: Judgment = {
      target: { "mcp:1": 0.1, "mcp:2": 0.9 },
      action: { open: 0.2, enable: 0.1, disable: 0.6, unclear: 0.1 },
      ready: 0.7,
    };
    const rows = rank(pre, favourDisabled);
    expect(ids(rows)).toEqual(["mcp:2", "mcp:1"]);
    expect(rows[0]?.targetP).toBe(0.9);
    // The disabled row supports [open, enable]; open (0.2) beats enable (0.1).
    expect(rows[0]?.verb).toBe("open");
    expect(rows[1]?.verb).toBe("disable");

    const favourEnabled: Judgment = {
      target: { "mcp:1": 0.9, "mcp:2": 0.1 },
      action: { open: 0.2, enable: 0.1, disable: 0.6, unclear: 0.1 },
      ready: 0.7,
    };
    expect(ids(rank(pre, favourEnabled))).toEqual(["mcp:1", "mcp:2"]);
  });

  it("applies the score formula to sent candidates", () => {
    const pre = prefilter("slack", [enabled]);
    const fuzzy = pre.all[0]?.fuzzy ?? 0;
    const judgment: Judgment = {
      target: { "mcp:1": 0.8 },
      action: { open: 0.3, disable: 0.5, unclear: 0.2 },
      ready: 0.5,
    };
    const [row] = rank(pre, judgment);
    expect(row?.score).toBeCloseTo(
      0.65 * 0.8 + 0.2 * (0.3 + 0.5) + 0.15 * fuzzy,
      10,
    );
  });

  it("scores candidates that were not sent at 0.15 * fuzzy", () => {
    const candidates = Array.from({ length: 20 }, (_, i) =>
      candidate(`Item ${String(i + 1).padStart(2, "0")}`),
    );
    const pre = prefilter("item", candidates);
    const judgment: Judgment = {
      target: {},
      action: { open: 1 },
      ready: 0,
    };
    const rows = rank(pre, judgment);
    const sentIds = new Set(pre.sendable.map((c) => c.id));
    const sent = rows.filter((row) => sentIds.has(row.candidate.id));
    const notSent = rows.filter((row) => !sentIds.has(row.candidate.id));
    expect(sent).toHaveLength(13);
    expect(notSent).toHaveLength(7);
    for (const row of notSent) {
      expect(row.score).toBeCloseTo(0.15 * row.fuzzy, 10);
    }
    for (const row of sent) {
      expect(row.score).toBeCloseTo(0.2 + 0.15 * row.fuzzy, 10);
    }
    // Judged rows sort above unjudged rows.
    expect(ids(rows.slice(0, 13))).toEqual(ids(sent));
  });

  it("breaks score ties on title", () => {
    const b = candidate("Bravo", { id: "b" });
    const a = candidate("Alpha", { id: "a" });
    const pre = prefilter("alpha bravo", [b, a]);
    const judgment: Judgment = {
      target: { a: 0.5, b: 0.5 },
      action: { open: 1 },
      ready: 0,
    };
    // Same fuzzy (one exact, one unmatched, titles of equal length).
    expect(pre.all[0]?.fuzzy).toBeCloseTo(pre.all[1]?.fuzzy ?? -1, 10);
    expect(ids(rank(pre, judgment))).toEqual(["a", "b"]);
  });
});

describe("resolveVerb", () => {
  const action = { disable: 0.8, open: 0.1, unclear: 0.1 };
  const judgment = (a: Record<string, number>): Judgment => ({
    target: {},
    action: a,
    ready: 0,
  });

  it("returns open without a judgment", () => {
    expect(
      resolveVerb(candidate("Slack MCP", { verbs: ["open", "disable"] }), null),
    ).toBe("open");
  });

  it("picks the best verb the candidate supports", () => {
    const verb = resolveVerb(
      candidate("Slack MCP", { verbs: ["open", "disable"] }),
      judgment(action),
    );
    expect(verb).toBe("disable");
    expect(MUTATING_VERBS.has(verb)).toBe(true);
  });

  it("falls back to open when the candidate lacks the winning verb", () => {
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs: ["open"] }),
        judgment(action),
      ),
    ).toBe("open");
  });

  it("returns open when unclear is the overall argmax", () => {
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs: ["open", "disable"] }),
        judgment({ disable: 0.3, open: 0.1, unclear: 0.6 }),
      ),
    ).toBe("open");
  });

  it("returns open when the candidate lacks the overall winner even if it has mass on another verb", () => {
    expect(
      resolveVerb(
        candidate("Marketplace", { verbs: ["open", "publish"] }),
        judgment({ disable: 0.9, unclear: 0.1 }),
      ),
    ).toBe("open");
  });

  // The row's own best verb is not the answer when a different mutating verb
  // won overall: the user asked to enable something, not to disable this.
  it("does not fall back to the row's own best verb when another verb won", () => {
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs: ["open", "disable"] }),
        judgment({ enable: 0.6, disable: 0.25, open: 0.1, unclear: 0.05 }),
      ),
    ).toBe("open");
    expect(
      resolveVerb(
        candidate("Jira MCP", { verbs: ["open", "enable"] }),
        judgment({ enable: 0.6, disable: 0.25, open: 0.1, unclear: 0.05 }),
      ),
    ).toBe("enable");
  });

  it("returns open when every verb has probability 0", () => {
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs: ["open", "disable"] }),
        judgment({ open: 0, disable: 0 }),
      ),
    ).toBe("open");
  });

  it("breaks overall ties toward the earlier key", () => {
    const verbs: Verb[] = ["open", "disable"];
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs }),
        judgment({ open: 0.5, disable: 0.5 }),
      ),
    ).toBe("open");
    expect(
      resolveVerb(
        candidate("Slack MCP", { verbs }),
        judgment({ disable: 0.5, open: 0.5 }),
      ),
    ).toBe("disable");
  });
});

describe("isReady", () => {
  const pre = prefilter("settings", [candidate("Settings", { id: "page" })]);

  it("exposes the thresholds", () => {
    expect(READY_THRESHOLD).toBe(0.6);
    expect(CERTAIN_TARGET_THRESHOLD).toBe(0.9);
  });

  it("is false without a judgment or rows", () => {
    expect(isReady(rank(pre, null), null)).toBe(false);
    const judgment: Judgment = { target: {}, action: {}, ready: 1 };
    expect(isReady([], judgment)).toBe(false);
  });

  it("is true when ready crosses the threshold", () => {
    const judgment: Judgment = {
      target: { page: 0.5 },
      action: {},
      ready: 0.6,
    };
    expect(isReady(rank(pre, judgment), judgment)).toBe(true);
  });

  it("is true when the top target is certain even if ready hedges", () => {
    const judgment: Judgment = {
      target: { page: 0.9 },
      action: {},
      ready: 0.2,
    };
    expect(isReady(rank(pre, judgment), judgment)).toBe(true);
  });

  it("is false when neither rule fires", () => {
    const judgment: Judgment = {
      target: { page: 0.5 },
      action: {},
      ready: 0.3,
    };
    expect(isReady(rank(pre, judgment), judgment)).toBe(false);
  });
});
