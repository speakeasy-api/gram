import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  canRead: false,
  canAdmin: false,
  plugin: vi.fn(),
  publishStatus: vi.fn(),
  audiences: vi.fn(),
  roles: vi.fn(),
  members: vi.fn(),
}));
const queries = [
  state.plugin,
  state.publishStatus,
  state.audiences,
  state.roles,
  state.members,
];
afterEach(cleanup);
beforeEach(() => {
  state.canRead = false;
  state.canAdmin = false;
  for (const query of queries) {
    query.mockReset().mockImplementation(() => {
      throw new Error("Privileged sidebar query mounted");
    });
  }
});
vi.mock("@/pages/plugins/use-plugin-assignments-visible", () => ({
  usePluginAssignmentsVisible: () => true,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: false,
    hasScope: (scope: string) =>
      scope === "org:read" ? state.canRead : state.canAdmin,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      href: () => "/skills",
      detail: { href: (id: string) => `/skills/${id}` },
    },
    plugins: {
      href: () => "/plugins",
      detail: {
        href: (id: string) => `/plugins/${id}`,
        skills: { href: (id: string) => `/plugins/${id}/skills` },
      },
    },
  }),
}));
vi.mock("@/pages/plugins/plugin-detail-sections", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/pages/plugins/plugin-detail-sections")
  >()),
  pluginSectionHref: (_routes: unknown, id: string, section: string) =>
    `/plugins/${id}/${section}`,
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarInfoLabel: () => null,
  DetailSidebarNav: ({
    backHref,
    backLabel,
    items,
  }: {
    backHref: string;
    backLabel: string;
    items: Array<{ title: string; href: string }>;
  }) => (
    <nav>
      <a href={backHref}>{backLabel}</a>
      {items.map((item) => (
        <a key={item.title} href={item.href}>
          {item.title}
        </a>
      ))}
    </nav>
  ),
}));
vi.mock("@gram/client/react-query/plugin", () => ({ usePlugin: state.plugin }));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: state.publishStatus,
}));
vi.mock("@gram/client/react-query/audiences", () => ({
  useAudiences: state.audiences,
}));
vi.mock("@gram/client/react-query/roles", () => ({ useRoles: state.roles }));
vi.mock("@gram/client/react-query/members", () => ({
  useMembers: state.members,
}));
import { PluginDetailSidebarNav } from "./plugin-detail-sidebar-nav";
describe("skill-only plugin sidebar", () => {
  it("mounts no org queries and preserves the concrete skill in navigation", () => {
    render(
      <MemoryRouter initialEntries={["/plugins/plugin-a?skillId=skill-a"]}>
        <Routes>
          <Route
            path="/plugins/:pluginId"
            element={<PluginDetailSidebarNav />}
          />
        </Routes>
      </MemoryRouter>,
    );
    for (const query of queries) expect(query).not.toHaveBeenCalled();
    expect(
      screen.getByRole("link", { name: "Back to skill" }).getAttribute("href"),
    ).toBe("/skills/skill-a");
    expect(
      screen.getByRole("link", { name: "Skills" }).getAttribute("href"),
    ).toContain("?skillId=skill-a");
    expect(screen.queryByText("Settings")).toBeNull();
    expect(screen.queryByText("Assignments")).toBeNull();
  });
});

describe("organization plugin sidebar", () => {
  it.each([false, true])(
    "preserves organization navigation with admin=%s",
    (canAdmin) => {
      state.canRead = true;
      state.canAdmin = canAdmin;
      for (const query of queries) query.mockReturnValue({ data: undefined });
      render(
        <MemoryRouter initialEntries={["/plugins/plugin-a"]}>
          <Routes>
            <Route
              path="/plugins/:pluginId"
              element={<PluginDetailSidebarNav />}
            />
          </Routes>
        </MemoryRouter>,
      );
      expect(
        screen
          .getByRole("link", { name: "Back to all plugins" })
          .getAttribute("href"),
      ).toBe("/plugins");
      for (const title of ["Overview", "MCP Servers", "Skills"]) {
        expect(screen.getByRole("link", { name: title })).toBeTruthy();
      }
      for (const title of ["Settings", "Assignments"]) {
        expect(screen.queryByRole("link", { name: title }) !== null).toBe(
          canAdmin,
        );
      }
      expect(state.plugin).toHaveBeenCalledWith({ id: "plugin-a" }, undefined, {
        throwOnError: false,
        enabled: true,
      });
      expect(state.publishStatus).toHaveBeenCalled();
      for (const query of [state.members, state.roles, state.audiences]) {
        expect(query).toHaveBeenCalledWith(undefined, undefined, {
          enabled: canAdmin,
        });
      }
    },
  );
});
