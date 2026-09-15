import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AssigneePicker } from "./assignee-picker";

const state = vi.hoisted(() => ({
  isError: false,
  isLoading: false,
  isSuccess: true,
  refetch: vi.fn(),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: () => ({ ...state, data: { users: [] } }),
}));
vi.mock("./assignee-avatar", () => ({ AssigneeAvatar: () => null }));
beforeEach(() => {
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
        onChange={vi.fn()}
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
  const onChange = vi.fn();
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
