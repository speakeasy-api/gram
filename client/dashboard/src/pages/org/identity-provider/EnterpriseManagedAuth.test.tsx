import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { EnterpriseManagedAuth } from "./EnterpriseManagedAuth";

const mocks = vi.hoisted(() => ({
  query: {
    data: {
      connection: undefined as undefined | { status: string; orgUrl: string },
    },
    error: null as Error | null,
    isPending: false,
    isFetching: false,
    refetch: vi.fn(),
  },
  flag: { status: "enabled" },
  admin: true,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) =>
    mocks.admin ? children : null,
}));
vi.mock("@/hooks/useFeatureFlag", () => ({ useFeatureFlag: () => mocks.flag }));
vi.mock("@gram/client/react-query/identityProviderConnection.js", () => ({
  useIdentityProviderConnection: () => mocks.query,
}));
vi.mock("./IdentityProviderTab", () => ({
  IdentityProviderTab: ({ rolloutEnabled }: { rolloutEnabled: boolean }) => (
    <div>Okta content: provider{!rolloutEnabled && " (rollout disabled)"}</div>
  ),
}));
vi.mock("./ApplicationsTab", () => ({
  ApplicationsTab: () => <div>Okta content: applications</div>,
}));
vi.mock("./CrossAppAccessTab", () => ({
  CrossAppAccessTab: () => <div>Okta content: cross-app-access</div>,
}));
vi.mock("@/components/api-error-alert", () => ({
  ApiErrorAlert: ({ error }: { error: Error | null }) =>
    error ? <div role="alert">{error.message}</div> : null,
}));

function show(query = "") {
  render(
    <MemoryRouter
      initialEntries={[`/example/identity?tab=enterprise-managed-auth${query}`]}
    >
      <EnterpriseManagedAuth />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  mocks.admin = true;
  mocks.flag.status = "enabled";
  mocks.query.data = { connection: undefined };
  mocks.query.error = null;
  mocks.query.isPending = false;
  mocks.query.refetch.mockClear();
});
afterEach(cleanup);

describe("Enterprise Managed Auth", () => {
  it("offers supported integrations rather than a global provider selection", () => {
    show();
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Auth" }),
    ).toBeTruthy();
    expect(screen.getByText(/separate from employee sign-in/)).toBeTruthy();
    expect(screen.getByText("Not connected")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Connect Okta" }).getAttribute("href"),
    ).toBe(
      "/example/identity?tab=enterprise-managed-auth&provider=okta&view=setup",
    );
    expect(screen.queryByRole("radio")).toBeNull();
    expect(screen.queryByRole("tab", { name: "Applications" })).toBeNull();
  });
  it.each(["pending", "verified", "degraded"])(
    "offers management for a %s connection",
    (status) => {
      mocks.query.data.connection = {
        status,
        orgUrl: "https://example.okta.com",
      };
      show();
      expect(screen.getByRole("link", { name: "Manage Okta" })).toBeTruthy();
      expect(
        screen.queryByText(/selected provider|switch providers/),
      ).toBeNull();
    },
  );
  it("offers reconnection for revoked connections", () => {
    mocks.query.data.connection = {
      status: "revoked",
      orgUrl: "https://example.okta.com",
    };
    show();
    expect(screen.getByRole("link", { name: "Connect Okta" })).toBeTruthy();
  });
  it("gates new connections when rollout is disabled", () => {
    mocks.flag.status = "disabled";
    show();
    expect(screen.queryByRole("link", { name: "Connect Okta" })).toBeNull();
    expect(screen.getByText(/Okta setup is not enabled/)).toBeTruthy();
  });
  it("does not show a disconnected status while loading", () => {
    mocks.query.isPending = true;
    show();
    expect(screen.queryByText("Not connected")).toBeNull();
    expect(screen.queryByRole("link", { name: "Connect Okta" })).toBeNull();
  });
  it("lets administrators retry failed provider loads", () => {
    mocks.query.data = undefined as never;
    mocks.query.error = new Error("Could not load Okta");
    show();
    expect(screen.getByRole("alert").textContent).toBe("Could not load Okta");
    expect(screen.queryByText("Not connected")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry loading Okta" }));
    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });
  it.each([
    ["setup", "provider"],
    ["applications", "applications"],
    ["cross-app-access", "cross-app-access"],
    ["unknown", "provider"],
  ])("opens the %s Okta view", (view, tab) => {
    mocks.query.data.connection = {
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show(`&provider=okta&view=${view}`);
    expect(screen.getByText(`Okta content: ${tab}`)).toBeTruthy();
    expect(screen.getAllByRole("tab").map((el) => el.textContent)).toEqual([
      "Setup",
      "Applications",
      "Cross App Access",
    ]);
    expect(
      screen
        .getByRole("link", { name: "All identity providers" })
        .getAttribute("href"),
    ).toBe("/example/identity?tab=enterprise-managed-auth");
  });
  it("navigates between the overview and provider workspace", () => {
    mocks.query.data.connection = {
      status: "pending",
      orgUrl: "https://example.okta.com",
    };
    show();
    fireEvent.click(screen.getByRole("link", { name: "Manage Okta" }));
    expect(screen.getByText("Okta content: provider")).toBeTruthy();
    // Installed Radix Tabs activates pointer selection on mouseDown, not click.
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Applications" }), {
      button: 0,
      ctrlKey: false,
    });
    expect(
      screen
        .getByRole("tab", { name: "Applications" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.getByText("Okta content: applications")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("link", { name: "All identity providers" }),
    );
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Auth" }),
    ).toBeTruthy();
  });
  it("activates Okta views using ArrowRight and Space", async () => {
    mocks.query.data.connection = {
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show("&provider=okta&view=setup");
    const setup = screen.getByRole("tab", { name: "Setup" });
    setup.focus();
    fireEvent.keyDown(setup, { key: "ArrowRight" });
    await waitFor(() =>
      expect(
        screen
          .getByRole("tab", { name: "Applications" })
          .getAttribute("aria-selected"),
      ).toBe("true"),
    );
    expect(screen.getByText("Okta content: applications")).toBeTruthy();
    fireEvent.keyDown(screen.getByRole("tab", { name: "Cross App Access" }), {
      key: " ",
    });
    expect(
      screen
        .getByRole("tab", { name: "Cross App Access" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.getByText("Okta content: cross-app-access")).toBeTruthy();
  });
  it.each(["setup", "applications", "cross-app-access"])(
    "shows creation without workspace navigation for unconnected %s links",
    (view) => {
      show(`&provider=okta&view=${view}`);
      expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
      expect(
        screen.getByRole("link", { name: "All identity providers" }),
      ).toBeTruthy();
      expect(screen.getByText("Okta content: provider")).toBeTruthy();
      expect(screen.queryByRole("tablist")).toBeNull();
      expect(screen.queryByText(/Manage how your AI agents/)).toBeNull();
    },
  );
  it("treats revoked connections as unconnected for deep links", () => {
    mocks.query.data.connection = {
      status: "revoked",
      orgUrl: "https://example.okta.com",
    };
    show("&provider=okta&view=cross-app-access");
    expect(screen.getByText("Okta content: provider")).toBeTruthy();
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.queryByText(/Manage how your AI agents/)).toBeNull();
  });
  it.each(["pending", "verified"])(
    "keeps workspace tabs for %s connections",
    (status) => {
      mocks.query.data.connection = {
        status,
        orgUrl: "https://example.okta.com",
      };
      show("&provider=okta&view=applications");
      expect(screen.getAllByRole("tab")).toHaveLength(3);
      expect(screen.getByText("Okta content: applications")).toBeTruthy();
      expect(screen.getByText(/Manage how your AI agents/)).toBeTruthy();
    },
  );
  it.each(["connection", "flag"])(
    "hides workspace navigation while %s loads",
    (loading) => {
      mocks.query.isPending = loading === "connection";
      mocks.flag.status = loading === "flag" ? "loading" : "enabled";
      show("&provider=okta&view=applications");
      expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
      expect(
        screen.getByRole("link", { name: "All identity providers" }),
      ).toBeTruthy();
      expect(screen.queryByRole("tablist")).toBeNull();
      expect(screen.queryByText(/Manage how your AI agents/)).toBeNull();
      expect(screen.queryByText(/Okta content:/)).toBeNull();
    },
  );
  it("preserves rollout gates on unconnected deep links", () => {
    mocks.flag.status = "disabled";
    show("&provider=okta&view=applications");
    expect(
      screen.getByText("Okta content: provider (rollout disabled)"),
    ).toBeTruthy();
    expect(screen.queryByRole("tablist")).toBeNull();
  });
  it("does not render an Okta workspace for nonadmins", () => {
    mocks.admin = false;
    show("&provider=okta&view=applications");
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  });
  it("does not send unknown providers into Okta setup", () => {
    show("&provider=unsupported");
    expect(screen.getByText("Identity provider not supported")).toBeTruthy();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
    expect(
      screen.getByRole("link", { name: "View identity providers" }),
    ).toBeTruthy();
  });
  it("does not render provider data for nonadmins", () => {
    mocks.admin = false;
    show();
    expect(screen.queryByRole("heading")).toBeNull();
  });
});
