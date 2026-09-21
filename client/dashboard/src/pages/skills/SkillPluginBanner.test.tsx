import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Skill } from "@gram/client/models/components/skill.js";

const discovery = vi.hoisted(() =>
  vi.fn(() => ({
    data: {
      plugins: [{ id: "plugin-a", name: "Example plugin", isDefault: true }],
    },
  })),
);
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
const permissions = vi.hoisted(() => ({
  hasScope: vi.fn(),
  distributions: true,
}));
vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => permissions.hasScope("plugin:write", "project-a"),
}));
vi.mock("@/pages/mcp/overview/PluginStatusBanner", () => ({
  ClientIconFan: () => null,
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: () => undefined,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/distributionPlugins.js", () => ({
  useDistributionPlugins: discovery,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  useSkillDistributionsInfinite: () => ({
    data: {
      pages: [
        {
          result: {
            distributions: permissions.distributions
              ? [
                  {
                    id: "distribution-a",
                    pluginId: "plugin-a",
                    pluginName: "Example plugin",
                  },
                ]
              : [],
          },
        },
      ],
    },
    hasNextPage: false,
  }),
  invalidateAllSkillDistributions: vi.fn(),
}));
vi.mock("@gram/client/react-query/distributeSkill.js", () => ({
  useDistributeSkillMutation: () => ({}),
}));
vi.mock("@gram/client/react-query/undistributeSkill.js", () => ({
  useUndistributeSkillMutation: () => ({}),
}));
import { SkillPluginBanner } from "./SkillPluginBanner";

afterEach(cleanup);
beforeEach(() => {
  permissions.hasScope
    .mockReset()
    .mockImplementation(
      (scope, resourceId) =>
        scope === "plugin:write" && resourceId === "project-a",
    );
  permissions.distributions = true;
});
describe("SkillPluginBanner", () => {
  it("shows the blocked state without distribution controls or links", () => {
    permissions.distributions = false;
    render(
      <TooltipProvider>
        <MemoryRouter>
          <SkillPluginBanner
            skill={
              {
                id: "scoped-skill",
                hasValidVersion: false,
                versionCount: 1,
              } as Skill
            }
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    expect(screen.getByText("Distribution blocked")).toBeTruthy();
    expect(
      screen.getByText(/None of this skill's versions pass validation/),
    ).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });
  it("hides distribution controls without plugin write", () => {
    permissions.hasScope.mockReturnValue(false);
    render(
      <TooltipProvider>
        <MemoryRouter>
          <SkillPluginBanner
            skill={
              {
                id: "scoped-skill",
                hasValidVersion: true,
                versionCount: 1,
              } as Skill
            }
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    expect(screen.queryByRole("button", { name: "Example plugin" })).toBeNull();
    expect(permissions.hasScope).toHaveBeenCalledWith(
      "plugin:write",
      "project-a",
    );
    expect(
      screen.getByRole("link", { name: "View Example plugin" }),
    ).toBeTruthy();
  });

  it("uses narrow discovery and exposes a link preserving the authorized skill", () => {
    render(
      <TooltipProvider>
        <MemoryRouter>
          <SkillPluginBanner
            skill={
              {
                id: "scoped-skill",
                hasValidVersion: true,
                versionCount: 1,
              } as Skill
            }
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Example plugin" }));
    expect(screen.getByRole("checkbox")).toBeTruthy();
    expect(discovery).toHaveBeenCalledWith(
      { skillId: "scoped-skill" },
      undefined,
      { throwOnError: false },
    );
    expect(
      screen
        .getByRole("link", { name: "View Example plugin" })
        .getAttribute("href"),
    ).toBe("/plugins/plugin-a?skillId=scoped-skill");
  });
});
