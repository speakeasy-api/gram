import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SyncFromOkta } from "./SyncFromOkta";

const mocks = vi.hoisted(() => ({
  flag: "enabled",
  query: {
    isPending: false,
    isError: false,
    error: null as Error | null,
  },
  connection: undefined as { provider: string; status: string } | undefined,
  applications: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.flag }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      catalog: { href: () => "/org/projects/proj/mcp/catalog" },
      add: { href: () => "/org/projects/proj/mcp/add" },
    },
  }),
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ slug: "proj" }),
}));
vi.mock("@/components/page-layout", () => {
  const Passthrough = ({ children }: { children?: React.ReactNode }) => (
    <>{children}</>
  );
  return {
    Page: Object.assign(Passthrough, {
      Header: Object.assign(Passthrough, { Breadcrumbs: () => null }),
      Body: Passthrough,
      Section: Object.assign(Passthrough, {
        Title: Passthrough,
        Description: Passthrough,
      }),
    }),
  };
});
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/pages/org/identity-provider/oktaEma", () => ({
  connectionsUnavailable: (error: unknown) =>
    error instanceof Error && error.message === "unavailable",
}));
vi.mock("@/pages/org/identity-provider/identityProviderQueries", () => ({
  useOktaConnection: () => ({
    query: mocks.query,
    connection: mocks.connection,
  }),
}));
vi.mock("./ApplicationsTab", () => ({
  ApplicationsTab: (props: unknown) => {
    mocks.applications(props);
    return <div>Okta applications</div>;
  },
}));

beforeEach(() => {
  mocks.flag = "enabled";
  mocks.connection = undefined;
  mocks.query.isError = false;
  mocks.query.error = null;
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function show() {
  render(
    <MemoryRouter
      initialEntries={["/org/projects/proj/mcp/add/sync-from-okta"]}
    >
      <Routes>
        <Route
          path="/org/projects/proj/mcp/add/sync-from-okta"
          element={<SyncFromOkta />}
        />
        <Route path="/org/projects/proj/mcp/add" element={<>Add server</>} />
      </Routes>
    </MemoryRouter>,
  );
}

it("sends people back to Add MCP server while the flag is off", () => {
  mocks.flag = "disabled";
  show();
  expect(screen.getByText("Add server")).toBeTruthy();
});

it("offers the catalog when this deployment cannot hold an Okta connection", () => {
  mocks.query.isError = true;
  mocks.query.error = new Error("unavailable");
  show();
  expect(
    screen.getByText("Okta can't be connected on this deployment"),
  ).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: "Browse the catalog" })
      .getAttribute("href"),
  ).toBe("/org/projects/proj/mcp/catalog");
});

it.each([
  ["no connection", undefined],
  ["a revoked connection", { provider: "okta", status: "revoked" }],
])("links to Okta setup when there is %s", (_, connection) => {
  mocks.connection = connection;
  show();
  expect(
    screen.getByRole("link", { name: "Set up Okta" }).getAttribute("href"),
  ).toBe("/org/identity?tab=identity-providers&provider=okta&view=overview");
  expect(screen.queryByText("Okta applications")).toBeNull();
});

it("shows the applications for this project once Okta is connected", () => {
  mocks.connection = { provider: "okta", status: "verified" };
  show();
  expect(screen.getByText("Okta applications")).toBeTruthy();
  const props = mocks.applications.mock.calls[0]?.[0] as {
    projectSlug: string;
    setupHref: (search: string) => string;
  };
  expect(props.projectSlug).toBe("proj");
  expect(props.setupHref("?tab=x")).toBe("/org/identity?tab=x");
});
