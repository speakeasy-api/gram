import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { IdentityProviders } from "./IdentityProviders";

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

vi.mock("./useEnterpriseIssuer", () => ({
  useEnterpriseIssuer: () => ({
    data: {
      keys: [],
      kmsCleanupPending: 0,
      state: "setup_required",
      canEnable: true,
    },
    isLoading: false,
    isError: false,
    isFetching: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("./SpeakeasyWorkspace", () => ({
  SpeakeasyWorkspace: () => <div>Speakeasy page</div>,
}));
vi.mock("./services/EmaServices", () => ({
  EmaServices: () => <div>Services table</div>,
}));
vi.mock("./services/EmaServiceWorkspace", () => ({
  EmaServiceWorkspace: ({ vendorIssuer }: { vendorIssuer: string }) => (
    <div>Service page: {vendorIssuer}</div>
  ),
}));
function show(
  query = "",
  props = { showExternal: true, showSpeakeasy: false },
) {
  render(
    <MemoryRouter
      initialEntries={[`/example/identity?tab=identity-providers${query}`]}
    >
      <IdentityProviders {...props} />
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

describe("Identity providers", () => {
  it("offers supported integrations rather than a global provider selection", () => {
    show();
    expect(
      screen.getByRole("heading", { name: "Identity providers" }),
    ).toBeTruthy();
    expect(screen.getByText(/live on the Single sign-on tab/)).toBeTruthy();
    expect(screen.getByText("Not connected")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Connect Okta" }).getAttribute("href"),
    ).toBe("/example/identity?tab=identity-providers&provider=okta&view=setup");
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
      screen.getByRole("heading", { name: "Identity providers" }),
    ).toBeTruthy();
  });
  it("lists the services identity chaining reaches below the providers", () => {
    show();
    expect(screen.getByText("Services table")).toBeTruthy();
  });
  it("opens one service from its link", () => {
    show("&service=https%3A%2F%2Flinear.example.com");
    expect(
      screen.getByText("Service page: https://linear.example.com"),
    ).toBeTruthy();
    expect(screen.queryByText("Services table")).toBeNull();
  });
  it("asks which page to open when a link names both a provider and a service", () => {
    show("&provider=okta&service=https%3A%2F%2Flinear.example.com");
    expect(screen.queryByText(/Service page:/)).toBeNull();
    expect(
      screen.getByText(/points at both an identity provider and a service/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "open the service" })
        .getAttribute("href"),
    ).toBe(
      "/example/identity?tab=identity-providers&service=https%3A%2F%2Flinear.example.com",
    );
    expect(
      screen
        .getByRole("link", { name: "Open the identity provider" })
        .getAttribute("href"),
    ).toBe("/example/identity?tab=identity-providers&provider=okta&view=setup");
  });
  it("offers only the service when the named provider is hidden by its rollout", () => {
    show("&provider=okta&service=https%3A%2F%2Flinear.example.com", {
      showExternal: false,
      showSpeakeasy: true,
    });
    expect(
      screen.queryByRole("link", { name: "Open the identity provider" }),
    ).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Open the service" })
        .getAttribute("href"),
    ).toBe(
      "/example/identity?tab=identity-providers&service=https%3A%2F%2Flinear.example.com",
    );
  });
  it("shows the provider list for unknown providers", () => {
    show("&provider=unsupported");
    expect(
      screen.getByRole("heading", { name: "Identity providers" }),
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

describe("provider cards", () => {
  it("lists Speakeasy beside external providers", () => {
    show("", { showExternal: true, showSpeakeasy: true });
    expect(
      screen.getByRole("article", { name: "Speakeasy identity provider" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: /Okta/ })).toBeTruthy();
  });

  it("lists only Speakeasy without the external-provider rollout", () => {
    show("&provider=okta", { showExternal: false, showSpeakeasy: true });
    expect(
      screen.getByRole("article", { name: "Speakeasy identity provider" }),
    ).toBeTruthy();
    expect(screen.queryByText(/Okta content/)).toBeNull();
    expect(screen.queryByRole("link", { name: /Okta/ })).toBeNull();
  });

  it("opens the Speakeasy page from its provider link", () => {
    show("", { showExternal: false, showSpeakeasy: true });
    fireEvent.click(screen.getByRole("link", { name: "Set up Speakeasy" }));
    expect(screen.getByText("Speakeasy page")).toBeTruthy();
    expect(
      screen.queryByRole("heading", { name: "Identity providers" }),
    ).toBeNull();
  });

  it("does not open the Speakeasy page when it is not offered", () => {
    show("&provider=speakeasy", { showExternal: true, showSpeakeasy: false });
    expect(screen.queryByText("Speakeasy page")).toBeNull();
    expect(
      screen.getByRole("heading", { name: "Identity providers" }),
    ).toBeTruthy();
  });
});
