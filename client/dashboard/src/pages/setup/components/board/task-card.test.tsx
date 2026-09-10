import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Action } from "@/components/ui/MoreActions";
import type { BoardTask } from "./board-store";
import { ONBOARDING_TASKS } from "./tasks";
import { TaskCard } from "./task-card";

vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({ actions }: { actions: Action[] }) => (
    <div data-testid="task-menu">
      {actions.map((action) => (
        <button
          key={action.label}
          disabled={action.disabled}
          onClick={action.onClick}
        >
          {action.label}
          {action.description && <span>{action.description}</span>}
        </button>
      ))}
    </div>
  ),
}));

afterEach(cleanup);

function renderCard(overrides: Partial<BoardTask> = {}, isReminding = false) {
  const onRemind = vi.fn<() => void>();
  const onOpen = vi.fn<() => void>();
  const task: BoardTask = {
    ...ONBOARDING_TASKS[0]!,
    status: "todo",
    verified: false,
    hidden: false,
    assignee: { kind: "email", email: "owner@example.com" },
    ...overrides,
  };
  render(
    <TaskCard
      task={task}
      canHide={false}
      isReminding={isReminding}
      onOpen={onOpen}
      onRemind={onRemind}
      onSetStatus={vi.fn<() => void>()}
      onAssign={vi.fn<() => void>()}
      onToggleHidden={vi.fn<() => void>()}
    />,
  );
  return { onRemind, onOpen };
}

describe("TaskCard reminder menu", () => {
  it("sends a reminder from the menu without opening the task", () => {
    const { onRemind, onOpen } = renderCard();
    const remind = screen.getByRole("button", { name: "Remind" });
    expect(screen.getByTestId("task-menu").contains(remind)).toBe(true);
    fireEvent.click(remind);
    expect(onRemind).toHaveBeenCalledOnce();
    expect(onOpen).not.toHaveBeenCalled();
  });

  it.each([
    [{ assignee: undefined }, "Assign someone first"],
    [{ status: "done" }, "This task is already done"],
  ] as const)("preserves reminder eligibility: %s", (overrides, reason) => {
    renderCard(overrides);
    const remind = screen.getByRole("button", { name: `Remind ${reason}` });
    expect((remind as HTMLButtonElement).disabled).toBe(true);
  });

  it("disables repeat reminders while sending", () => {
    renderCard({}, true);
    expect(
      (
        screen.getByRole("button", {
          name: "Sending reminder…",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
