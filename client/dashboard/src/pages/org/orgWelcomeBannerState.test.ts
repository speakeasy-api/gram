import { describe, expect, it } from "vitest";

import {
  isOverviewZeroData,
  recommendedWelcomeCardId,
  selectWelcomeCardIds,
  type WelcomeBannerInputs,
  type WelcomeCardId,
} from "./orgWelcomeBannerState";

const base: WelcomeBannerInputs = {
  isTrial: false,
  isAdmin: false,
  isZeroData: false,
  canSetUpOrg: false,
  platformMcpEnabled: false,
};

function ids(overrides: Partial<WelcomeBannerInputs>): WelcomeCardId[] {
  return selectWelcomeCardIds({ ...base, ...overrides });
}

describe("selectWelcomeCardIds", () => {
  it("trial admin: demo, guide, enterprise", () => {
    expect(ids({ isTrial: true, isAdmin: true, canSetUpOrg: true })).toEqual([
      "demo",
      "guide",
      "enterprise",
    ]);
  });

  it("trial member: demo, guide", () => {
    expect(ids({ isTrial: true })).toEqual(["demo", "guide"]);
  });

  it("drops enterprise on trial when the wizard is ineligible", () => {
    expect(ids({ isTrial: true, isAdmin: true, canSetUpOrg: false })).toEqual([
      "demo",
      "guide",
    ]);
  });

  it("non-trial admin, zero data: guide, enterprise", () => {
    expect(
      ids({
        isAdmin: true,
        isZeroData: true,
        canSetUpOrg: true,
        platformMcpEnabled: true,
      }),
    ).toEqual(["guide", "enterprise"]);
  });

  it("non-trial admin, has data: platform MCP, enterprise", () => {
    expect(
      ids({
        isAdmin: true,
        canSetUpOrg: true,
        platformMcpEnabled: true,
      }),
    ).toEqual(["platformMcp", "enterprise"]);
  });

  it("substitutes guide when Platform MCP is not enabled", () => {
    expect(
      ids({
        isAdmin: true,
        canSetUpOrg: true,
        platformMcpEnabled: false,
      }),
    ).toEqual(["guide", "enterprise"]);
  });

  it("non-trial member, zero data: guide", () => {
    expect(ids({ isZeroData: true })).toEqual(["guide"]);
  });

  it("non-trial member, has data: default project", () => {
    expect(ids({})).toEqual(["defaultProject"]);
  });
});

describe("recommendedWelcomeCardId", () => {
  it("prefers platform MCP, then default project, then guide", () => {
    expect(recommendedWelcomeCardId(["platformMcp", "enterprise"])).toBe(
      "platformMcp",
    );
    expect(recommendedWelcomeCardId(["defaultProject"])).toBe("defaultProject");
    expect(recommendedWelcomeCardId(["demo", "guide", "enterprise"])).toBe(
      "guide",
    );
  });
});

describe("isOverviewZeroData", () => {
  it("treats a missing overview as zero data", () => {
    expect(isOverviewZeroData(undefined)).toBe(true);
    expect(isOverviewZeroData({})).toBe(true);
  });

  it("is zero data when there are no servers and no tool calls", () => {
    expect(
      isOverviewZeroData({
        summary: { activeServersCount: 0, totalToolCalls: 0 },
      }),
    ).toBe(true);
  });

  it("is not zero data once either signal is non-zero", () => {
    expect(
      isOverviewZeroData({
        summary: { activeServersCount: 1, totalToolCalls: 0 },
      }),
    ).toBe(false);
    expect(
      isOverviewZeroData({
        summary: { activeServersCount: 0, totalToolCalls: 4 },
      }),
    ).toBe(false);
  });
});
