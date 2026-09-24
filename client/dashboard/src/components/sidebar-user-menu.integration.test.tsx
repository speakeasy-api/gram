import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({
    id: "user_admin",
    displayName: "Admin User",
    email: "admin@example.invalid",
  }),
  useSession: () => ({ organizations: [{ id: "org" }] }),
  useOrganization: () => ({ slug: "org" }),
  useIsPlatformAdmin: () => true,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "project" }),
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
  useProjectSlugForRequests: () => "project",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true, isLoading: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    settings: { goTo: vi.fn() },
    exploreDemo: { goTo: vi.fn() },
    identities: {
      detail: { overview: { href: (urn: string) => `/identities/${urn}` } },
    },
  }),
  useOrgRoutes: () => ({ billing: { goTo: vi.fn() } }),
}));
vi.mock("@/components/ui/ThemeSwitcher", () => ({ ThemeSwitcher: () => null }));

import { SidebarUserMenu } from "./sidebar-user-menu";

afterEach(() => {
  cleanup();
  document.querySelector('meta[name="gram-admin-server-url"]')?.remove();
});

it("keyboard navigation focuses and activates the real Platform admin menu item", async () => {
  const meta = document.createElement("meta");
  meta.name = "gram-admin-server-url";
  meta.content = "https://admin.example.invalid";
  document.head.append(meta);
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <SidebarUserMenu />
    </MemoryRouter>,
  );

  const trigger = screen.getByRole("button", { name: "Account menu" });
  trigger.focus();
  await user.keyboard("{ArrowDown}");

  const adminLink = await screen.findByRole("menuitem", {
    name: "Platform admin",
  });
  await user.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(adminLink);
  expect(adminLink.getAttribute("href")).toBe("https://admin.example.invalid");
  expect(adminLink.className).toContain("focus-visible:ring-2");
  expect(adminLink.className).toContain("focus-visible:ring-ring");

  const activated = vi.fn((event: Event) => event.preventDefault());
  adminLink.addEventListener("click", activated);
  await user.keyboard("{Enter}");
  expect(activated).toHaveBeenCalledOnce();
});

it("opens the account name as the first keyboard-accessible profile link", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <SidebarUserMenu />
      <Routes>
        <Route path="/" element={null} />
        <Route
          path="/identities/:identity"
          element={<h1>Identity overview</h1>}
        />
      </Routes>
    </MemoryRouter>,
  );

  screen.getByRole("button", { name: "Account menu" }).focus();
  await user.keyboard("{ArrowDown}");

  const profileLink = await screen.findByRole("menuitem", {
    name: "Admin User admin@example.invalid",
  });
  expect(document.activeElement).toBe(profileLink);
  expect(profileLink.getAttribute("href")).toBe(
    "/identities/user%3Auser_admin",
  );

  await user.keyboard("{Enter}");
  expect(
    await screen.findByRole("heading", { name: "Identity overview" }),
  ).toBeTruthy();
  expect(screen.queryByRole("menu")).toBeNull();
});
