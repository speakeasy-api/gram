import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { AssigneePicker } from "./assignee-picker";

const state = vi.hoisted(() => ({
  users: [] as { userId: string; name: string; email: string }[],
  isError: false,
  isLoading: false,
  isSuccess: true,
  refetch: vi.fn(),
  sendInvite: vi.fn(),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: () => ({ ...state, data: { users: state.users } }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({
    data: {
      roles: [
        { id: "member-role", slug: "member", name: "Member" },
        { id: "admin-role", slug: "admin", name: "Admin" },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/sendInvite.js", () => ({
  useSendInviteMutation: () => ({ mutateAsync: state.sendInvite }),
}));
vi.mock("./assignee-avatar", () => ({ AssigneeAvatar: () => null }));
beforeEach(() => {
  state.sendInvite.mockReset().mockResolvedValue(undefined);
  state.users = [];
  state.isError = false;
  state.isLoading = false;
  state.isSuccess = true;
  state.refetch.mockReset();
});
afterEach(cleanup);

it.each(["loading", "error"])(
  "does not offer external assignment during a directory %s",
  (mode) => {
    state.isSuccess = false;
    state.isLoading = mode === "loading";
    state.isError = mode === "error";
    const onChange = vi.fn<() => void>();
    const rendered = render(
      <AssigneePicker assignee={undefined} onChange={onChange} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));
    fireEvent.change(
      screen.getByPlaceholderText("Search team or enter an email"),
      {
        target: { value: "owner@example.test" },
      },
    );
    expect(screen.queryByText("Assign owner@example.test")).toBeNull();
    if (state.isError) {
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      expect(state.refetch).toHaveBeenCalledOnce();
    }
    state.isSuccess = true;
    state.isLoading = false;
    state.isError = false;
    rendered.rerender(
      <AssigneePicker assignee={undefined} onChange={onChange} />,
    );
    fireEvent.click(screen.getByText("Assign owner@example.test"));
    expect(onChange).toHaveBeenCalledWith({
      kind: "email",
      email: "owner@example.test",
    });
  },
);

it("shows the current owner when changing assignment", () => {
  const current = {
    userId: "user-current",
    name: "Current User",
    email: "current@example.test",
  };
  state.users = [current];
  render(
    <AssigneePicker
      assignee={{ kind: "user", ...current }}
      onChange={vi.fn<ComponentProps<typeof AssigneePicker>["onChange"]>()}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Assigned to Current User" }),
  );
  const owner = screen.getByRole("option", { name: /Current User/ });
  expect(owner.querySelector(".lucide-check")).not.toBeNull();
});

it.each([
  { kind: "email" as const, email: "invite@example.test" },
  {
    kind: "user" as const,
    userId: "member",
    name: "Team member",
    email: "member@example.test",
  },
])(
  "replaces the suggested role with the assigned person or email: %o",
  (assignee) => {
    render(
      <AssigneePicker
        assignee={assignee}
        placeholder="Engineering lead"
        onChange={vi.fn<ComponentProps<typeof AssigneePicker>["onChange"]>()}
      />,
    );
    expect(screen.queryByText("Engineering lead")).toBeNull();
    const trigger = screen.getByRole("button", {
      name: `Assigned to ${assignee.kind === "user" ? assignee.name : assignee.email}`,
    });
    expect(trigger.className).toContain("px-2");
    expect(trigger.className).toContain("-ms-2");
  },
);

it("requires an owner selection for a neutral workstream action", () => {
  const onChange = vi.fn<ComponentProps<typeof AssigneePicker>["onChange"]>();
  render(
    <AssigneePicker
      assignee={undefined}
      placeholder="Assign workstream"
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Assign workstream" }));
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.queryByText("Unassign")).toBeNull();
  expect(screen.queryByText("Mixed assignments")).toBeNull();
});

it("saves assignment before inviting and retries a failed invite without reassigning", async () => {
  const assignment = Promise.withResolvers<boolean>();
  const onChange = vi.fn(() => assignment.promise);
  state.sendInvite.mockRejectedValueOnce(new Error("Unavailable"));
  render(<AssigneePicker assignee={undefined} onChange={onChange} />);
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));
  fireEvent.change(
    screen.getByPlaceholderText("Search team or enter an email"),
    { target: { value: "owner@example.test" } },
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Send a team invite" }));
  fireEvent.change(screen.getByRole("combobox", { name: "Invite role" }), {
    target: { value: "admin-role" },
  });
  fireEvent.click(screen.getByText("Assign owner@example.test"));
  expect(state.sendInvite).not.toHaveBeenCalled();
  assignment.resolve(true);
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Retry team invite" }),
    ).toBeTruthy(),
  );
  expect(state.sendInvite).toHaveBeenCalledWith({
    request: {
      sendInviteRequestBody: {
        email: "owner@example.test",
        roleId: "admin-role",
      },
    },
  });
  fireEvent.click(screen.getByRole("button", { name: "Retry team invite" }));
  await waitFor(() => expect(state.sendInvite).toHaveBeenCalledTimes(2));
  expect(onChange).toHaveBeenCalledOnce();
});
it("does not invite after failed assignment", async () => {
  const onChange = vi.fn(async () => false);
  render(<AssigneePicker assignee={undefined} onChange={onChange} />);
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));
  fireEvent.change(
    screen.getByPlaceholderText("Search team or enter an email"),
    { target: { value: "owner@example.test" } },
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Send a team invite" }));
  fireEvent.click(screen.getByText("Assign owner@example.test"));
  await waitFor(() => expect(onChange).toHaveBeenCalledOnce());
  expect(state.sendInvite).not.toHaveBeenCalled();
});

it("offers the owner breakdown and clears mixed assignment without an invite", async () => {
  const onChange = vi.fn(async () => true);
  render(
    <AssigneePicker
      assignee={undefined}
      onChange={onChange}
      placeholder="Mixed owners"
      ownerBreakdown="Member: 2 · Outside: 1"
      bulkAssignment
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Mixed owners" }));
  expect(screen.getByText("Member: 2 · Outside: 1")).toBeTruthy();
  expect(screen.getByText(/including hidden and optional tasks/)).toBeTruthy();
  fireEvent.click(screen.getByText("Unassign"));
  await waitFor(() =>
    expect(onChange).toHaveBeenCalledExactlyOnceWith(undefined),
  );
  expect(state.sendInvite).not.toHaveBeenCalled();
});
