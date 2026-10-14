import { TooltipProvider } from "@/components/ui/Tooltip";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  flag: { status: "enabled" } as FeatureFlagResult,
  grants: [] as Array<{
    scope: string;
    selectors: Array<Record<string, string>>;
  }>,
  plugins: {
    data: undefined as unknown,
    error: null as unknown,
    isLoading: false,
  },
  distributions: vi.fn(),
  mutate: vi.fn(),
  picker: vi.fn(),
}));

vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  );
  return {
    Page: {
      Section: Object.assign(Section, {
        Title: Section,
        Description: Section,
        Body: Section,
        CTA: Section,
      }),
    },
  };
});
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => state.flag,
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
    isLoading: false,
    hasAnyScope: () => true,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
    plugins: Object.assign(
      {
        href: () => "/plugins",
        Link: ({ children }: { children: ReactNode }) => (
          <a href="/plugins">{children}</a>
        ),
      },
      { detail: { href: (id: string) => `/plugins/${id}` } },
    ),
    mcp: {
      details: {
        overview: { href: (slug: string) => `/mcp/${slug}/overview` },
      },
    },
  }),
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: () => undefined,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: () => state.plugins,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  useSkillDistributionsInfinite: state.distributions,
  invalidateAllSkillDistributions: vi.fn(),
}));
vi.mock("@gram/client/react-query/undistributeSkill.js", () => ({
  useUndistributeSkillMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));
vi.mock("@/pages/skills/SkillPickerDialog", () => ({
  SkillPickerDialog: (props: { target: { pluginId?: string } }) => {
    state.picker(props);
    return <div data-testid="picker">{props.target.pluginId}</div>;
  },
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate">{to}</div>,
}));

import { MCPSkillsTab, MCPSkillsTabRoute } from "./MCPSkillsTab";

const toolset = { id: "toolset-a", slug: "toolset-a", name: "Billing" };

const hostedPlugin = {
  id: "plugin-a",
  name: "Finance tools",
  assignmentCount: 2,
  servers: [{ id: "ps-1", displayName: "Billing", toolsetId: "toolset-a" }],
};
const wrapperPlugin = {
  id: "plugin-b",
  name: "Everything",
  assignmentCount: 0,
  servers: [{ id: "ps-2", displayName: "Billing", mcpServerId: "toolset-a" }],
};
const otherPlugin = {
  id: "plugin-c",
  name: "Unrelated",
  assignmentCount: 1,
  servers: [{ id: "ps-3", displayName: "Other", toolsetId: "toolset-z" }],
};

function distribution(
  id: string,
  pluginId: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    id,
    skillId: `skill-${id}`,
    pluginId,
    pluginName: "Plugin",
    skillDisplayName: `Skill ${id}`,
    skillName: `skill-${id}`,
    ...overrides,
  };
}

function renderTab(ui: ReactNode = <MCPSkillsTab toolset={toolset} />) {
  return render(
    <TooltipProvider>
      <MemoryRouter>{ui}</MemoryRouter>
    </TooltipProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  state.flag = { status: "enabled" };
  state.grants = [
    { scope: "skill:read", selectors: [{ resourceId: "project-a" }] },
  ];
  state.plugins = {
    data: { plugins: [hostedPlugin, wrapperPlugin, otherPlugin] },
    error: null,
    isLoading: false,
  };
  state.mutate.mockReset();
  state.picker.mockReset();
  state.distributions.mockReset().mockReturnValue({
    data: {
      pages: [
        {
          result: {
            distributions: [
              distribution("1", "plugin-a", {
                lastLoadedOverMcpAt: new Date(Date.now() - 5 * 60_000),
              }),
              distribution("2", "plugin-b"),
              distribution("3", "plugin-c"),
            ],
          },
        },
      ],
    },
    hasNextPage: false,
    error: null,
  });
});

describe("MCPSkillsTab", () => {
  it("lists skills under the plugins that include this server and skips the rest", () => {
    renderTab();
    expect(screen.getByText("Skill 1")).toBeDefined();
    expect(screen.getByText("Skill 2")).toBeDefined();
    expect(screen.queryByText("Skill 3")).toBeNull();
    expect(screen.getByText("Finance tools")).toBeDefined();
    expect(screen.getByText("Everything")).toBeDefined();
    expect(screen.queryByText("Unrelated")).toBeNull();
    // The distributions are the project's, grouped here, not per plugin.
    expect(state.distributions).toHaveBeenCalledWith(
      expect.objectContaining({ gramProject: "project-a", limit: 50 }),
      undefined,
      { throwOnError: false },
    );
  });

  it("shows when a skill was last loaded over MCP and warns about unassigned plugins", () => {
    renderTab();
    expect(screen.getAllByText("Never")).toHaveLength(1);
    expect(screen.getByText(/5 minutes ago/)).toBeDefined();
    expect(screen.getByText(/Not assigned to anyone yet/)).toBeDefined();
  });

  it("hides add and remove controls without plugin write", () => {
    renderTab();
    expect(screen.queryByRole("button", { name: /Add skill/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Remove/ })).toBeNull();
  });

  it("removes a served skill by undistributing it from its plugin", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceKind: "project", resourceId: "project-a" }],
    });
    renderTab();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Skill 1 from Plugin" }),
    );
    expect(state.mutate).toHaveBeenCalledWith(
      {
        request: {
          undistributeSkillRequestBody: {
            id: "skill-1",
            pluginId: "plugin-a",
          },
        },
      },
      expect.any(Object),
    );
  });

  it("opens the picker for the only plugin that includes the server", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceKind: "project", resourceId: "project-a" }],
    });
    state.plugins = {
      data: { plugins: [hostedPlugin, otherPlugin] },
      error: null,
      isLoading: false,
    };
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(screen.getByTestId("picker").textContent).toBe("plugin-a");
    expect(state.picker).toHaveBeenLastCalledWith(
      expect.objectContaining({
        excludedSkillIds: ["skill-1"],
        target: { pluginId: "plugin-a" },
      }),
    );
  });

  it("points at the plugins page when no plugin includes the server", () => {
    state.plugins = {
      data: { plugins: [otherPlugin] },
      error: null,
      isLoading: false,
    };
    renderTab();
    expect(
      screen.getByText("No plugin includes this server yet"),
    ).toBeDefined();
    expect(
      screen.getByText("Go to plugins").closest("a")?.getAttribute("href"),
    ).toBe("/plugins");
  });

  it("names the plugin to distribute to when nothing is served yet", () => {
    state.plugins = {
      data: { plugins: [hostedPlugin] },
      error: null,
      isLoading: false,
    };
    state.distributions.mockReturnValue({
      data: { pages: [{ result: { distributions: [] } }] },
      hasNextPage: false,
      error: null,
    });
    renderTab();
    expect(screen.getByText("No skills served yet")).toBeDefined();
    expect(
      screen.getByText(/Distribute a skill to Finance tools/),
    ).toBeDefined();
  });

  it("redirects to the overview while the flag is off", () => {
    state.flag = { status: "disabled" };
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/mcp/toolset-a/overview",
    );
  });
});
