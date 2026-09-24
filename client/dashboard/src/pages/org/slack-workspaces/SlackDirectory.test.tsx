import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { SlackDirectoryConnection } from "@gram/client/models/components/slackdirectoryconnection.js";
import { SlackDirectory } from "./SlackDirectory";
import { SlackSyncStatus, SlackSyncButton } from "./SlackSyncStatus";

const mocks = vi.hoisted(() => ({
  orgSlug: "example",
  query: vi.fn(),
  mutate: vi.fn(),
  pending: false,
  canEdit: true,
  scope: vi.fn(),
  error: null as Error | null,
  rows: [] as unknown[],
  total: null as number | null,
  sortAsOf: new Date("2026-01-01T00:00:00Z"),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: mocks.orgSlug }),
}));
vi.mock("./SlackPersonnelPicker", () => ({
  SlackPersonnelPicker: ({
    member,
    people,
    canEdit,
  }: {
    member: { id: string; mapping?: { displayName: string } };
    people?: unknown[];
    canEdit: boolean;
  }) => (
    <div data-testid={`picker-${member.id}`}>
      {member.mapping?.displayName ?? "Not mapped"}
      {canEdit ? "" : " · read-only"}
      {people ? ` · ${people.length} people` : ""}
    </div>
  ),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: (
    _r: unknown,
    _s: unknown,
    options: { enabled: boolean },
  ) => ({
    data: options.enabled
      ? { users: [{ userId: "user_synthetic" }] }
      : undefined,
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => {
      mocks.scope(scope);
      return mocks.canEdit;
    },
  }),
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  invalidateAllSlackDirectoryMembers: vi.fn(),
  useSlackDirectoryMembers: (request: unknown) => {
    mocks.query(request);
    return {
      data: mocks.pending
        ? undefined
        : {
            members: mocks.rows,
            total: mocks.total ?? mocks.rows.length,
            sortAsOf: mocks.sortAsOf,
          },
      isPending: mocks.pending,
      isFetching: mocks.pending,
      isError: Boolean(mocks.error),
      error: mocks.error,
      refetch: vi.fn(),
    };
  },
}));
vi.mock("@gram/client/react-query/syncSlackDirectory.js", () => ({
  useSyncSlackDirectoryMutation: () => ({
    mutate: mocks.mutate,
    error: null,
    isPending: false,
  }),
}));
vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: ({ date }: { date: Date }) => <time>{String(date)}</time>,
}));
const connection: SlackDirectoryConnection = {
  id: "00000000-0000-4000-8000-000000000001",
  workspaceId: "TEXAMPLE01",
  workspaceName: "Example Engineering",
  generation: "00000000-0000-4000-8000-000000000002",
  status: "connected",
  grantedScopes: [],
  updatedAt: new Date("2026-01-01T00:00:00Z"),
  memberCount: 2,
  syncStatus: "idle",
  directoryStatus: "current",
  lastFullSyncSucceededAt: new Date("2026-01-01T00:00:00Z"),
};
function show(children: React.ReactNode, search = "") {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter
        initialEntries={[
          `/example/identity?tab=slack-workspaces&slack_view=members${search}`,
        ]}
      >
        <TooltipProvider>{children}</TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  mocks.pending = false;
  mocks.canEdit = true;
  mocks.error = null;
  mocks.rows = [];
  mocks.total = null;
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("shows the all-workspaces directory and search", async () => {
  show(<SlackDirectory connections={[connection]} />);
  expect(screen.getByRole("heading", { name: "Slack members" })).toBeTruthy();
  expect(screen.getByText(/Email addresses do not confirm/)).toBeTruthy();
  expect(screen.getByText("No members to show")).toBeTruthy();
  fireEvent.change(
    screen.getByPlaceholderText("Search name, email or Slack ID…"),
    { target: { value: "avery" } },
  );
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "avery", connectionId: undefined }),
    ),
  );
  expect(screen.queryByText("Confirm mapping")).toBeNull();
});
it("opens a workspace link with its filter applied", () => {
  show(
    <SlackDirectory connections={[connection]} />,
    `&slack_workspace=${connection.id}`,
  );
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({ connectionId: connection.id }),
  );
});
it("labels absent profiles and missing email without inventing a person", () => {
  mocks.rows = [
    {
      id: "example-member",
      connectionId: connection.id,
      workspaceId: connection.workspaceId,
      workspaceName: connection.workspaceName,
      slackUserId: "UEXAMPLE01",
      status: "unknown",
      memberType: "unknown",
      lastSeenAt: "2025-12-01T00:00:00Z",
      observedInLastSync: false,
    },
  ];
  show(<SlackDirectory connections={[connection]} />);
  expect(screen.getByText("Not seen in last sync")).toBeTruthy();
  expect(screen.getByText("Not provided")).toBeTruthy();
  expect(screen.getByRole("table")).toBeTruthy();
});
it("hides deactivated members and bots until toggled and pages by number", async () => {
  mocks.rows = [
    {
      id: "example-member",
      connectionId: connection.id,
      workspaceId: connection.workspaceId,
      workspaceName: connection.workspaceName,
      slackUserId: "UEXAMPLE01",
      status: "active",
      memberType: "person",
      lastSeenAt: "2025-12-01T00:00:00Z",
      observedInLastSync: true,
    },
  ];
  mocks.total = 120;
  show(<SlackDirectory connections={[connection]} />);
  expect(mocks.query).toHaveBeenLastCalledWith(
    expect.objectContaining({
      includeDeactivated: false,
      includeBots: false,
      includeGuests: false,
      page: 1,
      limit: 50,
    }),
  );
  expect(screen.queryByText("Mapping status")).toBeNull();
  expect(screen.getByText(/Page 1 of 3/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
    ),
  );
  fireEvent.click(
    screen.getByRole("switch", { name: "Show deactivated members" }),
  );
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ includeDeactivated: true, page: 1 }),
    ),
  );
  fireEvent.click(screen.getByRole("switch", { name: "Show bots and apps" }));
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ includeDeactivated: true, includeBots: true }),
    ),
  );
  fireEvent.click(screen.getByRole("switch", { name: "Show guests" }));
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ includeGuests: true }),
    ),
  );
});
it("shows an error recovery action and never calls it an empty directory", () => {
  mocks.error = new Error("Could not load members");
  show(<SlackDirectory connections={[connection]} />);
  expect(
    screen.getByRole("button", { name: "Try loading members again" }),
  ).toBeTruthy();
  expect(screen.queryByText("No members to show")).toBeNull();
});
it("shows loading while the directory is pending", () => {
  mocks.pending = true;
  show(<SlackDirectory connections={[connection]} />);
  expect(screen.queryByText("No members to show")).toBeNull();
});
it("keeps the last full sync visible during rate-limit progress", () => {
  show(
    <SlackSyncStatus
      connection={{
        ...connection,
        syncStatus: "running",
        syncPhase: "waiting_for_slack",
        syncPages: 3,
        syncMembers: 450,
      }}
    />,
  );
  expect(screen.getByText(/Last full sync/)).toBeTruthy();
  expect(screen.getByRole("status").textContent).toContain("450 members found");
  expect(screen.getByRole("status").textContent).toContain("Waiting for Slack");
});
it("distinguishes never-synced, stale, unknown progress and failed states", () => {
  const view = show(
    <SlackSyncStatus
      connection={{
        ...connection,
        directoryStatus: "never_synced",
        lastFullSyncSucceededAt: undefined,
      }}
    />,
  );
  expect(screen.getByText("Never synced")).toBeTruthy();
  view.unmount();
  show(
    <SlackSyncStatus
      connection={{
        ...connection,
        directoryStatus: "stale",
        syncStatus: "unknown",
        lastErrorCode: "provider_unavailable",
        lastSyncFailedAt: new Date("2026-01-02T00:00:00Z"),
      }}
    />,
  );
  expect(
    screen.getByText(/previous or unavailable authorization/),
  ).toBeTruthy();
  expect(screen.getByText(/Sync to refresh it/).textContent).not.toContain(
    "Reconnect",
  );
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.getByRole("alert").textContent).toContain(
    "last complete directory is kept",
  );
});
it("manual sync sends the selected connection generation and blocks reconnect-required", () => {
  const view = show(<SlackSyncButton connection={connection} />);
  fireEvent.click(screen.getByRole("button", { name: "Sync now" }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        syncSlackDirectoryRequestBody: {
          id: connection.id,
          generation: connection.generation,
        },
      },
    }),
  );
  view.unmount();
  show(
    <SlackSyncButton
      connection={{ ...connection, status: "reconnect_required" }}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Sync now" }).hasAttribute("disabled"),
  ).toBe(true);
});

it("prevents shared demo sync even with a connected workspace", () => {
  mocks.orgSlug = "acme-demo";
  try {
    show(<SlackSyncButton connection={connection} />);
    expect(
      screen.getByRole("button", { name: "Sync now" }).hasAttribute("disabled"),
    ).toBe(true);
  } finally {
    mocks.orgSlug = "example";
  }
});

const unmappedMember = {
  id: "member-synthetic",
  connectionId: connection.id,
  workspaceId: connection.workspaceId,
  workspaceName: connection.workspaceName,
  slackUserId: "UEXAMPLE01",
  displayName: "Synthetic Account",
  status: "active",
  memberType: "person",
  lastSeenAt: new Date(),
  observedInLastSync: true,
  mappingStatus: "unmapped",
  mappingRevision: 0,
  observationToken: "example-evidence",
};
it("renders an inline Personnel picker per row with the organization's people", () => {
  mocks.rows = [
    unmappedMember,
    {
      ...unmappedMember,
      id: "member-mapped",
      mappingStatus: "mapped",
      mapping: {
        id: "mapping-synthetic",
        userId: "user_synthetic",
        displayName: "Synthetic Person",
        email: "synthetic@demo.getgram.ai",
        active: true,
      },
    },
  ];
  show(<SlackDirectory connections={[connection]} />);
  expect(screen.getByTestId("picker-member-synthetic").textContent).toBe(
    "Not mapped · 1 people",
  );
  expect(screen.getByTestId("picker-member-mapped").textContent).toContain(
    "Synthetic Person",
  );
  expect(screen.queryByRole("dialog")).toBeNull();
});
it("renders read-only pickers for employees without loading people", () => {
  mocks.canEdit = false;
  mocks.rows = [unmappedMember];
  show(<SlackDirectory connections={[connection]} />);
  expect(mocks.scope).toHaveBeenCalledWith("org:admin");
  expect(screen.getByTestId("picker-member-synthetic").textContent).toBe(
    "Not mapped · read-only",
  );
});
it("pins the sort time the server returned for the life of the view", async () => {
  const serverTime = new Date("2026-01-02T03:04:05Z");
  mocks.sortAsOf = serverTime;
  show(<SlackDirectory connections={[connection]} />);
  expect(mocks.query.mock.calls[0]?.[0].sortAsOf).toBeUndefined();
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ sortAsOf: serverTime }),
    ),
  );
  mocks.sortAsOf = new Date("2026-02-01T00:00:00Z");
  fireEvent.change(
    screen.getByPlaceholderText("Search name, email or Slack ID…"),
    { target: { value: "" } },
  );
  expect(mocks.query.mock.calls.at(-1)?.[0].sortAsOf).toBe(serverTime);
});
it("passes the mapping-status toolbar filter to the paginated query", () => {
  show(
    <SlackDirectory connections={[connection]} />,
    "&slack_mapping=needs_review",
  );
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({ mappingStatus: "needs_review" }),
  );
});
