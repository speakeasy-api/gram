import { afterEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SlackDirectory } from "./SlackDirectory";

const mocks = vi.hoisted(() => ({
  pending: true,
  admin: true,
  query: vi.fn(),
}));
vi.mock("./SlackMappingDialog", () => ({
  SlackMappingDialog: ({
    id,
    onClose,
  }: {
    id: string;
    onClose: () => void;
  }) => (
    <div role="dialog">
      Review {id}
      <button onClick={onClose}>Close review</button>
    </div>
  ),
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
function Location() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output>{location.search}</output>
      <button
        onClick={() =>
          void navigate(
            "?tab=slack-workspaces&slack_view=members&slack_member=membership-two&slack_workspace=filtered-workspace&slack_mapping=needs_review",
          )
        }
      >
        Other membership
      </button>
    </>
  );
}
function show() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter
        initialEntries={[
          "/example/identity?tab=slack-workspaces&slack_view=members&slack_member=membership-one&slack_workspace=filtered-workspace&slack_mapping=unmapped",
        ]}
      >
        <SlackDirectory connections={[]} />
        <Location />
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
it("opens the exact membership while a differently filtered directory is loading, and retains it through search", async () => {
  show();
  expect(screen.getByRole("dialog").textContent).toContain("membership-one");
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({
      connectionId: "filtered-workspace",
      mappingStatus: "unmapped",
    }),
  );
  fireEvent.change(
    screen.getByPlaceholderText("Search name, email or Slack ID…"),
    { target: { value: "no matching person" } },
  );
  mocks.pending = false;
  await waitFor(() =>
    expect(mocks.query).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "no matching person" }),
    ),
  );
  expect(screen.getByRole("dialog").textContent).toContain("membership-one");
  expect(screen.getByText("No matching members")).toBeTruthy();
});
it("follows URL selection changes and closes without dropping workspace or mapping filters", () => {
  show();
  fireEvent.click(screen.getByRole("button", { name: "Other membership" }));
  expect(screen.getByRole("dialog").textContent).toContain("membership-two");
  fireEvent.click(screen.getByRole("button", { name: "Close review" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("status").textContent).toContain(
    "slack_mapping=needs_review",
  );
  expect(screen.getByRole("status").textContent).toContain(
    "slack_workspace=filtered-workspace",
  );
  expect(screen.getByRole("status").textContent).not.toContain("slack_member");
});
it("does not mount the privileged membership dialog for a non-admin URL", () => {
  mocks.admin = false;
  show();
  expect(screen.queryByRole("dialog")).toBeNull();
});
