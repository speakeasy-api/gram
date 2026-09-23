import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import { SlackMappingDialog } from "./SlackMappingDialog";

const mocks = vi.hoisted(() => ({
  member: {} as SlackDirectoryMember,
  people: [] as OrganizationUser[],
  mutate: vi.fn(),
  close: vi.fn(),
  refetch: vi.fn(),
  error: null as Error | null,
}));
vi.mock("@gram/client/react-query/slackDirectoryMember.js", () => ({
  useSlackDirectoryMember: () => ({
    data: mocks.member,
    refetch: mocks.refetch,
    isFetching: false,
  }),
  invalidateAllSlackDirectoryMember: vi.fn(),
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  invalidateAllSlackDirectoryMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: () => ({
    data: { users: mocks.people },
    isFetching: false,
    refetch: mocks.refetch,
  }),
}));
vi.mock("@gram/client/react-query/setSlackIdentityMapping.js", () => ({
  useSetSlackIdentityMappingMutation: () => ({
    mutateAsync: mocks.mutate,
    isPending: false,
    isError: Boolean(mocks.error),
    error: mocks.error,
  }),
}));
vi.mock("@/components/ui/Combobox", () => ({
  Combobox: ({
    id,
    items,
    selected,
    onSelectionChange,
  }: {
    id: string;
    items: Array<{ value: string; label: string; description?: string }>;
    selected?: string;
    onSelectionChange: (item: unknown) => void;
  }) => (
    <select
      id={id}
      value={selected ?? ""}
      onChange={(e) =>
        onSelectionChange(items.find((item) => item.value === e.target.value))
      }
    >
      <option value="">Select a person…</option>
      {items.map((item) => (
        <option key={item.value} value={item.value}>
          {item.label} {item.description}
        </option>
      ))}
    </select>
  ),
}));
const person = (id: string, name: string): OrganizationUser => ({
  id: `membership-${id}`,
  userId: id,
  name,
  email: "synthetic@demo.getgram.ai",
  organizationId: "org_synthetic",
  createdAt: new Date(),
  updatedAt: new Date(),
});
beforeEach(() => {
  mocks.member = {
    id: "00000000-0000-4000-8000-000000000001",
    connectionId: "00000000-0000-4000-8000-000000000002",
    workspaceId: "TEXAMPLE01",
    workspaceName: "Example Engineering",
    slackUserId: "UEXAMPLE01",
    displayName: "Synthetic Person",
    email: " Synthetic@demo.getgram.ai ",
    status: "active",
    memberType: "person",
    lastSeenAt: new Date(),
    observedInLastSync: true,
    mappingRevision: 0,
    mappingStatus: "unmapped",
    observationToken: "current-evidence",
  };
  mocks.people = [
    person("user_synthetic_1", "Synthetic One"),
    person("user_synthetic_2", "Synthetic Two"),
  ];
  mocks.mutate.mockResolvedValue(mocks.member);
  mocks.error = null;
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function show() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SlackMappingDialog
        id={mocks.member.id}
        onClose={() => {
          mocks.close();
        }}
      />
    </QueryClientProvider>,
  );
}
function mapped() {
  mocks.member.mapping = {
    id: "mapping-example",
    userId: "user_synthetic_1",
    displayName: "Synthetic One",
    email: "different@demo.getgram.ai",
    active: true,
  };
  mocks.member.mappingRevision = 7;
  mocks.member.mappingStatus = "mapped";
}
it("suggests every matching existing person without selecting or confirming", () => {
  show();
  expect(
    screen.getAllByRole("option", { name: /Email match suggestion/ }),
  ).toHaveLength(2);
  expect((screen.getByLabelText("Personnel") as HTMLSelectElement).value).toBe(
    "",
  );
  expect(
    screen.getByRole("button", { name: "Confirm" }).hasAttribute("disabled"),
  ).toBe(true);
  expect(mocks.mutate).not.toHaveBeenCalled();
});
it("confirms exact selected person with the reviewed revision and source evidence", async () => {
  show();
  fireEvent.change(screen.getByLabelText("Personnel"), {
    target: { value: "user_synthetic_2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(mocks.close).toHaveBeenCalled());
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        setSlackIdentityMappingRequestBody: {
          id: mocks.member.id,
          userId: "user_synthetic_2",
          mappingRevision: 0,
          observationToken: "current-evidence",
        },
      },
    }),
  );
});
it("reassigns from the same Personnel dialog", async () => {
  mapped();
  show();
  fireEvent.change(screen.getByLabelText("Personnel"), {
    target: { value: "user_synthetic_2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(mocks.mutate).toHaveBeenCalled());
  expect(
    mocks.mutate.mock.calls[0]?.[0].request.setSlackIdentityMappingRequestBody,
  ).toMatchObject({ userId: "user_synthetic_2", mappingRevision: 7 });
});
it("unmaps only after choosing Not mapped and pressing Confirm", async () => {
  mapped();
  show();
  fireEvent.change(screen.getByLabelText("Personnel"), {
    target: { value: "__unmapped" },
  });
  expect(mocks.mutate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await waitFor(() => expect(mocks.mutate).toHaveBeenCalled());
  expect(
    mocks.mutate.mock.calls[0]?.[0].request.setSlackIdentityMappingRequestBody
      .userId,
  ).toBeUndefined();
});
it("shows sticky finding alongside current recovered directory state", () => {
  mapped();
  mocks.member.mappingConflictReason = "member_deactivated";
  mocks.member.mappingStatus = "needs_review";
  show();
  expect(screen.getByText(/Directory state: Active/)).toBeTruthy();
  expect(screen.getByText("Slack account was deactivated")).toBeTruthy();
});
it("allows only removal for a mapped bot", () => {
  mapped();
  mocks.member.memberType = "bot";
  show();
  expect(screen.queryByRole("option", { name: /Synthetic Two/ })).toBeNull();
  expect(screen.getByRole("option", { name: /Not mapped/ })).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Confirm" }).hasAttribute("disabled"),
  ).toBe(true);
});
it("requires reload and review after a rejected stale dialog", () => {
  mapped();
  mocks.error = new Error("This Slack member changed. Reload the member.");
  show();
  expect(
    screen.getByRole("button", { name: "Confirm" }).hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: "Reload member and review" }),
  );
  expect(mocks.refetch).toHaveBeenCalled();
});

it("identifies an inactive mapped person separately from Slack state", () => {
  mapped();
  mocks.member.mapping!.active = false;
  mocks.member.mappingStatus = "needs_review";
  show();
  expect(
    screen.getByText("Mapped person is no longer active in this organization"),
  ).toBeTruthy();
  expect(screen.getByText(/Directory state: Active/)).toBeTruthy();
});
