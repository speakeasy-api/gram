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

function renderCard(
  overrides: Partial<BoardTask> = {},
  isPending = false,
  canSetStatus = true,
) {
  const onOpen = vi.fn<() => void>();
  const task: BoardTask = {
    ...ONBOARDING_TASKS[0]!,
    title: "Task",
    description: "Task description",
    blockedBy: [],
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
      isPending={isPending}
      canAssign={false}
      canSetStatus={canSetStatus}
      onOpen={onOpen}
      onSetStatus={vi.fn<() => void>()}
      onAssign={vi.fn<() => void>()}
      onToggleHidden={vi.fn<() => void>()}
    />,
  );
  return { onOpen };
}

describe("TaskCard controls", () => {
  it("removes reminders and retains keyboard opening", () => {
    const { onOpen } = renderCard();
    expect(screen.queryByText(/remind/i)).toBeNull();
    fireEvent.keyDown(screen.getByRole("button", { name: "Task, To Do" }), {
      key: "Enter",
    });
    expect(onOpen).toHaveBeenCalledOnce();
  });
  it("hides unauthorized transitions", () => {
    renderCard({}, false, false);
    expect(screen.queryByText("Move to Done")).toBeNull();
  });
  it("locks fact completion", () => {
    renderCard({ verified: true, status: "done" });
    expect(screen.queryByText("Move to To Do")).toBeNull();
  });
  it("disables blocked transitions", () => {
    renderCard({ blockedBy: ["instrument-agents"] });
    expect(
      (screen.getByText("Move to Done") as HTMLButtonElement).disabled,
    ).toBe(true);
  });
  it("disables transitions while saving", () => {
    renderCard({}, true);
    expect(
      (
        screen.getByRole("button", {
          name: "Move to Done",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
