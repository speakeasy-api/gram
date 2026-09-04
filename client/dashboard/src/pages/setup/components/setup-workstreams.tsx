import type {
  SetupTask,
  SetupTaskStatus,
} from "@gram/client/models/components/setuptask.js";
import { SetupTaskCard } from "./setup-task-card";

type Workstream = {
  id: string;
  title: string;
  description: string;
  taskKeys: string[];
};

const SETUP_WORKSTREAMS: Workstream[] = [
  {
    id: "connect",
    title: "Connect identity",
    description: "Authenticate people and sync access.",
    taskKeys: ["connect-idp", "directory-sync"],
  },
  {
    id: "observe",
    title: "Observe agents",
    description: "Instrument agents, add integrations, and verify traffic.",
    taskKeys: [
      "instrument-agents",
      "additional-agent-config",
      "confirm-traffic",
    ],
  },
  {
    id: "distribute",
    title: "Distribute MCP",
    description: "Publish and distribute approved MCP servers.",
    taskKeys: ["create-marketplace", "distribute-servers", "platform-mcp"],
  },
  {
    id: "secure",
    title: "Secure traffic",
    description: "Apply the initial policy controls.",
    taskKeys: ["configure-policies"],
  },
];

const OPTIONAL_TASK_KEYS = new Set(["platform-mcp"]);

type SetupWorkstreamsProps = {
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

export function SetupWorkstreams({
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
}: SetupWorkstreamsProps): JSX.Element {
  const visibleTasks = new Map(tasks.map((task) => [task.key, task]));
  const everyTask = new Map(allTasks.map((task) => [task.key, task]));
  const taskTitles = new Map(allTasks.map((task) => [task.key, task.title]));
  const configuredKeys = new Set(
    SETUP_WORKSTREAMS.flatMap((workstream) => workstream.taskKeys),
  );
  const ungroupedKeys = allTasks
    .filter((task) => !configuredKeys.has(task.key))
    .map((task) => task.key);
  const workstreams =
    ungroupedKeys.length > 0
      ? [
          ...SETUP_WORKSTREAMS,
          {
            id: "additional",
            title: "Additional setup",
            description: "Complete the remaining organization tasks.",
            taskKeys: ungroupedKeys,
          },
        ]
      : SETUP_WORKSTREAMS;

  return (
    <div
      role="region"
      aria-label="Setup workstreams"
      className="grid min-h-0 flex-1 items-start gap-4 overflow-y-auto pr-1 lg:grid-cols-2"
    >
      {tasks.length === 0 ? (
        <p className="border p-6 text-sm text-muted-foreground lg:col-span-2">
          No setup tasks match this view.
        </p>
      ) : null}
      {[0, 1].map((column) => (
        <div key={column} className="flex flex-col gap-2">
          {workstreams
            .filter((_, index) => index % 2 === column)
            .map((workstream) => {
              const workstreamTasks = workstream.taskKeys
                .map((key) => visibleTasks.get(key))
                .filter((task): task is SetupTask => task !== undefined);
              if (workstreamTasks.length === 0) return null;

              const requiredTasks = workstream.taskKeys
                .filter((key) => !OPTIONAL_TASK_KEYS.has(key))
                .map((key) => everyTask.get(key))
                .filter((task): task is SetupTask => task !== undefined);
              const completedTasks = requiredTasks.filter(
                (task) => task.status === "done",
              ).length;

              return (
                <section
                  key={workstream.id}
                  className="border bg-card"
                  aria-labelledby={`setup-workstream-${workstream.id}`}
                >
                  <header className="flex items-start justify-between gap-4 border-b bg-surface-secondary-default px-4 py-4">
                    <div className="min-w-0">
                      <h2
                        id={`setup-workstream-${workstream.id}`}
                        className="font-medium text-foreground"
                      >
                        {workstream.title}
                      </h2>
                      <p className="mt-1 text-sm text-muted-foreground">
                        {workstream.description}
                      </p>
                    </div>
                    <p className="shrink-0 text-xs font-medium tabular-nums text-muted-foreground">
                      {completedTasks} / {requiredTasks.length}
                    </p>
                  </header>
                  <div>
                    {workstreamTasks.map((task) => {
                      const assignedToCurrentUser =
                        task.assignee?.userId === currentUserId ||
                        task.assignee?.email.toLowerCase() ===
                          currentUserEmail.toLowerCase();
                      const canOpen = canAdmin || assignedToCurrentUser;

                      return (
                        <SetupTaskCard
                          key={task.key}
                          task={task}
                          optional={OPTIONAL_TASK_KEYS.has(task.key)}
                          blockedTitles={task.blockedBy.map(
                            (key) => taskTitles.get(key) ?? key,
                          )}
                          canChangeStatus={
                            task.blockedBy.length === 0 && canOpen
                          }
                          canOpen={canOpen}
                          canAssign={canAdmin}
                          isPlatformAdmin={isPlatformAdmin}
                          pending={pending}
                          retryInvite={
                            retryInviteKeys.has(task.key)
                              ? () => onRetryInvite(task)
                              : undefined
                          }
                          onOpen={() => onOpen(task)}
                          onStatusChange={(status) =>
                            onStatusChange(task, status)
                          }
                          onAssign={() => onAssign(task)}
                          onRemind={() => onRemind(task)}
                          onHiddenChange={(hidden) =>
                            onHiddenChange(task, hidden)
                          }
                        />
                      );
                    })}
                  </div>
                </section>
              );
            })}
        </div>
      ))}
    </div>
  );
}
