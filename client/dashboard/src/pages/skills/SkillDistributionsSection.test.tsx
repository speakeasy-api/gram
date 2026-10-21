import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { SkillDistributionsSection } from "./SkillDistributionsSection";

const state = vi.hoisted(() => ({
  distributions: [] as PluginSkillDistribution[],
  mutateAsync: vi.fn(),
}));

vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => true,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  invalidateAllSkillDistributions: vi.fn(),
  useSkillDistributionsInfinite: () => ({
    data: { pages: [{ result: { distributions: state.distributions } }] },
    isPending: false,
    hasNextPage: false,
  }),
}));
vi.mock("@gram/client/react-query/undistributeSkill.js", () => ({
  useUndistributeSkillMutation: () => ({
    mutateAsync: state.mutateAsync,
    isPending: false,
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

beforeEach(() => {
  state.mutateAsync.mockReset();
  state.distributions = [
    {
      channel: "plugin",
      createdAt: new Date(0),
      updatedAt: new Date(0),
      createdByUserId: "user-1",
      id: "distribution-1",
      pluginId: "plugin-1",
      pluginName: "Example plugin",
      projectId: "project-1",
      resolvedVersionId: "version-1",
      skillDisplayName: "Example skill",
      skillId: "skill-1",
      skillName: "example-skill",
    },
  ];
});
afterEach(cleanup);

function show() {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <SkillDistributionsSection skillId="skill-1" />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

it("keeps unknown channels visible without submitting a plugin removal", () => {
  state.distributions[0]!.channel = unrecognized("future_channel");
  show();
  expect(screen.getByText(/unsupported distribution channel/)).toBeTruthy();
  const remove = screen.getByRole("button", {
    name: "Remove from Example plugin",
  });
  expect(remove.hasAttribute("disabled")).toBe(true);
  fireEvent.click(remove);
  expect(state.mutateAsync).not.toHaveBeenCalled();
});

it("still removes a supported plugin distribution", async () => {
  show();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove from Example plugin" }),
  );
  await waitFor(() =>
    expect(state.mutateAsync).toHaveBeenCalledWith({
      request: {
        undistributeSkillRequestBody: { id: "skill-1", pluginId: "plugin-1" },
      },
    }),
  );
});
