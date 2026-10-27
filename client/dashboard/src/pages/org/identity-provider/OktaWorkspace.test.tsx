import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { LiveConnection } from "./connectionView";
import { OktaProviderCard, OktaWorkspace } from "./OktaWorkspace";
import { makeChecklistItem, makeConnection } from "./tabs/setup/testFixtures";

const mocks = vi.hoisted(() => ({
  query: {
    data: {
      connection: undefined as
        | undefined
        | { provider: string; status: string; orgUrl: string },
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
vi.mock("./tabs/cross-app-access/CrossAppAccessTab", async () => {
  const { ConnectionChecklist } =
    await import("./tabs/setup/ConnectionChecklist");
  return {
    CrossAppAccessTab: ({ connection }: { connection: LiveConnection }) => (
      <TooltipProvider>
        <div>Okta content: cross-app-access</div>
        {connection.checklist && (
          <ConnectionChecklist
            connection={connection}
            groups={["cross_app_access"]}
          />
        )}
      </TooltipProvider>
    ),
  };
});
vi.mock("./OktaOverview", () => ({
  OktaOverview: ({ connection }: { connection: { kind: string } }) => (
    <div>Okta content: overview ({connection.kind})</div>
  ),
}));
vi.mock("./oktaEma", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./oktaEma")>()),
  connectionsUnavailable: (error: unknown) =>
    error instanceof Error && error.message === "unavailable",
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "missing" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ userSessionIssuers: { href: () => "/issuers" } }),
}));
vi.mock("@/components/api-error-alert", () => ({
  ApiErrorAlert: ({ error }: { error: Error | null }) =>
    error ? <div role="alert">{error.message}</div> : null,
}));

function show(view: string) {
  render(
    <MemoryRouter
      initialEntries={[
        `/example/identity?tab=identity-providers&provider=okta&view=${view}`,
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
  it.each(["pending", "verified", "revoked"])(
    "does not expose Okta management for an unsupported provider (%s)",
    (status) => {
      mocks.query.data.connection = {
        provider: "future_provider",
        status,
        orgUrl: "https://identity.example.com",
      };
      show("setup");
      expect(
        screen.getByText(/managing this unsupported identity provider/),
      ).toBeTruthy();
      expect(screen.queryByRole("tab")).toBeNull();
      expect(screen.queryByText(/Okta content:/)).toBeNull();
      expect(screen.getByRole("region", { name: "Okta" })).toBeTruthy();
      expect(
        screen
          .getByRole("link", { name: "All identity providers" })
          .getAttribute("href"),
      ).toBe("/example/identity?tab=identity-providers");
    },
  );

  it("does not offer a connect action for an unsupported provider", () => {
    mocks.query.data.connection = {
      provider: "future_provider",
      status: "revoked",
      orgUrl: "https://identity.example.com",
    };
    render(
      <MemoryRouter>
        <OktaProviderCard />
      </MemoryRouter>,
    );
    expect(
      screen.getByText(/managing this unsupported identity provider/),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Okta/ })).toBeNull();
    expect(
      screen.getByRole("article", { name: "Okta identity provider" }),
    ).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
    expect(screen.queryByText("Not connected")).toBeNull();
  });

  it.each([
    ["overview", "overview (connected)"],
    ["setup", "setup"],
    ["applications", "applications"],
    ["cross-app-access", "cross-app-access"],
    ["unknown", "overview (connected)"],
  ])("opens the %s Okta view", (view, content) => {
    mocks.query.data.connection = {
      provider: "okta",
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show(view);
    expect(screen.getByText(`Okta content: ${content}`)).toBeTruthy();
    expect(screen.getAllByRole("tab").map((el) => el.textContent)).toEqual([
      "Overview",
      "Connection",
      "Applications",
      "Cross App Access",
    ]);
    expect(
      screen
        .getByRole("link", { name: "All identity providers" })
        .getAttribute("href"),
    ).toBe("/example/identity?tab=identity-providers");
  });
  it("activates Okta views using ArrowRight and Space", async () => {
    mocks.query.data.connection = {
      provider: "okta",
      status: "verified",
      orgUrl: "https://example.okta.com",
    };
    show("setup");
    const setup = screen.getByRole("tab", { name: "Connection" });
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
  it("keeps a collapsed checklist collapsed after visiting another tab", async () => {
    mocks.query.data.connection = makeConnection({
      status: "verified",
      checklist: [makeChecklistItem("record_ai_agent", "cross_app_access")],
    });
    show("cross-app-access");
    const group = () =>
      screen.getByRole("button", {
        name: "Cross App Access setup, 0 of 1 complete",
      });
    expect(group().getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(group());
    const crossAppAccess = screen.getByRole("tab", {
      name: "Cross App Access",
    });
    crossAppAccess.focus();
    fireEvent.keyDown(crossAppAccess, { key: "ArrowLeft" });
    await waitFor(() =>
      expect(screen.getByText("Okta content: applications")).toBeTruthy(),
    );
    fireEvent.keyDown(screen.getByRole("tab", { name: "Applications" }), {
      key: "ArrowRight",
    });
    await waitFor(() =>
      expect(group().getAttribute("aria-expanded")).toBe("false"),
    );
  });
  it("offers the overview and connection, not Applications or Cross App Access, before Okta is connected", () => {
    show("setup");
    expect(screen.getByRole("heading", { name: "Okta" })).toBeTruthy();
    expect(screen.getByText("Okta content: setup")).toBeTruthy();
    expect(screen.getAllByRole("tab").map((el) => el.textContent)).toEqual([
      "Overview",
      "Connection",
    ]);
  });
  it.each([
    ["applications", "no", undefined],
    ["applications", "a revoked", "revoked"],
    ["cross-app-access", "no", undefined],
    ["cross-app-access", "a revoked", "revoked"],
  ])(
    "lands %s links on the overview with %s connection",
    (view, _, status) => {
      if (status) {
        mocks.query.data.connection = {
          provider: "okta",
          status,
          orgUrl: "https://example.okta.com",
        };
      }
      show(view);
      expect(
        screen.getByText("Okta content: overview (not-connected)"),
      ).toBeTruthy();
      expect(
        screen.queryByRole("tab", { name: "Cross App Access" }),
      ).toBeNull();
      expect(screen.queryByRole("tab", { name: "Applications" })).toBeNull();
    },
  );
  it.each(["pending", "verified"])(
    "keeps workspace tabs for %s connections",
    (status) => {
      mocks.query.data.connection = {
        provider: "okta",
        status,
        orgUrl: "https://example.okta.com",
      };
      show("cross-app-access");
      expect(screen.getAllByRole("tab")).toHaveLength(4);
      expect(screen.getByText("Okta content: cross-app-access")).toBeTruthy();
    },
  );
  it("hides workspace navigation while the connection loads", () => {
    mocks.query.isPending = true;
    show("setup");
    expect(
      screen.getByRole("link", { name: "All identity providers" }),
    ).toBeTruthy();
    expect(screen.queryByRole("tablist")).toBeNull();
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
  it("keeps what needs only the domain when this deployment cannot connect Okta", () => {
    mocks.query.data = undefined as never;
    mocks.query.error = new Error("unavailable");
    show("setup");
    expect(
      screen.getByText("Okta content: overview (unavailable)"),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("tablist")).toBeNull();
  });
});

it.each(["setup", "applications", "cross-app-access"])(
  "blocks unknown status on the %s view",
  (view) => {
    mocks.query.data.connection = {
      provider: "okta",
      status: "future_status",
      orgUrl: "https://example.okta.com",
    };
    show(view);
    expect(screen.getByText(/Connection status is unknown/)).toBeTruthy();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByText(/Okta content:/)).toBeNull();
  },
);
