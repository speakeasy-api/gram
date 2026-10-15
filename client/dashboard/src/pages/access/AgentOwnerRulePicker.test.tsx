import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AgentOwnerRulePicker } from "./AgentOwnerRulePicker";

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        { id: "user-a", name: "Person A", email: "a@example.test" },
        { id: "user-b", name: "Person B", email: "b@example.test" },
      ],
    },
    isPending: false,
    isError: false,
  }),
}));

afterEach(cleanup);

it("uses owner selectors and preserves the other selected owners", () => {
  const onChange = vi.fn<() => void>();
  render(
    <AgentOwnerRulePicker
      selectors={[{ resourceKind: "agent_owner", resourceId: "user-a" }]}
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Person B" }));
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent_owner", resourceId: "user-a" },
    { resourceKind: "agent_owner", resourceId: "user-b" },
  ]);
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Also allow each role holder to create their own agents",
    }),
  );
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent_owner", resourceId: "user-a" },
    { resourceKind: "agent_owner", resourceId: "self" },
  ]);
});

it("can remove an owner without removing other owners", () => {
  const onChange = vi.fn<() => void>();
  render(
    <AgentOwnerRulePicker
      selectors={[
        { resourceKind: "agent_owner", resourceId: "user-a" },
        { resourceKind: "agent_owner", resourceId: "user-b" },
      ]}
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Person A" }));
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent_owner", resourceId: "user-b" },
  ]);
});

it.each([
  { selectors: null, label: "Anyone in the organization" },
  {
    selectors: [{ resourceKind: "agent_owner" as const, resourceId: "self" }],
    label: "Each role holder",
  },
])(
  "shows the saved $label mode without rewriting it",
  ({ selectors, label }) => {
    const onChange = vi.fn<() => void>();
    render(<AgentOwnerRulePicker selectors={selectors} onChange={onChange} />);
    expect(screen.getByRole("combobox").textContent).toContain(label);
    expect(onChange).not.toHaveBeenCalled();
  },
);
