import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors: Array<Record<string, string>>;
  }>,
  writableSkills: [] as string[],
  mutate: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project-a" }) }));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({
    hasScope: (scope: string, resourceId: string) =>
      scope === "skill:write" && state.writableSkills.includes(resourceId),
    grants: state.grants,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
    resourceId,
  }: {
    children: ReactNode;
    scope: string;
    resourceId: string;
  }) =>
    scope === "skill:write" && state.writableSkills.includes(resourceId) ? (
      <>{children}</>
    ) : null,
}));
vi.mock("@/components/ui/ViewToggle/use-view-mode", () => ({
  useViewMode: () => ["grid", vi.fn()],
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: () => undefined,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  useSkillDistributionsInfinite: state.query,
  invalidateAllSkillDistributions: vi.fn(),
}));
vi.mock("@gram/client/react-query/undistributeSkill.js", () => ({
  useUndistributeSkillMutation: () => ({ mutate: state.mutate }),
}));
vi.mock("../skills/SkillPickerDialog", () => ({
  SkillPickerDialog: () => null,
}));
import { PluginSkillsSection } from "./PluginSkillsSection";
afterEach(cleanup);
beforeEach(() => {
  state.grants = [
    {
      scope: "skill:read",
      selectors: [{ projectId: "project-a", resourceId: "skill-a" }],
    },
  ];
  state.writableSkills = [];
  state.mutate.mockReset();
  state.query.mockReset().mockReturnValue({
    data: { pages: [{ result: { distributions: [] } }] },
    hasNextPage: false,
  });
});
describe("plugin membership authorization", () => {
  it("invokes undistribute from the card remove control without card navigation", () => {
    state.writableSkills = ["skill-a"];
    state.query.mockReturnValue({
      data: {
        pages: [
          {
            result: {
              distributions: [
                {
                  id: "distribution-a",
                  skillId: "skill-a",
                  pluginId: "plugin-a",
                  skillDisplayName: "Example skill",
                  skillName: "example-skill",
                },
              ],
            },
          },
        ],
      },
      hasNextPage: false,
    });
    render(
      <TooltipProvider>
        <MemoryRouter>
          <PluginSkillsSection
            pluginId="plugin-a"
            skillId="skill-a"
            onMutated={vi.fn<(message: string) => void>()}
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    const remove = screen.getByRole("button", { name: "Remove skill" });
    // The card must not own button semantics that intercept nested keyboard actions.
    expect(remove.parentElement?.closest('[role="button"]')).toBeNull();
    fireEvent.click(remove);
    expect(state.mutate).toHaveBeenCalledWith(
      {
        request: {
          undistributeSkillRequestBody: { id: "skill-a", pluginId: "plugin-a" },
        },
      },
      expect.any(Object),
    );
  });
  it("hides removal for a writer of a different skill", () => {
    state.writableSkills = ["skill-b"];
    state.query.mockReturnValue({
      data: {
        pages: [
          {
            result: {
              distributions: [
                {
                  id: "distribution-a",
                  skillId: "skill-a",
                  pluginId: "plugin-a",
                  skillDisplayName: "Example skill",
                  skillName: "example-skill",
                },
              ],
            },
          },
        ],
      },
      hasNextPage: false,
    });
    render(
      <TooltipProvider>
        <MemoryRouter>
          <PluginSkillsSection
            pluginId="plugin-a"
            skillId="skill-a"
            onMutated={vi.fn<(message: string) => void>()}
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    expect(screen.queryByRole("button", { name: "Remove skill" })).toBeNull();
    expect(state.mutate).not.toHaveBeenCalled();
  });
  it("filters membership by the concrete skill for resource-only readers", () => {
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        skillId="skill-a"
        onMutated={vi.fn<(message: string) => void>()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: "skill-a", limit: 50 },
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("retains the complete membership for project readers", () => {
    state.grants = [
      { scope: "skill:read", selectors: [{ projectId: "project-a" }] },
    ];
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        skillId="skill-a"
        onMutated={vi.fn<(message: string) => void>()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: undefined, limit: 50 },
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("loads membership for an explicit project-resource reader without a skill ID", () => {
    state.grants = [
      {
        scope: "skill:read",
        selectors: [{ projectId: "project-a", resourceId: "project-a" }],
      },
    ];
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        onMutated={vi.fn<(message: string) => void>()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: undefined, limit: 50 },
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("does not request an unscoped membership without project permission", () => {
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        onMutated={vi.fn<(message: string) => void>()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: undefined, limit: 50 },
      undefined,
      { throwOnError: false, enabled: false },
    );
  });
});
