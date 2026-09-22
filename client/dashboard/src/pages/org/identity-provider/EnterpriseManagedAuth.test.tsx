import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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
  admin: true,
}));
vi.mock("nuqs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("nuqs")>()),
  useQueryState: (await import("./nuqsRouterMock")).useRouterQueryState,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) =>
    mocks.admin ? children : null,
}));
vi.mock("@gram/client/react-query/identityProviderConnection.js", () => ({
  useIdentityProviderConnection: () => mocks.query,
}));
vi.mock("./tabs/setup/OktaConnectionTab", () => ({
  OktaConnectionTab: () => <div>Okta content: setup</div>,
}));
vi.mock("./tabs/applications/ApplicationsTab", () => ({
  ApplicationsTab: () => <div>Okta content: applications</div>,
}));
vi.mock("./tabs/cross-app-access/CrossAppAccessTab", () => ({
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
  it("navigates between the overview and provider workspace", () => {
    mocks.query.data.connection = {
      status: "pending",
      orgUrl: "https://example.okta.com",
    };
    show();
    fireEvent.click(screen.getByRole("link", { name: "Manage Okta" }));
    expect(screen.getByText("Okta content: setup")).toBeTruthy();
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
  it("shows the provider list for unknown providers", () => {
    show("&provider=unsupported");
    expect(
      screen.getByRole("heading", { name: "Enterprise Managed Auth" }),
    ).toBeTruthy();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  });
  it("does not render an Okta workspace for nonadmins", () => {
    mocks.admin = false;
    show("&provider=okta&view=applications");
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  });
  it("does not render provider data for nonadmins", () => {
    mocks.admin = false;
    show();
    expect(screen.queryByRole("heading")).toBeNull();
  });
});
