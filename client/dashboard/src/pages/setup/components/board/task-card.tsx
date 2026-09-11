import { Badge } from "@/components/ui/Badge";
import { type Action, MoreActions } from "@/components/ui/MoreActions";
import { cn } from "@/lib/utils";
import { AssigneePicker } from "./assignee-picker";
import type { Assignee, BoardTask } from "./board-store";
import { TASK_STATUS_META, TASK_STATUSES, type TaskStatus } from "./tasks";

interface TaskCardProps {
  task: BoardTask;
  canHide: boolean;
  canAssign: boolean;
  canSetStatus: boolean;
  isPending: boolean;
  onOpen: () => void;
  onSetStatus: (status: TaskStatus) => void;
  onAssign: (assignee: Assignee | undefined) => void;
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
    { icon: "maximize-2", label: "Open task", onClick: onOpen },
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
  canAssign,
  canSetStatus,
  isPending,
  onOpen,
  onSetStatus,
  onAssign,
  onToggleHidden,
}: TaskCardProps): JSX.Element {
  return (
    <article
      className={cn(
        "group bg-card border-border hover:border-foreground/40 relative flex min-w-0 flex-col gap-0.5 border px-3 py-1.5 text-left transition-colors",
        task.hidden && "opacity-60",
      )}
    >
      <button
        type="button"
        aria-label={`${task.title}, ${TASK_STATUS_META[task.status].label}`}
        onClick={onOpen}
        className="focus-visible:ring-ring absolute inset-0 cursor-pointer focus-visible:ring-2 focus-visible:outline-none"
      />
      <div className="pointer-events-none flex items-center justify-between gap-2">
        <span className="text-eyebrow">{task.suggestedOwner}</span>
        <div className="flex items-center gap-1">
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
          <div className="pointer-events-auto relative">
            <MoreActions
              triggerStyle={{ height: 24 }}
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

      <div className="pointer-events-none mb-1 flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <h3 className="text-foreground min-w-0 truncate text-sm leading-snug font-medium">
            {task.title}
          </h3>
          {task.badge && <Badge size="sm">{task.badge}</Badge>}
        </div>
        <p className="text-muted-foreground line-clamp-1 text-xs leading-snug">
          {task.description}
        </p>
      </div>

      <div className="pointer-events-none border-border flex items-center justify-between gap-2 border-t pt-0.5">
        <div className="pointer-events-auto relative min-w-0 flex-1">
          <AssigneePicker
            assignee={task.assignee}
            onChange={onAssign}
            disabled={!canAssign || isPending}
          />
        </div>
        <span className="text-muted-foreground flex shrink-0 items-center gap-1.5 text-xs">
          <span
            className={cn(
              "size-1.5 rounded-full",
              TASK_STATUS_META[task.status].dotClassName,
            )}
          />
          {TASK_STATUS_META[task.status].label}
        </span>
      </div>

      {task.blockedBy.length > 0 && (
        <span className="pointer-events-none text-muted-foreground text-xs">
          Blocked by: {task.blockedBy.join(", ")}
        </span>
      )}
    </article>
  );
}
