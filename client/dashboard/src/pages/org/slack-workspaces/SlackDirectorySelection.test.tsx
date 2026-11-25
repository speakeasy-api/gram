import { afterEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SlackDirectory } from "./SlackDirectory";

const mocks = vi.hoisted(() => ({
  pending: true,
  admin: true,
  query: vi.fn(),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: () => ({ data: undefined }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  useSlackDirectoryMembers: (request: unknown) => {
    mocks.query(request);
    return {
      data: mocks.pending ? undefined : { members: [], total: 0 },
      isPending: mocks.pending,
      isFetching: mocks.pending,
      isError: false,
      error: null,
    };
  },
}));
function show(search: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[`/example/identity${search}`]}>
        <SlackDirectory connections={[]} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.pending = true;
  mocks.admin = true;
});
it("opens a person's Slack ID from an identity link with every member type shown", () => {
  show(
    "?tab=slack-workspaces&slack_view=members&slack_workspace=filtered-workspace&slack_search=UEXAMPLE01&slack_deactivated=true&slack_bots=true&slack_guests=true",
  );
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({
      connectionId: "filtered-workspace",
      search: "UEXAMPLE01",
      includeDeactivated: true,
      includeBots: true,
      includeGuests: true,
    }),
  );
  expect(
    (
      screen.getByPlaceholderText(
        "Search name, email or Slack ID…",
      ) as HTMLInputElement
    ).value,
  ).toBe("UEXAMPLE01");
});
it("keeps workspace and mapping filters while searching", async () => {
  mocks.pending = false;
  show(
    "?tab=slack-workspaces&slack_view=members&slack_workspace=filtered-workspace&slack_mapping=unmapped",
  );
  fireEvent.change(
    screen.getByPlaceholderText("Search name, email or Slack ID…"),
    { target: { value: "no matching person" } },
  );
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({
        search: "no matching person",
        connectionId: "filtered-workspace",
        mappingStatus: "unmapped",
      }),
    ),
  );
  expect(screen.getByText("No matching members")).toBeTruthy();
});
