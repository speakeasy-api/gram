import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors: Array<Record<string, string>>;
  }>,
  mutate: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-a" }),
  useProject: () => ({ id: "project-a", slug: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({
    grants: state.grants,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
  }),
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
  state.mutate.mockReset();
  state.query.mockReset().mockReturnValue({
    data: { pages: [{ result: { distributions: [] } }] },
    hasNextPage: false,
  });
});
describe("plugin membership authorization", () => {
  it("does not offer the project skill picker to resource-only readers", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceId: "project-a" }],
    });
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        skillId="skill-a"
        onMutated={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: "Add Skill" })).toBeNull();
  });
  it("invokes undistribute from the card remove control without card navigation", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceKind: "project", resourceId: "project-a" }],
    });
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
  it("hides removal for skill writers without plugin write", () => {
    state.grants.push({
      scope: "skill:write",
      selectors: [{ resourceKind: "skill", resourceId: "skill-a" }],
    });
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
      {
        gramProject: "project-a",
        gramSession: "session-a",
        pluginId: "plugin-a",
        skillId: "skill-a",
        limit: 50,
      },
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
      {
        gramProject: "project-a",
        gramSession: "session-a",
        pluginId: "plugin-a",
        skillId: undefined,
        limit: 50,
      },
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
      {
        gramProject: "project-a",
        gramSession: "session-a",
        pluginId: "plugin-a",
        skillId: undefined,
        limit: 50,
      },
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
      {
        gramProject: "project-a",
        gramSession: "session-a",
        pluginId: "plugin-a",
        skillId: undefined,
        limit: 50,
      },
      undefined,
      { throwOnError: false, enabled: false },
    );
  });
});
