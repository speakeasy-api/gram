import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { type Action, MoreActions } from "@/components/ui/MoreActions";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { BoardTask } from "./board-store";
import { TaskDependencies } from "./task-dependencies";
import { TASK_STATUS_META, TASK_STATUSES, type TaskStatus } from "./tasks";

interface TaskCardProps {
  task: BoardTask;
  canHide: boolean;
  canSetStatus: boolean;
  isPending: boolean;
  onOpen: () => void;
  onSetStatus: (status: TaskStatus) => void;
  onToggleHidden: () => void;
}

function buildMenuActions({
  task,
  canHide,
  onOpen,
  onSetStatus,
  onToggleHidden,
  canSetStatus,
  isPending,
}: Pick<
  TaskCardProps,
  | "task"
  | "canHide"
  | "onOpen"
  | "onSetStatus"
  | "onToggleHidden"
  | "canSetStatus"
  | "isPending"
>): Action[] {
  const actions: Action[] = [
    {
      icon: "maximize-2",
      label: "Open task",
      onClick: onOpen,
      disabled: task.blockedBy.length > 0,
    },
  ];
  if (canSetStatus && !task.verified) {
    for (const status of TASK_STATUSES) {
      if (status === task.status) continue;
      actions.push({
        label: `Move to ${TASK_STATUS_META[status].label}`,
        onClick: () => onSetStatus(status),
        separatorBefore: actions.length === 1,
        disabled: isPending || (status !== "todo" && task.blockedBy.length > 0),
      });
    }
  }
  if (canHide) {
    actions.push({
      icon: task.hidden ? "eye" : "eye-off",
      label: task.hidden ? "Show on board" : "Hide from board",
      onClick: onToggleHidden,
      separatorBefore: true,
      disabled: isPending,
    });
  }
  return actions;
}

export function TaskCard({
  task,
  canHide,
  canSetStatus,
  isPending,
  onOpen,
  onSetStatus,
  onToggleHidden,
}: TaskCardProps): JSX.Element {
  const blocked = task.blockedBy.length > 0;
  const descriptionRef = useRef<HTMLParagraphElement>(null);
  const [descriptionTruncated, setDescriptionTruncated] = useState(false);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const hasTooltip = blocked || descriptionTruncated;
  useEffect(() => {
    const description = descriptionRef.current;
    if (!description) return;
    const measure = () => {
      const truncated =
        description.scrollHeight > description.clientHeight ||
        description.scrollWidth > description.clientWidth;
      setDescriptionTruncated(truncated);
      if (!blocked && !truncated) setTooltipOpen(false);
    };
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(measure);
    observer?.observe(description);
    window.addEventListener("resize", measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [task.description, blocked]);
  const activation = (
    <button
      type="button"
      aria-label={`${task.title}, ${TASK_STATUS_META[task.verified ? "done" : task.status].label}`}
      aria-disabled={blocked || undefined}
      onClick={() => {
        if (!blocked) onOpen();
      }}
      className={cn(
        "focus-visible:ring-ring absolute inset-0 focus-visible:ring-2 focus-visible:outline-none",
        blocked ? "cursor-not-allowed" : "cursor-pointer",
      )}
    />
  );
  return (
    <article
      className={cn(
        "group bg-card border-border relative flex min-w-0 flex-col gap-0.5 border px-3 py-1.5 text-left transition-colors",
        !blocked && "hover:border-foreground/40",
        task.hidden && "opacity-60",
      )}
    >
      <TooltipProvider>
        <Tooltip
          disableHoverableContent
          open={hasTooltip && tooltipOpen}
          onOpenChange={(open) => setTooltipOpen(hasTooltip && open)}
        >
          <TooltipTrigger asChild>{activation}</TooltipTrigger>
          {hasTooltip && (
            <TooltipContent
              className={cn(
                "pointer-events-none bg-card text-foreground",
                blocked && "border-warning-default",
              )}
            >
              {blocked ? (
                <TaskDependencies dependencies={task.blockedBy} wrapTitles />
              ) : (
                task.description
              )}
            </TooltipContent>
          )}
        </Tooltip>
      </TooltipProvider>
      {blocked && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 text-muted-foreground opacity-[0.08]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(45deg, currentColor 0 1px, transparent 1px 7px), repeating-linear-gradient(-45deg, currentColor 0 1px, transparent 1px 7px)",
          }}
        />
      )}
      <div className="pointer-events-none flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="text-foreground min-w-0 truncate text-sm leading-snug font-medium">
            {task.title}
          </h3>
          {task.badge && <Badge size="sm">{task.badge}</Badge>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {task.hidden && (
            <Badge variant="warning" size="sm">
              Hidden
            </Badge>
          )}
          <div className="pointer-events-auto relative">
            <MoreActions
              size="compact"
              align="end"
              actions={buildMenuActions({
                task,
                canHide,
                onOpen,
                onSetStatus,
                onToggleHidden,
                canSetStatus,
                isPending,
              })}
            />
          </div>
        </div>
      </div>

      <p
        ref={descriptionRef}
        className="text-muted-foreground pointer-events-none mb-1 line-clamp-1 text-xs leading-snug"
      >
        {task.description}
      </p>
    </article>
  );
}
