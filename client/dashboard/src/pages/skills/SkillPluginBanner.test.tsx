import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
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
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
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
            distributions: [
              {
                id: "distribution-a",
                pluginId: "plugin-a",
                pluginName: "Example plugin",
              },
            ],
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
describe("SkillPluginBanner", () => {
  it("uses narrow discovery and exposes a link preserving the authorized skill", () => {
    render(
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
      </MemoryRouter>,
    );
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
