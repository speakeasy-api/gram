import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { BoardTask } from "./board-store";
import type { OnboardingWorkstreamDefinition } from "./tasks";
import { countTasksBelow } from "./count-tasks-below";

interface WorkstreamColumnProps {
  workstream: OnboardingWorkstreamDefinition;
  tasks: BoardTask[];
  children: ReactNode;
}

export function WorkstreamColumn({
  workstream,
  tasks,
  children,
}: WorkstreamColumnProps): JSX.Element {
  const requiredTasks = tasks.filter((task) => !task.hidden && !task.badge);
  const completedTasks = requiredTasks.filter(
    (task) => task.status === "done",
  ).length;
  const headingId = `onboarding-workstream-${workstream.id}`;
  const taskListRef = useRef<HTMLDivElement>(null);
  const [tasksBelow, setTasksBelow] = useState(0);
  const updateTasksBelow = useCallback(() => {
    if (taskListRef.current) {
      setTasksBelow(countTasksBelow(taskListRef.current));
    }
  }, []);

  const taskIds = tasks.map((task) => task.id).join(",");
  useEffect(() => {
    const taskList = taskListRef.current;
    if (!taskList) return;

    updateTasksBelow();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(updateTasksBelow);
    observer.observe(taskList);
    for (const card of taskList.children) observer.observe(card);
    return () => observer.disconnect();
  }, [taskIds, updateTasksBelow]);

  return (
    <section
      aria-labelledby={headingId}
      className="bg-card flex min-h-64 max-h-full flex-col overflow-hidden border md:min-h-0"
    >
      <header className="bg-surface-secondary-default flex h-16 shrink-0 items-center justify-between gap-4 border-b px-3 py-2">
        <div className="min-w-0">
          <h2 id={headingId} className="text-foreground font-medium">
            {workstream.title}
          </h2>
          <p className="text-muted-foreground mt-0.5 line-clamp-1 text-sm">
            {workstream.description}
          </p>
        </div>
        <p className="text-muted-foreground shrink-0 text-sm font-medium tabular-nums">
          {requiredTasks.length === 0
            ? "No required tasks"
            : `${completedTasks} / ${requiredTasks.length}`}
        </p>
      </header>
      <div
        ref={taskListRef}
        className="grid min-h-0 flex-1 content-start gap-3 overflow-y-auto overscroll-contain p-3"
        onScroll={updateTasksBelow}
      >
        {children}
      </div>
      {tasksBelow > 0 ? (
        <div className="border-border bg-surface-secondary-default border-t px-3 py-1.5 text-center text-xs font-medium text-muted-foreground">
          ↓ {tasksBelow} more {tasksBelow === 1 ? "task" : "tasks"} below
        </div>
      ) : null}
    </section>
  );
}
