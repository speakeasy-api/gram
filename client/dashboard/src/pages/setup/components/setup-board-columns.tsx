import { useState } from "react";
import type {
  SetupTask,
  SetupTaskStatus,
} from "@gram/client/models/components/setuptask.js";
import { cn } from "@/lib/utils";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SETUP_TASK_STATUS_LABELS, SetupTaskCard } from "./setup-task-card";

const SETUP_BOARD_STATUSES: SetupTaskStatus[] = [
  "todo",
  "in_progress",
  "awaiting_support",
  "done",
];

const STATUS_ACCENTS: Record<SetupTaskStatus, string> = {
  todo: "border-foreground",
  in_progress: "border-information-default",
  awaiting_support: "border-warning-default",
  done: "border-success-default",
};

type SetupBoardColumnsProps = {
  tasks: SetupTask[];
  allTasks: SetupTask[];
  currentUserId: string;
  currentUserEmail: string;
  canAdmin: boolean;
  isPlatformAdmin: boolean;
  pending: boolean;
  retryInviteKeys: Set<string>;
  onOpen: (task: SetupTask) => void;
  onStatusChange: (task: SetupTask, status: SetupTaskStatus) => void;
  onAssign: (task: SetupTask) => void;
  onRemind: (task: SetupTask) => void;
  onHiddenChange: (task: SetupTask, hidden: boolean) => void;
  onRetryInvite: (task: SetupTask) => void;
};

export function SetupBoardColumns({
  tasks,
  allTasks,
  currentUserId,
  currentUserEmail,
  canAdmin,
  isPlatformAdmin,
  pending,
  retryInviteKeys,
  onOpen,
  onStatusChange,
  onAssign,
  onRemind,
  onHiddenChange,
  onRetryInvite,
}: SetupBoardColumnsProps): JSX.Element {
  const [mobileStatus, setMobileStatus] = useState<SetupTaskStatus>("todo");
  const taskTitles = new Map(allTasks.map((task) => [task.key, task.title]));
  const counts = new Map(
    SETUP_BOARD_STATUSES.map((status) => [
      status,
      tasks.filter((task) => task.status === status).length,
    ]),
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-x-auto">
      <SegmentedControl
        value={mobileStatus}
        onChange={setMobileStatus}
        className="grid w-full grid-cols-4 md:hidden"
        options={[
          { value: "todo", label: `To do ${counts.get("todo")}` },
          {
            value: "in_progress",
            label: `Active ${counts.get("in_progress")}`,
          },
          {
            value: "awaiting_support",
            label: `Support ${counts.get("awaiting_support")}`,
          },
          { value: "done", label: `Done ${counts.get("done")}` },
        ]}
      />
      <div
        className="grid h-full min-h-80 grid-cols-1 gap-4 md:min-w-[1120px] md:grid-cols-4"
        aria-label="Setup board"
      >
        {SETUP_BOARD_STATUSES.map((status) => {
          const columnTasks = tasks.filter((task) => task.status === status);
          return (
            <section
              key={status}
              className={cn(
                "min-h-0 flex-col border bg-surface-secondary-default md:flex",
                mobileStatus === status ? "flex" : "hidden",
              )}
            >
              <header className="flex items-center justify-between border-b px-4 py-3">
                <h2
                  className={cn(
                    "border-l-2 pl-2 text-eyebrow",
                    STATUS_ACCENTS[status],
                  )}
                >
                  {SETUP_TASK_STATUS_LABELS[status]}
                </h2>
                <span className="font-mono text-xs text-muted-foreground">
                  {columnTasks.length}
                </span>
              </header>
              <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
                {columnTasks.length === 0 ? (
                  <p className="border border-dashed p-4 text-center text-sm text-muted-foreground">
                    No tasks
                  </p>
                ) : null}
                {columnTasks.map((task) => {
                  const assignedToCurrentUser =
                    task.assignee?.userId === currentUserId ||
                    task.assignee?.email.toLowerCase() ===
                      currentUserEmail.toLowerCase();
                  return (
                    <SetupTaskCard
                      key={task.key}
                      task={task}
                      blockedTitles={task.blockedBy.map(
                        (key) => taskTitles.get(key) ?? key,
                      )}
                      canChangeStatus={
                        task.blockedBy.length === 0 &&
                        (canAdmin || assignedToCurrentUser)
                      }
                      canAssign={canAdmin}
                      isPlatformAdmin={isPlatformAdmin}
                      pending={pending}
                      retryInvite={
                        retryInviteKeys.has(task.key)
                          ? () => onRetryInvite(task)
                          : undefined
                      }
                      onOpen={() => onOpen(task)}
                      onStatusChange={(nextStatus) =>
                        onStatusChange(task, nextStatus)
                      }
                      onAssign={() => onAssign(task)}
                      onRemind={() => onRemind(task)}
                      onHiddenChange={(hidden) => onHiddenChange(task, hidden)}
                    />
                  );
                })}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}
