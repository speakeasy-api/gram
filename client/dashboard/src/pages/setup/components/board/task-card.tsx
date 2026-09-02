import type { DragEvent, KeyboardEvent, MouseEvent } from "react";
import { Badge } from "@/components/ui/Badge";
import { type Action, MoreActions } from "@/components/ui/MoreActions";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { AssigneePicker } from "./assignee-picker";
import type { Assignee, BoardTask } from "./board-store";
import { RemindButton } from "./remind-button";
import { TASK_STATUS_META, TASK_STATUSES, type TaskStatus } from "./tasks";

/** Drag payload type: the task id travels as this custom MIME type. */
export const TASK_DRAG_TYPE = "application/x-gram-onboarding-task";

// Inline controls sit inside a card whose own click opens the task dialog.
const stopPropagation = (event: MouseEvent) => event.stopPropagation();

interface TaskCardProps {
  task: BoardTask;
  canHide: boolean;
  isReminding: boolean;
  onOpen: () => void;
  onSetStatus: (status: TaskStatus) => void;
  onAssign: (assignee: Assignee | undefined) => void;
  onToggleHidden: () => void;
  onRemind: () => void;
}

function buildMenuActions({
  task,
  canHide,
  onOpen,
  onSetStatus,
  onToggleHidden,
}: Pick<
  TaskCardProps,
  "task" | "canHide" | "onOpen" | "onSetStatus" | "onToggleHidden"
>): Action[] {
  const actions: Action[] = [
    { icon: "maximize-2", label: "Open task", onClick: onOpen },
  ];
  if (!task.verified) {
    for (const status of TASK_STATUSES) {
      if (status === task.status) continue;
      actions.push({
        label: `Move to ${TASK_STATUS_META[status].label}`,
        onClick: () => onSetStatus(status),
        separatorBefore: actions.length === 1,
      });
    }
  }
  if (canHide) {
    actions.push({
      icon: task.hidden ? "eye" : "eye-off",
      label: task.hidden ? "Show on board" : "Hide from board",
      onClick: onToggleHidden,
      separatorBefore: true,
    });
  }
  return actions;
}

export function TaskCard({
  task,
  canHide,
  isReminding,
  onOpen,
  onSetStatus,
  onAssign,
  onToggleHidden,
  onRemind,
}: TaskCardProps): JSX.Element {
  // Server-verified tasks are pinned to Done, so there is nowhere to drag them.
  const draggable = !task.verified;

  const handleDragStart = (event: DragEvent<HTMLElement>) => {
    event.dataTransfer.setData(TASK_DRAG_TYPE, task.id);
    event.dataTransfer.effectAllowed = "move";
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    // Keys pressed inside the assignee picker or menu belong to them.
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen();
    }
  };

  return (
    <article
      role="button"
      tabIndex={0}
      aria-label={`${task.title}, ${TASK_STATUS_META[task.status].label}`}
      draggable={draggable}
      onDragStart={draggable ? handleDragStart : undefined}
      onClick={onOpen}
      onKeyDown={handleKeyDown}
      className={cn(
        "group bg-card border-border hover:border-foreground/40 focus-visible:ring-ring flex cursor-pointer flex-col gap-2 border p-3 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none",
        draggable && "cursor-grab active:cursor-grabbing",
        task.hidden && "opacity-60",
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-eyebrow pt-1">{task.suggestedOwner}</span>
        <div className="flex items-center gap-1" onClick={stopPropagation}>
          {task.badge && <Badge size="sm">{task.badge}</Badge>}
          {task.verified && (
            <Badge variant="success" size="sm">
              Verified
            </Badge>
          )}
          {task.hidden && (
            <Badge variant="warning" size="sm">
              Hidden
            </Badge>
          )}
          <MoreActions
            actions={buildMenuActions({
              task,
              canHide,
              onOpen,
              onSetStatus,
              onToggleHidden,
            })}
          />
        </div>
      </div>

      <div>
        <h3 className="text-foreground text-sm leading-snug font-medium">
          {task.title}
        </h3>
        <p className="text-muted-foreground mt-0.5 line-clamp-2 text-xs leading-snug">
          {task.description}
        </p>
      </div>

      <div
        className="border-border mt-1 flex items-center justify-between gap-2 border-t pt-2"
        onClick={stopPropagation}
      >
        <AssigneePicker assignee={task.assignee} onChange={onAssign} />
        <RemindButton
          task={task}
          isReminding={isReminding}
          onRemind={onRemind}
        />
      </div>

      {task.lastRemindedAt && (
        <span className="text-muted-foreground text-[11px]">
          Reminded {formatRelativeTime(task.lastRemindedAt)}
        </span>
      )}
    </article>
  );
}
