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
  query: vi.fn(),
  mutate: vi.fn(),
  pending: false,
  error: null as Error | null,
  rows: [] as unknown[],
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  invalidateAllSlackDirectoryMembers: vi.fn(),
  useSlackDirectoryMembers: (request: unknown) => {
    mocks.query(request);
    return {
      data: mocks.pending
        ? undefined
        : { members: mocks.rows, total: mocks.rows.length },
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
  mocks.error = null;
  mocks.rows = [];
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("shows the all-workspaces directory and search without mapping controls", async () => {
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
  expect(screen.getByRole("status").textContent).toContain(
    "progress is unavailable",
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "last complete directory is kept",
  );
});
it("manual sync sends the selected connection generation and blocks reconnect-required", () => {
  const view = show(<SlackSyncButton connection={connection} />);
  fireEvent.click(screen.getByRole("button", { name: "Sync members" }));
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
    screen
      .getByRole("button", { name: "Sync members" })
      .hasAttribute("disabled"),
  ).toBe(true);
});
