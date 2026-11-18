import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  restoreLocation,
  stubLocationReplace,
} from "@/lib/stub-location-replace";

const orgSlug = vi.hoisted(() => ({ current: "acme" }));
const isPlatformAdmin = vi.hoisted(() => vi.fn(() => true));
const logout = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
const hasAnyScope = vi.hoisted(() =>
  vi.fn((): ((scopes: string[]) => boolean) => () => true),
);

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({
    id: "user_demo_sidebar",
    displayName: "Example User",
    email: "user@example.invalid",
    photoUrl: "",
  }),
  useSession: () => ({ organizations: [{ id: "o1" }] }),
  useOrganization: () => ({ slug: orgSlug.current }),
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "proj" }),
  useSdkClient: () => ({ auth: { logout } }),
  useProjectSlugForRequests: () => "proj",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: hasAnyScope(), isLoading: false }),
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

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderMenu() {
  return render(
    <MemoryRouter>
      <SidebarUserMenu />
      <LocationProbe />
    </MemoryRouter>,
  );
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
  hasAnyScope.mockReset();
  hasAnyScope.mockReturnValue(() => true);
  logout.mockReset().mockResolvedValue(undefined);
  restoreLocation();
});

describe("SidebarUserMenu", () => {
  it("renders the inline theme switcher and the user name", () => {
    renderMenu();
    expect(screen.getByTestId("theme-switcher")).toBeTruthy();
    expect(screen.getByText("Example User")).toBeTruthy();
  });

  it("links the account name to the signed-in user's identity page", () => {
    renderMenu();

    expect(
      screen
        .getByRole("link", { name: "Example User user@example.invalid" })
        .getAttribute("href"),
    ).toBe("/acme/projects/proj/identities/user%3Auser_demo_sidebar/overview");
    expect(screen.queryByText("View user profile")).toBeNull();
  });

  it("keeps the account name as plain text without org:read", () => {
    hasAnyScope.mockReturnValue(
      (scopes: string[]) => !scopes.includes("org:read"),
    );
    renderMenu();

    expect(
      screen.queryByRole("link", { name: "Example User user@example.invalid" }),
    ).toBeNull();
    expect(screen.getByText("Example User")).toBeTruthy();
  });

  it("links the crown icon to Platform admin in a new tab", () => {
    configureAdminServerUrl();
    renderMenu();
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

    renderMenu();

    expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
  });

  it("hides the Platform admin link when its URL is not configured", () => {
    renderMenu();

    expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
  });

  it.each(["not a URL", "javascript:alert(1)", "http://admin.example.invalid"])(
    "hides the Platform admin link for unsafe URL %s",
    (url) => {
      configureAdminServerUrl(url);
      renderMenu();

      expect(screen.queryByRole("link", { name: "Platform admin" })).toBeNull();
    },
  );

  it.each(["localhost", "127.0.0.1", "[::1]"])(
    "allows HTTP for the development loopback host %s",
    (host) => {
      configureAdminServerUrl(`http://${host}:8080`);
      renderMenu();

      expect(
        screen
          .getByRole("link", { name: "Platform admin" })
          .getAttribute("href"),
      ).toBe(`http://${host}:8080`);
    },
  );

  it("links Roadmap to roadmap.speakeasy.com and has no GitHub issues link", () => {
    renderMenu();
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const roadmap = screen.getByText("Roadmap").closest("a");
    expect(roadmap?.getAttribute("href")).toBe("https://roadmap.speakeasy.com");
    expect(screen.queryByText(/Bug or Feature Request/)).toBeNull();
  });

  it("links Platform Status to status.speakeasy.com in a new tab", () => {
    renderMenu();
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const status = screen.getByText("Platform Status").closest("a");
    expect(status?.getAttribute("href")).toBe("https://status.speakeasy.com/");
    expect(status?.getAttribute("target")).toBe("_blank");
    expect(status?.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("labels the support item Get Support, then Close Support while the chat is open", () => {
    installMockPylon();
    renderMenu();

    expect(screen.getByText("Get Support")).toBeTruthy();

    fireEvent.click(screen.getByText("Get Support"));
    expect(screen.getByText("Close Support")).toBeTruthy();
    expect(screen.queryByText("Get Support")).toBeNull();
  });

  it("returns the support item to Get Support when the chat window is hidden", () => {
    const pylon = installMockPylon();
    renderMenu();

    fireEvent.click(screen.getByText("Get Support"));
    expect(screen.getByText("Close Support")).toBeTruthy();

    act(() => {
      pylon.emitHide();
    });

    expect(screen.getByText("Get Support")).toBeTruthy();
    expect(screen.queryByText("Close Support")).toBeNull();
  });

  it("always offers Explore demo org outside the demo org", () => {
    renderMenu();
    fireEvent.click(screen.getByTestId("user-menu-trigger"));

    fireEvent.click(screen.getByText("Explore demo org"));
    expect(screen.getByTestId("location").textContent).toBe("/explore-demo");
  });

  it("logs out and leaves the page when Log out is clicked", async () => {
    const replace = stubLocationReplace();

    renderMenu();
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    fireEvent.click(screen.getByText("Log out"));

    await vi.waitFor(() => {
      expect(logout).toHaveBeenCalledOnce();
      expect(replace).toHaveBeenCalledWith("/login");
    });
  });

  it("still leaves the page when logout rejects", async () => {
    const error = new Error("network");
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    try {
      logout.mockRejectedValueOnce(error);
      const replace = stubLocationReplace();

      renderMenu();
      fireEvent.click(screen.getByTestId("user-menu-trigger"));
      fireEvent.click(screen.getByText("Log out"));

      await vi.waitFor(() => {
        expect(replace).toHaveBeenCalledExactlyOnceWith("/login");
      });
      expect(logout).toHaveBeenCalledOnce();
      expect(consoleError).toHaveBeenCalledExactlyOnceWith(
        "logoutToLogin: logout did not complete",
        error,
      );
    } finally {
      consoleError.mockRestore();
    }
  });

  it("hides Explore demo org while already in the demo org", () => {
    orgSlug.current = DEMO_ORG_SLUG;
    renderMenu();
    fireEvent.click(screen.getByTestId("user-menu-trigger"));

    expect(screen.queryByText("Explore demo org")).toBeNull();
  });
});
