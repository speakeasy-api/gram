// oxlint-disable react/only-export-components -- shared status labels keep columns and cards consistent
import type {
  SetupTask,
  SetupTaskStatus,
} from "@gram/client/models/components/setuptask.js";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { cn } from "@/lib/utils";

export const SETUP_TASK_STATUS_LABELS: Record<SetupTaskStatus, string> = {
  todo: "To do",
  in_progress: "In progress",
  awaiting_support: "Awaiting support",
  done: "Done",
};

type SetupTaskCardProps = {
  task: SetupTask;
  blockedTitles: string[];
  canChangeStatus: boolean;
  canOpen: boolean;
  canAssign: boolean;
  isPlatformAdmin: boolean;
  pending: boolean;
  retryInvite?: () => void;
  onOpen: () => void;
  onStatusChange: (status: SetupTaskStatus) => void;
  onAssign: () => void;
  onRemind: () => void;
  onHiddenChange: (hidden: boolean) => void;
};

function initials(label: string): string {
  return label
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase();
}

function taskActionLabel(status: SetupTaskStatus, blocked: boolean): string {
  if (blocked) return "View task";
  if (status === "todo") return "Start";
  if (status === "done") return "Review task";
  return "Continue task";
}

export function SetupTaskCard({
  task,
  blockedTitles,
  canChangeStatus,
  canOpen,
  canAssign,
  isPlatformAdmin,
  pending,
  retryInvite,
  onOpen,
  onStatusChange,
  onAssign,
  onRemind,
  onHiddenChange,
}: SetupTaskCardProps): JSX.Element {
  const blocked = task.blockedBy.length > 0;
  const completedByFact =
    "completedByFact" in task && task.completedByFact === true;
  const ownerLabel = task.assignee?.name ?? task.assignee?.email;
  const descriptionId = `setup-task-${task.key}-description`;
  const actions: Action[] = [];

  if (canAssign && ownerLabel) {
    actions.push({
      label: "Change owner",
      onClick: onAssign,
      disabled: pending,
    });
  }
  actions.push({
    label: "Send reminder",
    onClick: onRemind,
    disabled: pending || !ownerLabel,
    description: ownerLabel ? undefined : "Assign an owner first",
  });
  if (retryInvite) {
    actions.push({
      label: "Retry invite",
      onClick: retryInvite,
      disabled: pending,
    });
  }
  if (canChangeStatus && !completedByFact) {
    for (const [status, label] of Object.entries(SETUP_TASK_STATUS_LABELS)) {
      if (status === task.status) continue;
      actions.push({
        label: `Move to ${label.toLowerCase()}`,
        onClick: () => onStatusChange(status as SetupTaskStatus),
        disabled: pending,
      });
    }
  }
  if (isPlatformAdmin) {
    actions.push({
      label: task.hidden ? "Restore task" : "Hide task",
      onClick: () => onHiddenChange(!task.hidden),
      disabled: pending,
      destructive: !task.hidden,
    });
  }

  return (
    <article
      className={cn(
        "flex h-64 shrink-0 flex-col border bg-card",
        (blocked || task.hidden) && "text-muted-foreground",
      )}
      data-testid={`setup-task-${task.key}`}
    >
      <button
        type="button"
        className="w-full flex-1 space-y-3 p-4 text-left enabled:hover:bg-surface-secondary-default disabled:cursor-not-allowed"
        onClick={onOpen}
        disabled={pending || blocked || task.hidden || !canOpen}
        aria-describedby={descriptionId}
      >
        <h3 className="font-medium text-foreground">{task.title}</h3>
        <p id={descriptionId} className="text-sm text-muted-foreground">
          {task.description}
        </p>
        {blocked ? (
          <p className="border-l-2 border-warning-default pl-2 text-xs text-default-warning">
            Blocked by {blockedTitles.join(", ")}
          </p>
        ) : null}
        {task.hidden ? <Badge size="sm">Hidden</Badge> : null}
        {ownerLabel ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Avatar className="size-6">
              {task.assignee?.photoUrl ? (
                <AvatarImage src={task.assignee.photoUrl} alt="" />
              ) : null}
              <AvatarFallback>{initials(ownerLabel)}</AvatarFallback>
            </Avatar>
            <span className="truncate">{ownerLabel}</span>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">No owner assigned</p>
        )}
      </button>

      <div className="flex min-w-0 items-center gap-2 border-t p-3">
        <Button
          size="sm"
          variant="secondary"
          className="min-w-0 flex-1"
          onClick={onOpen}
          disabled={pending || blocked || task.hidden || !canOpen}
          aria-label={`${taskActionLabel(task.status, blocked)}: ${task.title}`}
        >
          {taskActionLabel(task.status, blocked)}
        </Button>
        {canAssign && !ownerLabel ? (
          <Button
            size="sm"
            variant="tertiary"
            onClick={onAssign}
            disabled={pending}
          >
            Assign
          </Button>
        ) : null}
        <MoreActions actions={actions} triggerDisabled={actions.length === 0} />
      </div>
    </article>
  );
}
