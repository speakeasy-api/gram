import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors?: Array<Record<string, string>>;
  }>,
  query: vi.fn(() => ({ isPending: true })),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project-a" }) }));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ isLoading: false, grants: state.grants }),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@gram/client/react-query/skillEfficacyInsights.js", () => ({
  useSkillEfficacyInsights: state.query,
}));
import { SkillInsightsSection } from "./SkillInsightsSection";
afterEach(cleanup);
beforeEach(() => {
  state.query.mockClear();
  state.grants = [
    {
      scope: "skill:read",
      selectors: [{ resourceId: "skill-a", projectId: "project-a" }],
    },
  ];
});
describe("skill insight collection permissions", () => {
  it("does not request project analytics with only a concrete skill grant", () => {
    const { container } = render(
      <SkillInsightsSection
        data={{ skill: { id: "skill-a" } } as GetSkillResult}
        versionLabels={new Map()}
        versionsLoading={false}
        versionsError={null}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { skillIds: ["skill-a"], includeVersions: true },
      undefined,
      { throwOnError: false, enabled: false },
    );
    expect(container.textContent).toBe("");
  });
  it("suppresses analytics for unrestricted skill readers without project read", () => {
    state.grants = [{ scope: "skill:read" }, { scope: "skill:write" }];
    render(
      <SkillInsightsSection
        data={{ skill: { id: "skill-a" } } as GetSkillResult}
        versionLabels={new Map()}
        versionsLoading={false}
        versionsError={null}
      />,
    );
    expect(state.query).toHaveBeenLastCalledWith(
      expect.any(Object),
      undefined,
      { throwOnError: false, enabled: false },
    );
  });
  it("preserves analytics for administrators with both required reads", () => {
    state.grants = [{ scope: "project:read" }, { scope: "skill:read" }];
    render(
      <SkillInsightsSection
        data={{ skill: { id: "skill-a" } } as GetSkillResult}
        versionLabels={new Map()}
        versionsLoading={false}
        versionsError={null}
      />,
    );
    expect(state.query).toHaveBeenLastCalledWith(
      expect.any(Object),
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("does not borrow project read from another project", () => {
    state.grants = [
      { scope: "skill:read" },
      { scope: "project:read", selectors: [{ projectId: "project-b" }] },
    ];
    render(
      <SkillInsightsSection
        data={{ skill: { id: "skill-a" } } as GetSkillResult}
        versionLabels={new Map()}
        versionsLoading={false}
        versionsError={null}
      />,
    );
    expect(state.query).toHaveBeenLastCalledWith(
      expect.any(Object),
      undefined,
      { throwOnError: false, enabled: false },
    );
  });
});
