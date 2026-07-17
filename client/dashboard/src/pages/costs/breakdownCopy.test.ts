import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { describe, expect, it } from "vitest";
import { breakdownCaption, breakdownTitle } from "./breakdownCopy";
import { PIVOTS, SESSIONS_AXIS } from "./taxonomy";

// The copy is assembled from dimension labels, so every label has to read
// correctly in four sentence frames. Expectations are spelled out in full rather
// than derived from LABELS — a test that rebuilt the string the same way the
// code does would pass on any label, including "Cost by Employment Statuss".

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

const COST = "$4.04";

const CASES: Case[] = [
  {
    dim: Dimension.DivisionName,
    title: "Cost by Division",
    empty: "Splitting spend by Division.",
    single: "$4.04 — all from a single Division.",
    split: "$4.04 split across 3 Divisions.",
  },
  {
    dim: Dimension.DepartmentName,
    title: "Cost by Department",
    empty: "Splitting spend by Department.",
    single: "$4.04 — all from a single Department.",
    split: "$4.04 split across 3 Departments.",
  },
  {
    dim: Dimension.Email,
    title: "Cost by User",
    empty: "Splitting spend by User.",
    single: "$4.04 — all from a single User.",
    split: "$4.04 split across 3 Users.",
  },
  {
    dim: Dimension.HookSource,
    title: "Cost by Agent",
    empty: "Splitting spend by Agent.",
    single: "$4.04 — all from a single Agent.",
    split: "$4.04 split across 3 Agents.",
  },
  {
    dim: Dimension.JobTitle,
    title: "Cost by Job Title",
    empty: "Splitting spend by Job Title.",
    single: "$4.04 — all from a single Job Title.",
    split: "$4.04 split across 3 Job Titles.",
  },
  {
    dim: Dimension.EmployeeType,
    title: "Cost by Employment Type",
    empty: "Splitting spend by Employment Type.",
    single: "$4.04 — all from a single Employment Type.",
    split: "$4.04 split across 3 Employment Types.",
  },
  {
    dim: Dimension.CostCenterName,
    title: "Cost by Cost Center",
    empty: "Splitting spend by Cost Center.",
    single: "$4.04 — all from a single Cost Center.",
    split: "$4.04 split across 3 Cost Centers.",
  },
  {
    dim: Dimension.Model,
    title: "Cost by Model",
    empty: "Splitting spend by Model.",
    single: "$4.04 — all from a single Model.",
    split: "$4.04 split across 3 Models.",
  },
  {
    dim: Dimension.AccountType,
    title: "Cost by Account Type",
    empty: "Splitting spend by Account Type.",
    single: "$4.04 — all from a single Account Type.",
    split: "$4.04 split across 3 Account Types.",
  },
  {
    dim: Dimension.Provider,
    title: "Cost by Provider",
    empty: "Splitting spend by Provider.",
    single: "$4.04 — all from a single Provider.",
    split: "$4.04 split across 3 Providers.",
  },
  {
    dim: Dimension.Role,
    title: "Cost by Role",
    empty: "Splitting spend by Role.",
    single: "$4.04 — all from a single Role.",
    split: "$4.04 split across 3 Roles.",
  },
  {
    dim: Dimension.McpServerName,
    title: "Cost by MCP Server",
    empty: "Splitting spend by MCP Server.",
    single: "$4.04 — all from a single MCP Server.",
    split: "$4.04 split across 3 MCP Servers.",
  },
  {
    dim: Dimension.McpToolName,
    title: "Cost by MCP Tool",
    empty: "Splitting spend by MCP Tool.",
    single: "$4.04 — all from a single MCP Tool.",
    split: "$4.04 split across 3 MCP Tools.",
  },
  {
    dim: Dimension.SkillName,
    title: "Cost by Skill",
    empty: "Splitting spend by Skill.",
    single: "$4.04 — all from a single Skill.",
    split: "$4.04 split across 3 Skills.",
  },
  {
    dim: Dimension.AgentName,
    title: "Cost by Subagent",
    empty: "Splitting spend by Subagent.",
    single: "$4.04 — all from a single Subagent.",
    split: "$4.04 split across 3 Subagents.",
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

  describe("the sessions axis", () => {
    it("names itself a list, not a breakdown", () => {
      expect(breakdownTitle(SESSIONS_AXIS, Dimension.Model)).toBe(
        "Agent sessions",
      );
      expect(
        breakdownCaption({
          axisValue: SESSIONS_AXIS,
          groupBy: Dimension.Model,
          costLabel: COST,
          groupCount: 42,
        }),
      ).toBe("Every agent session, listed individually.");
    });

    // Sessions render via a tableOverride, so `rows` is empty and groupCount is
    // 0 — the caption must not fall through to "Splitting spend by Model."
    it("ignores the group count the overridden table leaves at zero", () => {
      expect(
        breakdownCaption({
          axisValue: SESSIONS_AXIS,
          groupBy: Dimension.Model,
          costLabel: COST,
          groupCount: 0,
        }),
      ).toBe("Every agent session, listed individually.");
    });
  });

  // The scope qualifier this copy deliberately omits: "of R&D" reads fine but
  // "of Adam" does not, and the hero above already names the entity.
  it("never qualifies the spend with an entity name", () => {
    const caption = breakdownCaption({
      axisValue: Dimension.HookSource,
      groupBy: Dimension.HookSource,
      costLabel: COST,
      groupCount: 1,
    });
    expect(caption).not.toMatch(/\bof\b|\bin\b/);
  });
});
