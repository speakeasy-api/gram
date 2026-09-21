import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it, vi } from "vitest";
const forbidden = vi.hoisted(() =>
  vi.fn(() => {
    throw new Error("Privileged sidebar query mounted");
  }),
);
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ isLoading: false, hasScope: () => false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      href: () => "/skills",
      detail: { href: (id: string) => `/skills/${id}` },
    },
    plugins: {
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
vi.mock("@gram/client/react-query/plugin", () => ({ usePlugin: forbidden }));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: forbidden,
}));
vi.mock("@gram/client/react-query/audiences", () => ({
  useAudiences: forbidden,
}));
vi.mock("@gram/client/react-query/roles", () => ({ useRoles: forbidden }));
vi.mock("@gram/client/react-query/members", () => ({ useMembers: forbidden }));
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
    expect(forbidden).not.toHaveBeenCalled();
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
