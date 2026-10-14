import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LegacyCallbackAlert } from "./clientAlerts";

const rbac = vi.hoisted(() => ({
  canReadOrg: true,
  requested: [] as string[][],
  isPlatformAdmin: true,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        settings: { href: (id: string) => `/providers/${id}/settings` },
      },
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: string[]) => {
      rbac.requested.push(scopes);
      return rbac.canReadOrg;
    },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => rbac.isPlatformAdmin,
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

afterEach(() => {
  cleanup();
  rbac.canReadOrg = true;
  rbac.requested = [];
  rbac.isPlatformAdmin = true;
});

describe("LegacyCallbackAlert", () => {
  it("renders nothing for a current client or a non-admin", () => {
    const { rerender, container } = render(
      <LegacyCallbackAlert
        legacyCallbackUrl={false}
        callbackUrl="https://app.example.com/mcp/remote_login_callback"
        onMigrate={vi.fn<() => void>()}
        canMigrate
      />,
    );
    expect(container.textContent).toBe("");

    rbac.isPlatformAdmin = false;
    rerender(
      <LegacyCallbackAlert
        legacyCallbackUrl
        callbackUrl="https://app.example.com/mcp/remote_login_callback"
        onMigrate={vi.fn<() => void>()}
        canMigrate
      />,
    );
    expect(container.textContent).toBe("");
  });

  it("hides Migrate from callers who cannot save", () => {
    render(
      <LegacyCallbackAlert
        legacyCallbackUrl
        callbackUrl="https://app.example.com/mcp/remote_login_callback"
        onMigrate={vi.fn<() => void>()}
        canMigrate={false}
      />,
    );

    expect(screen.getByText(/runs in compatibility mode/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Migrate" })).toBeNull();
  });

  it("names both callback URLs and migrates only after confirmation", () => {
    const onMigrate = vi.fn<() => void>();
    render(
      <LegacyCallbackAlert
        legacyCallbackUrl
        callbackUrl="https://app.example.com/mcp/remote_login_callback"
        onMigrate={onMigrate}
        canMigrate
      />,
    );

    expect(
      screen.getByText("https://app.example.com/oauth/callback"),
    ).toBeTruthy();
    expect(
      screen.getByText("https://app.example.com/mcp/remote_login_callback"),
    ).toBeTruthy();
    expect(screen.getByText(/runs in compatibility mode/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Migrate" }));
    expect(onMigrate).not.toHaveBeenCalled();

    const confirm = screen
      .getAllByRole("button", { name: "Migrate" })
      .find((button) => button.closest("[role='dialog']"));
    expect(confirm).toBeTruthy();
    expect(
      confirm!
        .closest("[role='dialog']")!
        .textContent?.includes(
          "https://app.example.com/mcp/remote_login_callback",
        ),
    ).toBe(true);
    fireEvent.click(confirm!);
    expect(onMigrate).toHaveBeenCalledTimes(1);
  });
});
