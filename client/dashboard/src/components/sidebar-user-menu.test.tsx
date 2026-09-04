import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const orgSlug = vi.hoisted(() => ({ current: "acme" }));
const isPlatformAdmin = vi.hoisted(() => vi.fn(() => true));
const exploreDemoGoTo = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ displayName: "Sagar", email: "s@x.dev", photoUrl: "" }),
  useSession: () => ({ organizations: [{ id: "o1" }] }),
  useOrganization: () => ({ slug: orgSlug.current }),
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "proj" }),
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    settings: { goTo: vi.fn() },
    exploreDemo: { goTo: exploreDemoGoTo },
  }),
  useOrgRoutes: () => ({
    billing: { goTo: vi.fn() },
  }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
}));
vi.mock("@/components/ui/Dropdown", () => ({
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

vi.mock("@/components/ui/ThemeSwitcher", () => ({
  ThemeSwitcher: () => <div data-testid="theme-switcher" />,
}));

import { DEMO_ORG_SLUG } from "@/lib/demo";
import { isPylonChatOpen, togglePylonChat } from "@/lib/pylon";
import { installMockPylon } from "@/lib/pylon-test-mock";

import { SidebarUserMenu } from "./sidebar-user-menu";

function configureAdminServerUrl(url = "https://admin.example.invalid"): void {
  const meta = document.createElement("meta");
  meta.name = "gram-admin-server-url";
  meta.content = url;
  document.head.append(meta);
}

afterEach(() => {
  if (isPylonChatOpen()) {
    togglePylonChat();
  }
  cleanup();
  document.querySelector('meta[name="gram-admin-server-url"]')?.remove();
  Reflect.deleteProperty(window, "Pylon");
  orgSlug.current = "acme";
  isPlatformAdmin.mockReset();
  isPlatformAdmin.mockReturnValue(true);
  exploreDemoGoTo.mockReset();
});

describe("SidebarUserMenu", () => {
  it("renders the inline theme switcher and the user name", () => {
    render(<SidebarUserMenu />);
    expect(screen.getByTestId("theme-switcher")).toBeTruthy();
    expect(screen.getAllByText("Sagar").length).toBeGreaterThan(0);
  });

  it("links the crown icon to Platform admin in a new tab", () => {
    configureAdminServerUrl();
    render(<SidebarUserMenu />);
    const platformAdmin = screen.getByRole("link", { name: "Platform admin" });

    expect(platformAdmin.getAttribute("href")).toBe(
      "https://admin.example.invalid",
    );
    expect(platformAdmin.getAttribute("target")).toBe("_blank");
    expect(platformAdmin.getAttribute("rel")).toBe("noopener noreferrer");
    expect(platformAdmin.querySelector(".lucide-crown")).toBeTruthy();
  });

  it("hides the Platform admin link from regular users", () => {
    configureAdminServerUrl();
    isPlatformAdmin.mockReturnValue(false);

    render(<SidebarUserMenu />);

    expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
  });

  it("hides the Platform admin link when its URL is not configured", () => {
    render(<SidebarUserMenu />);

    expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
  });

  it.each(["not a URL", "javascript:alert(1)", "http://admin.example.invalid"])(
    "hides the Platform admin link for unsafe URL %s",
    (url) => {
      configureAdminServerUrl(url);
      render(<SidebarUserMenu />);

      expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
    },
  );

  it.each(["localhost", "127.0.0.1", "[::1]"])(
    "allows HTTP for the development loopback host %s",
    (host) => {
      configureAdminServerUrl(`http://${host}:8080`);
      render(<SidebarUserMenu />);

      expect(
        screen
          .getByRole("link", { name: "Platform admin" })
          .getAttribute("href"),
      ).toBe(`http://${host}:8080`);
    },
  );

  it("links Roadmap to roadmap.speakeasy.com and has no GitHub issues link", () => {
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const roadmap = screen.getByText("Roadmap").closest("a");
    expect(roadmap?.getAttribute("href")).toBe("https://roadmap.speakeasy.com");
    expect(screen.queryByText(/Bug or Feature Request/)).toBeNull();
  });

  it("links Platform Status to status.speakeasy.com in a new tab", () => {
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const status = screen.getByText("Platform Status").closest("a");
    expect(status?.getAttribute("href")).toBe("https://status.speakeasy.com/");
    expect(status?.getAttribute("target")).toBe("_blank");
    expect(status?.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("labels the support item Get Support, then Close Support while the chat is open", () => {
    installMockPylon();
    render(<SidebarUserMenu />);

    expect(screen.getByText("Get Support")).toBeTruthy();

    fireEvent.click(screen.getByText("Get Support"));
    expect(screen.getByText("Close Support")).toBeTruthy();
    expect(screen.queryByText("Get Support")).toBeNull();
  });

  it("returns the support item to Get Support when the chat window is hidden", () => {
    const pylon = installMockPylon();
    render(<SidebarUserMenu />);

    fireEvent.click(screen.getByText("Get Support"));
    expect(screen.getByText("Close Support")).toBeTruthy();

    act(() => {
      pylon.emitHide();
    });

    expect(screen.getByText("Get Support")).toBeTruthy();
    expect(screen.queryByText("Close Support")).toBeNull();
  });

  it("always offers Explore demo org outside the demo org", () => {
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));

    fireEvent.click(screen.getByText("Explore demo org"));
    expect(exploreDemoGoTo).toHaveBeenCalledOnce();
  });

  it("hides Explore demo org while already in the demo org", () => {
    orgSlug.current = DEMO_ORG_SLUG;
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));

    expect(screen.queryByText("Explore demo org")).toBeNull();
  });
});
