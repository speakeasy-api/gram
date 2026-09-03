import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({
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
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    settings: { goTo: vi.fn() },
    exploreDemo: { goTo: vi.fn() },
  }),
  useOrgRoutes: () => ({ billing: { goTo: vi.fn() } }),
}));
vi.mock("react-router", () => ({ useNavigate: () => vi.fn() }));
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
  render(<SidebarUserMenu />);

  const trigger = screen.getByRole("button", { name: "Account menu" });
  trigger.focus();
  await user.keyboard("{ArrowDown}");

  const adminLink = await screen.findByRole("menuitem", {
    name: "Platform admin",
  });
  expect(document.activeElement).toBe(adminLink);
  expect(adminLink.className).toContain("focus-visible:ring-2");
  expect(adminLink.className).toContain("focus-visible:ring-ring");

  const activated = vi.fn((event: Event) => event.preventDefault());
  adminLink.addEventListener("click", activated);
  await user.keyboard("{Enter}");
  expect(activated).toHaveBeenCalledOnce();
});
