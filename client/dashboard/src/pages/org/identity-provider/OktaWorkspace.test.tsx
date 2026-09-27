import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { OktaWorkspace } from "./OktaWorkspace";

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
}));
vi.mock("nuqs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("nuqs")>()),
  useQueryState: (await import("./nuqsRouterMock")).useRouterQueryState,
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

function show(view: string) {
  render(
    <MemoryRouter
      initialEntries={[
        `/example/identity?tab=enterprise-managed-auth&provider=okta&view=${view}`,
      ]}
    >
      <OktaWorkspace />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  mocks.query.data = { connection: undefined };
  mocks.query.error = null;
  mocks.query.isPending = false;
  mocks.query.refetch.mockClear();
});
afterEach(cleanup);

describe("Okta workspace", () => {
  it.each([
    ["setup", "setup"],
    ["applications", "applications"],
    ["cross-app-access", "cross-app-access"],
    ["unknown", "setup"],
  ])("opens the %s Okta view", (view, tab) => {
    mocks.query.data.connection = {
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show(view);
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
  it("activates Okta views using ArrowRight and Space", async () => {
    mocks.query.data.connection = {
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show("setup");
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
      show(view);
      expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
      expect(
        screen.getByRole("link", { name: "All identity providers" }),
      ).toBeTruthy();
      expect(screen.getByText("Okta content: setup")).toBeTruthy();
      expect(screen.queryByRole("tablist")).toBeNull();
      expect(screen.queryByText(/Manage how your AI agents/)).toBeNull();
    },
  );
  it("treats revoked connections as unconnected for deep links", () => {
    mocks.query.data.connection = {
      status: "revoked",
      orgUrl: "https://example.okta.com",
    };
    show("cross-app-access");
    expect(screen.getByText("Okta content: setup")).toBeTruthy();
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
      show("applications");
      expect(screen.getAllByRole("tab")).toHaveLength(3);
      expect(screen.getByText("Okta content: applications")).toBeTruthy();
      expect(screen.getByText(/Manage how your AI agents/)).toBeTruthy();
    },
  );
  it("hides workspace navigation while the connection loads", () => {
    mocks.query.isPending = true;
    show("applications");
    expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "All identity providers" }),
    ).toBeTruthy();
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.queryByText(/Manage how your AI agents/)).toBeNull();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  });
  it("shows a failed load inline without tabs", () => {
    mocks.query.data = undefined as never;
    mocks.query.error = new Error("Could not load Okta");
    show("applications");
    expect(screen.getByRole("alert").textContent).toBe("Could not load Okta");
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  });
});
