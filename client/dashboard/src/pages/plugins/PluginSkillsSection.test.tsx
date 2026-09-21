import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  fullRead: false,
  canWrite: false,
  mutate: vi.fn(),
  query: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project-a" }) }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => state.canWrite,
    hasAnyScopeInProject: () => state.fullRead,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    state.canWrite ? <>{children}</> : null,
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
  state.fullRead = false;
  state.canWrite = false;
  state.mutate.mockReset();
  state.query.mockReset().mockReturnValue({
    data: { pages: [{ result: { distributions: [] } }] },
    hasNextPage: false,
  });
});
describe("plugin membership authorization", () => {
  it("invokes undistribute from the card remove control without card navigation", () => {
    state.canWrite = true;
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
            onMutated={vi.fn()}
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
  it("filters membership by the concrete skill for resource-only readers", () => {
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        skillId="skill-a"
        onMutated={vi.fn()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: "skill-a", limit: 50 },
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("retains the complete membership for project readers", () => {
    state.fullRead = true;
    render(
      <PluginSkillsSection
        pluginId="plugin-a"
        skillId="skill-a"
        onMutated={vi.fn()}
      />,
    );
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: undefined, limit: 50 },
      undefined,
      { throwOnError: false, enabled: true },
    );
  });
  it("does not request an unscoped membership without project permission", () => {
    render(<PluginSkillsSection pluginId="plugin-a" onMutated={vi.fn()} />);
    expect(state.query).toHaveBeenCalledWith(
      { pluginId: "plugin-a", skillId: undefined, limit: 50 },
      undefined,
      { throwOnError: false, enabled: false },
    );
  });
});
