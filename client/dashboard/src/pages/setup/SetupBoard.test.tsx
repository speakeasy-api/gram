import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import SetupBoard from "./SetupBoard";
import { resolveBoardTasks } from "./components/board/board-store";
import { TASK_STATUSES } from "./components/board/tasks";
import type { OnboardingBoardState } from "./components/board/use-onboarding-board";
const tasks = resolveBoardTasks(
  [
    "connect-idp",
    "instrument-agents",
    "confirm-traffic",
    "configure-policies",
  ].map((key, i) => ({
    key,
    title: key,
    description: "Task",
    status: TASK_STATUSES[i]!,
    hidden: false,
    completedByFact: false,
    blockedBy: [],
  })),
);
function renderBoard(options: Partial<OnboardingBoardState> = {}) {
  const board: OnboardingBoardState = {
    tasks,
    error: undefined,
    writeError: null,
    isLoading: false,
    isPending: false,
    retry: vi.fn(),
    canAssign: true,
    canHideTasks: false,
    canSetStatus: () => true,
    setStatus: vi.fn(),
    assign: vi.fn(),
    setHidden: vi.fn(),
    ...options,
  };
  render(
    <SetupBoard
      tasks={board.tasks}
      board={board}
      renderTask={(task) => <button>{task.title}</button>}
    />,
  );
  return board;
}
afterEach(cleanup);
it("restores four status columns using shared server status", () => {
  renderBoard();
  for (const [i, label] of [
    "To Do",
    "In Progress",
    "Awaiting Support",
    "Done",
  ].entries()) {
    expect(
      within(screen.getByRole("region", { name: label })).getByText(
        tasks[i]!.title,
      ),
    ).toBeTruthy();
  }
});
it("routes status drops to the shared mutation", () => {
  const board = renderBoard();
  fireEvent.dragStart(screen.getByText("connect-idp").parentElement!, {
    dataTransfer: { setData: vi.fn() },
  });
  fireEvent.drop(screen.getByRole("region", { name: "In Progress" }));
  expect(board.setStatus).toHaveBeenCalledWith("connect-idp", "in_progress");
});
it.each(["blocked", "unauthorized", "pending"])(
  "rejects %s drops",
  (reason) => {
    const board = renderBoard({
      tasks: [
        {
          ...tasks[0]!,
          blockedBy: reason === "blocked" ? ["identity-provider"] : [],
        },
      ],
      canSetStatus: () => reason !== "unauthorized",
      isPending: reason === "pending",
    });
    fireEvent.dragStart(screen.getByText("connect-idp").parentElement!, {
      dataTransfer: { setData: vi.fn() },
    });
    fireEvent.drop(screen.getByRole("region", { name: "Done" }));
    expect(board.setStatus).not.toHaveBeenCalled();
  },
);
it("keeps all columns visible with filtered empty tasks", () => {
  renderBoard({ tasks: [] });
  expect(screen.getAllByText("No tasks")).toHaveLength(4);
});
