import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { describe, expect, it } from "vitest";
import { breakdownCaption, breakdownTitle, scopePhrase } from "./breakdownCopy";
import { type Crumb, PIVOTS, SESSIONS_AXIS } from "./taxonomy";

// The copy is assembled from dimension labels and drill-path values, so both
// have to read correctly in every sentence frame. Expectations are spelled out
// in full rather than derived from LABELS — a test that rebuilt the string the
// way the code does would pass on "Cost by Employment Statuss".

const COST = "$4.04";

const at = (dim: Dimension, value: string): Crumb => ({ dim, value });
const ADAM = at(Dimension.Email, "adam@speakeasy.com");
const CLAUDE_CODE = at(Dimension.HookSource, "claude-code");
const RND = at(Dimension.DivisionName, "R&D");
const ENGINEERING = at(Dimension.DepartmentName, "Engineering");

// ── Scope: the drill path as prose ──────────────────────────────────────────

describe("scopePhrase", () => {
  type ScopeCase = { name: string; path: Crumb[]; spend: string };
  const CASES: ScopeCase[] = [
    { name: "the project root", path: [], spend: "all project spend" },
    { name: "one division", path: [RND], spend: "R&D's spend" },
    // A user reads as their name, not the address the breadcrumb uses.
    { name: "one user", path: [ADAM], spend: "Adam's spend" },
    {
      name: "a user's agent",
      path: [ADAM, CLAUDE_CODE],
      spend: "Adam's claude-code spend",
    },
    {
      name: "two org levels",
      path: [RND, ENGINEERING],
      spend: "R&D's Engineering spend",
    },
    {
      name: "the full chain",
      path: [RND, ENGINEERING, ADAM, CLAUDE_CODE],
      spend: "R&D's Engineering's Adam's claude-code spend",
    },
  ];

  it.each(CASES)("names $name", ({ path, spend }) => {
    expect(scopePhrase(path, "spend")).toBe(spend);
  });

  it("swaps the noun for the session list", () => {
    expect(scopePhrase([], "sessions")).toBe("all project sessions");
    expect(scopePhrase([ADAM], "sessions")).toBe("Adam's sessions");
    expect(scopePhrase([ADAM, CLAUDE_CODE], "sessions")).toBe(
      "Adam's claude-code sessions",
    );
  });

  // "Operations's" is wrong; English drops the second s.
  it("does not double the s on a name that ends in one", () => {
    expect(
      scopePhrase([at(Dimension.DivisionName, "Operations")], "spend"),
    ).toBe("Operations' spend");
    expect(
      scopePhrase(
        [at(Dimension.DivisionName, "Operations"), ENGINEERING],
        "spend",
      ),
    ).toBe("Operations' Engineering spend");
  });

  it("keeps an unset value legible", () => {
    expect(scopePhrase([at(Dimension.DivisionName, "")], "spend")).toBe(
      "(unset)'s spend",
    );
  });

  // The empty user bucket is the company credential's spend (Claude Code on an
  // API key/gateway emits no user identity), so it reads as the shared team
  // account rather than "(unset)".
  it("names the unset user bucket the team-wide account", () => {
    expect(scopePhrase([at(Dimension.Email, "")], "spend")).toBe(
      "Team-wide API Usage's spend",
    );
  });
});

// ── Title + caption, per breakdown axis ─────────────────────────────────────

type Case = {
  dim: Dimension;
  // "Cost by …"
  title: string;
  // groupCount 0 — loading or an empty slice.
  empty: string;
  // groupCount 1 — not a split; the active axis is offered even with one value.
  single: string;
  // groupCount > 1 — the real breakdown, and the plural under test.
  split: string;
};

// Captions below are asserted at the project root, so each row isolates the
// dimension's own labels; the drill-path grammar is covered by scopePhrase.
const CASES: Case[] = [
  {
    dim: Dimension.DivisionName,
    title: "Cost by Division",
    empty: "Showing all project spend, broken down by Division.",
    single: "Showing all project spend — $4.04, all from a single Division.",
    split: "Showing all project spend — $4.04 across 3 Divisions.",
  },
  {
    dim: Dimension.DepartmentName,
    title: "Cost by Department",
    empty: "Showing all project spend, broken down by Department.",
    single: "Showing all project spend — $4.04, all from a single Department.",
    split: "Showing all project spend — $4.04 across 3 Departments.",
  },
  {
    dim: Dimension.Email,
    title: "Cost by User",
    empty: "Showing all project spend, broken down by User.",
    single: "Showing all project spend — $4.04, all from a single User.",
    split: "Showing all project spend — $4.04 across 3 Users.",
  },
  {
    dim: Dimension.HookSource,
    title: "Cost by Agent",
    empty: "Showing all project spend, broken down by Agent.",
    single: "Showing all project spend — $4.04, all from a single Agent.",
    split: "Showing all project spend — $4.04 across 3 Agents.",
  },
  {
    dim: Dimension.JobTitle,
    title: "Cost by Job Title",
    empty: "Showing all project spend, broken down by Job Title.",
    single: "Showing all project spend — $4.04, all from a single Job Title.",
    split: "Showing all project spend — $4.04 across 3 Job Titles.",
  },
  {
    dim: Dimension.EmployeeType,
    title: "Cost by Employment Type",
    empty: "Showing all project spend, broken down by Employment Type.",
    single:
      "Showing all project spend — $4.04, all from a single Employment Type.",
    split: "Showing all project spend — $4.04 across 3 Employment Types.",
  },
  {
    dim: Dimension.CostCenterName,
    title: "Cost by Cost Center",
    empty: "Showing all project spend, broken down by Cost Center.",
    single: "Showing all project spend — $4.04, all from a single Cost Center.",
    split: "Showing all project spend — $4.04 across 3 Cost Centers.",
  },
  {
    dim: Dimension.Model,
    title: "Cost by Model",
    empty: "Showing all project spend, broken down by Model.",
    single: "Showing all project spend — $4.04, all from a single Model.",
    split: "Showing all project spend — $4.04 across 3 Models.",
  },
  {
    dim: Dimension.AccountType,
    title: "Cost by Account Type",
    empty: "Showing all project spend, broken down by Account Type.",
    single:
      "Showing all project spend — $4.04, all from a single Account Type.",
    split: "Showing all project spend — $4.04 across 3 Account Types.",
  },
  {
    dim: Dimension.Provider,
    title: "Cost by Provider",
    empty: "Showing all project spend, broken down by Provider.",
    single: "Showing all project spend — $4.04, all from a single Provider.",
    split: "Showing all project spend — $4.04 across 3 Providers.",
  },
  {
    dim: Dimension.Role,
    title: "Cost by Role",
    empty: "Showing all project spend, broken down by Role.",
    single: "Showing all project spend — $4.04, all from a single Role.",
    split: "Showing all project spend — $4.04 across 3 Roles.",
  },
  {
    dim: Dimension.McpServerName,
    title: "Cost by MCP Server",
    empty: "Showing all project spend, broken down by MCP Server.",
    single: "Showing all project spend — $4.04, all from a single MCP Server.",
    split: "Showing all project spend — $4.04 across 3 MCP Servers.",
  },
  {
    dim: Dimension.McpToolName,
    title: "Cost by MCP Tool",
    empty: "Showing all project spend, broken down by MCP Tool.",
    single: "Showing all project spend — $4.04, all from a single MCP Tool.",
    split: "Showing all project spend — $4.04 across 3 MCP Tools.",
  },
  {
    dim: Dimension.SkillName,
    title: "Cost by Skill",
    empty: "Showing all project spend, broken down by Skill.",
    single: "Showing all project spend — $4.04, all from a single Skill.",
    split: "Showing all project spend — $4.04 across 3 Skills.",
  },
  {
    dim: Dimension.AgentName,
    title: "Cost by Subagent",
    empty: "Showing all project spend, broken down by Subagent.",
    single: "Showing all project spend — $4.04, all from a single Subagent.",
    split: "Showing all project spend — $4.04 across 3 Subagents.",
  },
];

describe("breakdown copy", () => {
  it.each(CASES)("titles the $title cut", ({ dim, title }) => {
    expect(breakdownTitle(dim, dim)).toBe(title);
  });

  it.each(CASES)("captions $title", ({ dim, empty, single, split }) => {
    const caption = (groupCount: number) =>
      breakdownCaption({
        axisValue: dim,
        groupBy: dim,
        path: [],
        costLabel: COST,
        groupCount,
      });
    expect(caption(0)).toBe(empty);
    expect(caption(1)).toBe(single);
    expect(caption(3)).toBe(split);
  });

  // A pivot added without reviewing its copy would otherwise silently fall back
  // to "Cost by group" / "3 Groups".
  it("covers every pivot the taxonomy offers", () => {
    expect(CASES.map((c) => c.dim).sort()).toEqual(
      PIVOTS.map((p) => p.dim).sort(),
    );
  });

  it("reads the drill path into the caption", () => {
    expect(
      breakdownCaption({
        axisValue: Dimension.Model,
        groupBy: Dimension.Model,
        path: [ADAM, CLAUDE_CODE],
        costLabel: COST,
        groupCount: 2,
      }),
    ).toBe("Showing Adam's claude-code spend — $4.04 across 2 Models.");
  });

  describe("the sessions axis", () => {
    const sessions = (path: Crumb[], groupCount = 42) =>
      breakdownCaption({
        axisValue: SESSIONS_AXIS,
        groupBy: Dimension.Model,
        path,
        costLabel: COST,
        groupCount,
      });

    it("names itself a list, not a breakdown", () => {
      expect(breakdownTitle(SESSIONS_AXIS, Dimension.Model)).toBe(
        "Agent sessions",
      );
      expect(sessions([])).toBe(
        "Showing all project sessions, listed individually.",
      );
      expect(sessions([ADAM, CLAUDE_CODE])).toBe(
        "Showing Adam's claude-code sessions, listed individually.",
      );
    });

    // Sessions render via a tableOverride, so `rows` is empty and groupCount is
    // 0 — the caption must not fall through to "broken down by Model."
    it("ignores the group count the overridden table leaves at zero", () => {
      expect(sessions([ADAM], 0)).toBe(
        "Showing Adam's sessions, listed individually.",
      );
    });
  });

  // The qualifier that produced "$4.04 of Adam": a user is possessed, never
  // used as the object of a preposition.
  it("never says 'of' or 'in' a user", () => {
    for (const groupCount of [0, 1, 3]) {
      const caption = breakdownCaption({
        axisValue: Dimension.HookSource,
        groupBy: Dimension.HookSource,
        path: [ADAM],
        costLabel: COST,
        groupCount,
      });
      expect(caption).not.toMatch(/\b(of|in) Adam\b/);
    }
  });
});
