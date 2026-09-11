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
    const onChange = vi.fn();
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
