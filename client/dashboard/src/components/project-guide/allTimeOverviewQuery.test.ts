import type { GetProjectOverviewResult } from "@gram/client/models/components/getprojectoverviewresult.js";
import { describe, expect, it } from "vitest";
import {
  PROJECT_GUIDE_OVERVIEW_FROM,
  allTimeOverviewScope,
  isOverviewEmpty,
} from "./allTimeOverviewQuery";

function overview(
  summary: Partial<GetProjectOverviewResult["summary"]>,
): GetProjectOverviewResult {
  const empty = {
    activeServersCount: 0,
    activeUsersCount: 0,
    failedChats: 0,
    failedToolCalls: 0,
    llmClientBreakdown: [],
    resolvedChats: 0,
    topServers: [],
    topUsers: [],
    totalChats: 0,
    totalToolCalls: 0,
  };
  return {
    comparison: { ...empty },
    metricsMode: "tool_call",
    summary: { ...empty, ...summary },
  } as GetProjectOverviewResult;
}

describe("allTimeOverviewScope", () => {
  it("starts at the fixed all-time boundary", () => {
    const scope = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:30.500Z"),
    });
    expect(scope).toEqual({
      organization: "org",
      project: "proj",
      range: {
        from: PROJECT_GUIDE_OVERVIEW_FROM,
        to: "2026-08-17T12:01:00.000Z",
      },
    });
  });

  it("keeps the same key for a minute so the query is not re-issued per render", () => {
    const a = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:01.000Z"),
    });
    const b = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:59.000Z"),
    });
    expect(a).toEqual(b);
  });

  it("rounds a boundary instant up to the next bucket, never backwards", () => {
    const scope = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:00.000Z"),
    });
    expect(scope.range).toEqual({
      from: PROJECT_GUIDE_OVERVIEW_FROM,
      to: "2026-08-17T12:01:00.000Z",
    });
  });
});

describe("isOverviewEmpty", () => {
  it("treats a missing overview as not empty, so an unknown project keeps the dashboard", () => {
    expect(isOverviewEmpty(undefined)).toBe(false);
  });

  it("is empty with no servers, no tool calls, and no chats", () => {
    expect(isOverviewEmpty(overview({}))).toBe(true);
  });

  it("is not empty when hook telemetry arrived without a server or policy", () => {
    expect(isOverviewEmpty(overview({ totalToolCalls: 3 }))).toBe(false);
  });

  it("is not empty when a server was active", () => {
    expect(isOverviewEmpty(overview({ activeServersCount: 1 }))).toBe(false);
  });

  it("is not empty when the assistant has chats", () => {
    expect(isOverviewEmpty(overview({ totalChats: 2 }))).toBe(false);
  });
});
