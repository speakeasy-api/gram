import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ displayName: "Sagar", email: "s@x.dev", photoUrl: "" }),
  useIsAdmin: () => false,
  useSession: () => ({ organizations: [{ id: "o1" }] }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "proj" }),
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ settings: { goTo: vi.fn() } }),
  useOrgRoutes: () => ({
    billing: { goTo: vi.fn() },
    adminSettings: { goTo: vi.fn() },
  }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
}));
vi.mock("@speakeasy-api/moonshine", async (orig) => ({
  ...(await orig<typeof import("@speakeasy-api/moonshine")>()),
  ThemeSwitcher: () => <div data-testid="theme-switcher" />,
  // Radix DropdownMenu requires pointerDown+click to open in happy-dom.
  // Stub the full primitive family so content is always rendered and
  // fireEvent.click on the trigger is sufficient.
  DropdownMenu: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuItem: ({
    children,
    onClick,
    asChild,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    asChild?: boolean;
  }) =>
    asChild ? (
      <div>{children}</div>
    ) : (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
}));

import { SidebarUserMenu } from "./sidebar-user-menu";

afterEach(cleanup);

describe("SidebarUserMenu", () => {
  it("renders the inline theme switcher and the user name", () => {
    render(<SidebarUserMenu />);
    expect(screen.getByTestId("theme-switcher")).toBeTruthy();
    expect(screen.getAllByText("Sagar").length).toBeGreaterThan(0);
  });

  it("links Roadmap to roadmap.speakeasy.com and has no GitHub issues link", () => {
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const roadmap = screen.getByText("Roadmap").closest("a");
    expect(roadmap?.getAttribute("href")).toBe("https://roadmap.speakeasy.com");
    expect(screen.queryByText(/Bug or Feature Request/)).toBeNull();
  });
});
