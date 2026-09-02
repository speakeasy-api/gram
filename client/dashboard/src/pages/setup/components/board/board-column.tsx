import { type DragEvent, type ReactNode, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { TASK_DRAG_TYPE } from "./task-card";
import {
  isOnboardingTaskId,
  type OnboardingTaskId,
  TASK_STATUS_META,
  type TaskStatus,
} from "./tasks";

interface BoardColumnProps {
  status: TaskStatus;
  count: number;
  onDropTask: (id: OnboardingTaskId) => void;
  children: ReactNode;
}

function carriesTask(event: DragEvent<HTMLElement>): boolean {
  return event.dataTransfer.types.includes(TASK_DRAG_TYPE);
}

/** One status column: a drop target for cards dragged from other columns. */
export function BoardColumn({
  status,
  count,
  onDropTask,
  children,
}: BoardColumnProps): JSX.Element {
  const meta = TASK_STATUS_META[status];
  const [isOver, setIsOver] = useState(false);
  // dragenter/dragleave fire for every descendant the pointer crosses, so
  // track depth and only clear the highlight once the column itself is left.
  const depth = useRef(0);

  const handleDragEnter = (event: DragEvent<HTMLElement>) => {
    if (!carriesTask(event)) return;
    depth.current += 1;
    setIsOver(true);
  };

  const handleDragLeave = () => {
    depth.current = Math.max(0, depth.current - 1);
    if (depth.current === 0) setIsOver(false);
  };

  const handleDragOver = (event: DragEvent<HTMLElement>) => {
    if (!carriesTask(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  };

  const handleDrop = (event: DragEvent<HTMLElement>) => {
    event.preventDefault();
    depth.current = 0;
    setIsOver(false);
    const id = event.dataTransfer.getData(TASK_DRAG_TYPE);
    if (isOnboardingTaskId(id)) onDropTask(id);
  };

  return (
    <section
      aria-label={`${meta.label} column`}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
      className={cn(
        "flex min-h-[28rem] flex-col gap-3 border border-dashed border-transparent p-2 transition-colors",
        isOver && "border-foreground/40 bg-card/60",
      )}
    >
      <header className="flex items-center justify-between gap-2 px-1">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className={cn("size-2 shrink-0 rounded-full", meta.dotClassName)}
            aria-hidden="true"
          />
          <h2 className="text-foreground text-sm font-medium">{meta.label}</h2>
          <span className="text-muted-foreground truncate text-xs">
            {meta.hint}
          </span>
        </div>
        <span className="text-eyebrow tabular-nums">{count}</span>
      </header>
      <div className="flex flex-col gap-2">{children}</div>
      {count === 0 && (
        <p className="border-border text-muted-foreground border border-dashed px-3 py-6 text-center text-xs">
          Drag a task here
        </p>
      )}
    </section>
  );
}
