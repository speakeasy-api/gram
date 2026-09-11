import { useState, type ReactNode } from "react";
import type { BoardTask } from "./components/board/board-store";
import {
  TASK_STATUSES,
  TASK_STATUS_META,
  type TaskStatus,
} from "./components/board/tasks";
import type { OnboardingBoardState } from "./components/board/use-onboarding-board";

export default function SetupBoard({
  tasks,
  board,
  renderTask,
}: {
  tasks: BoardTask[];
  board: OnboardingBoardState;
  renderTask: (task: BoardTask) => ReactNode;
}): JSX.Element {
  const [dragged, setDragged] = useState<BoardTask | null>(null);
  const canDrop = (status: TaskStatus) =>
    dragged !== null &&
    !board.isPending &&
    board.canSetStatus(dragged) &&
    (status === "todo" || dragged.blockedBy.length === 0);
  return (
    <div
      role="region"
      aria-label="Setup Kanban"
      className="grid min-h-0 flex-1 grid-cols-1 gap-4 overflow-y-auto md:grid-cols-2 xl:grid-cols-4"
    >
      {TASK_STATUSES.map((status) => {
        const columnTasks = tasks.filter((task) => task.status === status);
        return (
          <section
            key={status}
            aria-labelledby={`setup-column-${status}`}
            className="bg-card flex min-h-64 flex-col border md:min-h-0"
            onDragOver={(event) => {
              if (canDrop(status)) event.preventDefault();
            }}
            onDrop={(event) => {
              event.preventDefault();
              if (dragged && canDrop(status))
                void board.setStatus(dragged.id, status);
              setDragged(null);
            }}
          >
            <header className="bg-surface-secondary-default flex shrink-0 items-center justify-between gap-2 border-b p-3">
              <h2 id={`setup-column-${status}`} className="font-medium">
                {TASK_STATUS_META[status].label}
              </h2>
              <span className="text-muted-foreground text-sm">
                {columnTasks.length}
              </span>
            </header>
            <div className="grid min-h-0 content-start gap-3 overflow-y-auto p-3">
              {columnTasks.map((task) => (
                <div
                  key={task.id}
                  className="min-w-0"
                  draggable={!board.isPending && board.canSetStatus(task)}
                  onDragStart={(event) => {
                    setDragged(task);
                    event.dataTransfer.effectAllowed = "move";
                    event.dataTransfer.setData("text/plain", task.id);
                  }}
                  onDragEnd={() => setDragged(null)}
                >
                  {renderTask(task)}
                </div>
              ))}
              {columnTasks.length === 0 && (
                <p className="text-muted-foreground text-sm">No tasks</p>
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
}
