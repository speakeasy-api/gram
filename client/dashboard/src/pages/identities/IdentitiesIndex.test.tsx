import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import IdentitiesIndex from "./IdentitiesIndex";

const mocks = vi.hoisted(() => ({
  flag: "enabled",
  orgRead: true,
  agents: vi.fn(),
  coverage: vi.fn(),
  roster: vi.fn(),
  session: {
    user: { id: "user_example" },
    organizationOverride: false,
    impersonatorEmail: undefined as string | undefined,
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example", slug: "example" }),
  useSession: () => mocks.session,
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useProjectSlugForRequests: () => "example",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.orgRead }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.flag }),
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => null,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
  useRoutes: () => ({
    agents: { href: () => "/agents" },
    identities: {
      agents: {
        href: () => "/identities/agents",
        new: { href: () => "/identities/agents/new" },
      },
      detail: {
        overview: { href: (urn: string) => `/identities/${urn}/overview` },
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles: [] } }),
}));
vi.mock("./identityRoster", async (original) => ({
  ...(await original<typeof import("./identityRoster")>()),
  fetchRegisteredAgents: (...args: unknown[]) => mocks.agents(...args),
  fetchIdentityRoster: (...args: unknown[]) => mocks.roster(...args),
}));
vi.mock("./identityDeviceCoverage", async (original) => ({
  ...(await original<typeof import("./identityDeviceCoverage")>()),
  fetchDeviceCoverage: (...args: unknown[]) => mocks.coverage(...args),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/page-layout", () => {
  const Box = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return {
    Page: Object.assign(Box, {
      Header: Object.assign(Box, { Breadcrumbs: () => null }),
      Body: Box,
      Section: Object.assign(Box, {
        Title: Box,
        Description: Box,
        CTA: Box,
        Body: Box,
      }),
      Toolbar: Object.assign(Box, {
        Leading: Box,
        Actions: Box,
        Search: () => null,
        // Each filter holding a value gets a clear button, the way the real
        // sheet gives its chips one, so a test can take a filter back off.
        Filters: ({
          schema,
          values,
          onClear,
        }: {
          schema: { id: string }[];
          values: Record<string, unknown>;
          onClear: (id: string) => void;
        }) => (
          <div data-testid="filters">
            {schema.map((item) => item.id).join(",")}
            {Object.entries(values)
              .filter(([, value]) =>
                Array.isArray(value) ? value.length > 0 : value != null,
              )
              .map(([id]) => (
                <button key={id} type="button" onClick={() => onClear(id)}>
                  Clear {id}
                </button>
              ))}
          </div>
        ),
      }),
    }),
  };
});
vi.mock("@/components/chart/stat-tile", () => ({
  StatTile: () => null,
  StatTileSkeleton: () => null,
  StatTileGroup: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/ui/Table", () => ({
  Table: ({ data }: { data: { id: string; name: string }[] }) => (
    <div data-testid="rows">
      {data.map((row) => (
        <div key={row.id}>{row.name}</div>
      ))}
    </div>
  ),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.flag = "enabled";
  mocks.orgRead = true;
  mocks.session = {
    user: { id: "user_example" },
    organizationOverride: false,
    impersonatorEmail: undefined,
  };
  mocks.agents.mockResolvedValue({
    items: [{ id: "agent_example", name: "Registered agent" }],
  });
  mocks.coverage.mockResolvedValue({
    byUserId: new Map(),
    byEmail: new Map(),
    deviceCount: 0,
    truncated: false,
  });
  mocks.roster.mockResolvedValue([
    {
      userId: "unknown_subject",
      userType: "external",
      totalInputTokens: 0,
      totalOutputTokens: 0,
      lastSeenUnixNano: "0",
    },
    {
      userId: "human@example.com",
      userType: "external",
      totalInputTokens: 0,
      totalOutputTokens: 0,
      lastSeenUnixNano: "0",
    },
  ]);
});
/** Every rendered table's text, so an assertion does not care which one. */
const rowsText = () =>
  screen
    .getAllByTestId("rows")
    .map((node) => node.textContent)
    .join(" ");

function setup(search = "", kind: "person" | "agent" = "agent") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/identities${search}`]}>
        <IdentitiesIndex kind={kind} />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const result = render(tree());
  return { ...result, rerenderPage: () => result.rerender(tree()) };
}
it.each(["disabled", "loading", "missing", "error"])(
  "does not fetch registered agents with rollout %s",
  async (status) => {
    mocks.flag = status;
    setup("", "person");
    await waitFor(() => expect(rowsText()).toContain("unknown_subject"));
    expect(
      screen
        .getByRole("link", { name: "Configure IDP sync" })
        .getAttribute("href"),
    ).toBe("/org/identity");
    expect(mocks.agents).not.toHaveBeenCalled();
    expect(screen.queryByText("Registered agent")).toBeNull();
  },
);
it("hides cached registered agents after rollout is disabled", async () => {
  const page = setup();
  await screen.findByText("Registered agent");
  mocks.flag = "disabled";
  page.rerenderPage();
  expect(screen.queryByText("Registered agent")).toBeNull();
  expect(screen.queryByText("New agent identity")).toBeNull();
});
it("skips organization-only device coverage for a project reader", async () => {
  mocks.orgRead = false;
  setup();
  await screen.findByText("Registered agent");
  expect(mocks.coverage).not.toHaveBeenCalled();
});
it.each(["unknown", "unknown,agent"])(
  "preserves legacy kind=%s and exposes a clearable filter",
  async (kind) => {
    setup(`?kind=${kind}`, "person");
    await waitFor(() => expect(rowsText()).toContain("unknown_subject"));
    expect(
      screen
        .getAllByTestId("rows")
        .map((node) => node.textContent)
        .join(" "),
    ).not.toContain("human@example.com");
    // The kind filter is still honoured and still clearable, now through the
    // filter list rather than a segmented control the two tables made
    // redundant. Clearing it puts the rows it was hiding back.
    expect(screen.getByTestId("filters").textContent).toContain("kind");
    await userEvent.click(screen.getByRole("button", { name: "Clear kind" }));
    await waitFor(() => expect(rowsText()).toContain("human@example.com"));
  },
);

// Ported from the agent-management page's tests when that page was retired.
// A support session is reading on someone else's behalf, and an agent's
// management API answers to the owner — so the roster does not read it at all
// rather than reading it as the person being supported.
it("does not read agents in an organization override session", async () => {
  mocks.session = {
    user: { id: "user_example" },
    organizationOverride: true,
    impersonatorEmail: undefined,
  };
  setup("", "agent");
  await waitFor(() => expect(mocks.roster).toHaveBeenCalled());
  expect(mocks.agents).not.toHaveBeenCalled();
  expect(screen.queryByText("New agent identity")).toBeNull();
});

it("does not read agents while impersonating", async () => {
  mocks.session = {
    user: { id: "user_example" },
    organizationOverride: false,
    impersonatorEmail: "support@example.test",
  };
  setup("", "agent");
  await waitFor(() => expect(mocks.roster).toHaveBeenCalled());
  expect(mocks.agents).not.toHaveBeenCalled();
  expect(screen.queryByText("New agent identity")).toBeNull();
});
