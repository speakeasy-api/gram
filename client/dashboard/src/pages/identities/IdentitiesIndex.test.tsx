import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example", slug: "example" }),
  useSession: () => ({ user: { id: "user_example" } }),
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
  useRoutes: () => ({ agents: { href: () => "/agents" } }),
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
        Filters: ({ schema }: { schema: { id: string }[] }) => (
          <div data-testid="filters">
            {schema.map((item) => item.id).join(",")}
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
  mocks.agents.mockResolvedValue([
    { id: "agent_example", name: "Registered agent" },
  ]);
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
function setup(search = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/identities${search}`]}>
        <IdentitiesIndex />
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
    setup();
    await waitFor(() =>
      expect(screen.getByTestId("rows").textContent).toContain(
        "unknown_subject",
      ),
    );
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
    setup(`?kind=${kind}`);
    await waitFor(() =>
      expect(screen.getByTestId("rows").textContent).toContain(
        "unknown_subject",
      ),
    );
    expect(screen.getByTestId("rows").textContent).not.toContain(
      "human@example.com",
    );
    expect(screen.getByTestId("filters").textContent).toContain("kind");
    expect(
      screen
        .getByRole("button", { name: "Custom" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    await waitFor(() =>
      expect(screen.getByTestId("rows").textContent).toContain(
        "human@example.com",
      ),
    );
  },
);
