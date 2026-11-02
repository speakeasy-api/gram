import { describe, expect, it } from "vitest";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  EVENT_SOURCE_ATTRIBUTE_PATH,
  loadedViaLogFilter,
  selectedLoadedVia,
  skillLoadChannel,
  splitLoads,
  splitSkillLoads,
  withLoadedVia,
} from "./skillLoadChannel";

describe("skillLoadChannel", () => {
  it("reads hook-reported skill rows as hook", () => {
    expect(skillLoadChannel({ targetType: "skill", eventSource: "hook" })).toBe(
      "hook",
    );
  });

  it("reads any other skill row as MCP", () => {
    expect(
      skillLoadChannel({ targetType: "skill", eventSource: "resource_read" }),
    ).toBe("mcp");
  });

  it("ignores rows that are not skill loads", () => {
    expect(
      skillLoadChannel({
        targetType: "hosted_mcp_server",
        eventSource: "tool_call",
      }),
    ).toBeNull();
  });
});

describe("loaded via filters", () => {
  it("round-trips each channel through the applied chips", () => {
    expect(selectedLoadedVia(withLoadedVia([], "mcp"))).toBe("mcp");
    expect(selectedLoadedVia(withLoadedVia([], "hook"))).toBe("hook");
  });

  it("replaces the previous channel rather than stacking", () => {
    const filters = withLoadedVia(withLoadedVia([], "hook"), "mcp");
    expect(
      filters.filter((f) => f.path === EVENT_SOURCE_ATTRIBUTE_PATH),
    ).toHaveLength(1);
    expect(selectedLoadedVia(filters)).toBe("mcp");
  });

  it("clears only its own path", () => {
    const other = {
      id: "a",
      path: "gram.tool.name",
      op: Operator.Eq,
      value: "x",
    };
    expect(withLoadedVia(withLoadedVia([other], "hook"), null)).toEqual([
      other,
    ]);
  });

  it("does not claim a chip it did not write", () => {
    expect(
      selectedLoadedVia([
        {
          id: "a",
          path: EVENT_SOURCE_ATTRIBUTE_PATH,
          op: Operator.Eq,
          value: "tool_call",
        },
      ]),
    ).toBeNull();
  });

  it("builds the payload filter for MCP as not-a-hook", () => {
    expect(loadedViaLogFilter("mcp")).toEqual({
      path: EVENT_SOURCE_ATTRIBUTE_PATH,
      operator: Operator.NotEq,
      values: ["hook"],
    });
  });
});

describe("splitLoads", () => {
  it("counts what hooks did not report as MCP", () => {
    expect(splitLoads(167, 18)).toEqual({ total: 167, mcp: 149, hook: 18 });
  });

  it("never reports more hook loads than total loads", () => {
    expect(splitLoads(3, 5)).toEqual({ total: 3, mcp: 0, hook: 3 });
  });
});

describe("splitSkillLoads", () => {
  const skills = [
    { targetId: "release-checklist", eventCount: 96 },
    { targetId: "company-glossary", eventCount: 71 },
  ];

  it("reads a skill missing from a complete hook ranking as all MCP", () => {
    const bySkill = splitSkillLoads(
      skills,
      new Map([["release-checklist", 18]]),
      true,
    );

    expect(bySkill.get("release-checklist")).toEqual({
      total: 96,
      mcp: 78,
      hook: 18,
    });
    expect(bySkill.get("company-glossary")).toEqual({
      total: 71,
      mcp: 71,
      hook: 0,
    });
  });

  it("leaves a skill unsplit when the hook ranking was cut off", () => {
    const bySkill = splitSkillLoads(
      skills,
      new Map([["release-checklist", 18]]),
      false,
    );

    expect(bySkill.get("release-checklist")).toEqual({
      total: 96,
      mcp: 78,
      hook: 18,
    });
    expect(bySkill.has("company-glossary")).toBe(false);
  });
});
