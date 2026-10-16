import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AgentOwnerRulePicker } from "./AgentOwnerRulePicker";
import type { Selector } from "@gram/client/models/components/selector.js";

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
  const onChange = vi.fn<(selectors: Selector[] | null) => void>();
  const view = render(
    <AgentOwnerRulePicker
      selectors={[
        { resourceKind: "agent", resourceId: "*", ownerUserId: "user-a" },
      ]}
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Person B" }));
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent", resourceId: "*", ownerUserId: "user-a" },
    { resourceKind: "agent", resourceId: "*", ownerUserId: "user-b" },
  ]);
  view.rerender(
    <AgentOwnerRulePicker
      selectors={onChange.mock.lastCall![0]}
      onChange={onChange}
    />,
  );
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Also include each role holder’s agents",
    }),
  );
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent", resourceId: "*", ownerUserId: "user-a" },
    { resourceKind: "agent", resourceId: "*", ownerUserId: "user-b" },
    { resourceKind: "agent", resourceId: "*", ownerUserId: "self" },
  ]);
});

it("can remove an owner without removing other owners", () => {
  const onChange = vi.fn<() => void>();
  render(
    <AgentOwnerRulePicker
      selectors={[
        { resourceKind: "agent", resourceId: "*", ownerUserId: "user-a" },
        { resourceKind: "agent", resourceId: "*", ownerUserId: "user-b" },
      ]}
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Person A" }));
  expect(onChange).toHaveBeenLastCalledWith([
    { resourceKind: "agent", resourceId: "*", ownerUserId: "user-b" },
  ]);
});

it.each([
  { selectors: null, label: "Anyone in the organization" },
  {
    selectors: [
      { resourceKind: "agent" as const, resourceId: "*", ownerUserId: "*" },
    ],
    label: "Anyone in the organization",
  },
  {
    selectors: [
      { resourceKind: "agent" as const, resourceId: "*", ownerUserId: "self" },
    ],
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

it("replaces selectors when switching between all, self, and selected people", () => {
  const onChange = vi.fn<(selectors: Selector[] | null) => void>();
  const view = render(
    <AgentOwnerRulePicker selectors={null} onChange={onChange} />,
  );
  const choose = (label: string, expected: Selector[] | null) => {
    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: label }));
    expect(onChange).toHaveBeenLastCalledWith(expected);
    view.rerender(
      <AgentOwnerRulePicker
        selectors={onChange.mock.lastCall![0]}
        onChange={onChange}
      />,
    );
    expect(screen.getByRole("combobox").textContent).toContain(label);
  };
  choose("Each role holder", [
    { resourceKind: "agent", resourceId: "*", ownerUserId: "self" },
  ]);
  expect(screen.queryByLabelText("Find owners")).toBeNull();
  choose("Selected people", []);
  fireEvent.click(screen.getByRole("checkbox", { name: "Person A" }));
  view.rerender(
    <AgentOwnerRulePicker
      selectors={onChange.mock.lastCall![0]}
      onChange={onChange}
    />,
  );
  choose("Anyone in the organization", null);
  expect(screen.queryByLabelText("Find owners")).toBeNull();
});
