import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";
import {
  fallbackTaskTitle,
  isOnboardingTaskId,
  suggestedOwnerFor,
  workstreamPresentation,
  type TaskStatus,
} from "./onboarding-tasks";

export type Assignee =
  | {
      kind: "user";
      userId: string;
      name: string;
      email: string;
      photoUrl?: string;
    }
  | { kind: "email"; email: string };

export function assigneeLabel(assignee: Assignee): string {
  return assignee.kind === "user" ? assignee.name : assignee.email;
}

export function assigneeIdentity(assignee: Assignee): string {
  return assignee.kind === "user" ? assignee.userId : assignee.email;
}

export interface OnboardingTask {
  id: string;
  /** False when this dashboard version has no renderer for the key. */
  supported: boolean;
  title: string;
  description: string;
  status: TaskStatus;
  /** Server facts force the task done; its status cannot be changed. */
  verified: boolean;
  countsTowardProgress: boolean;
  /** Derived from progress membership, never the other way around. */
  badge?: string;
  hidden: boolean;
  blockedBy: string[];
  suggestedOwner: string;
  assignee?: Assignee;
}

export interface OnboardingWorkstream {
  id: string;
  title: string;
  description: string;
  suggestedOwner: string;
  /** Authorized members in display order, hidden tasks included when readable. */
  tasks: OnboardingTask[];
}

export interface OnboardingProgress {
  done: number;
  total: number;
}

export interface OnboardingModel {
  /** Every authorized task, in canonical (workstream) order. */
  tasks: OnboardingTask[];
  /** Tasks on the board and in the walk; hidden tasks excluded. */
  visibleTasks: OnboardingTask[];
  workstreams: OnboardingWorkstream[];
  unsupportedTaskKeys: string[];
  progress: OnboardingProgress;
  task: (id: string) => OnboardingTask | undefined;
  /** Title for a dependency key, from server metadata where available. */
  titleFor: (key: string) => string;
}

function toAssignee(assignee: SetupTask["assignee"]): Assignee | undefined {
  if (!assignee) return undefined;
  if (assignee.userId) {
    return {
      kind: "user",
      userId: assignee.userId,
      name: assignee.name ?? assignee.email,
      email: assignee.email,
      photoUrl: assignee.photoUrl,
    };
  }
  return { kind: "email", email: assignee.email };
}

function toTask(task: SetupTask): OnboardingTask {
  return {
    id: task.key,
    supported: isOnboardingTaskId(task.key),
    title: task.title,
    description: task.description,
    status: task.status,
    verified: task.completedByFact,
    countsTowardProgress: task.countsTowardProgress,
    badge: task.countsTowardProgress ? undefined : "Optional",
    hidden: task.hidden,
    blockedBy: task.blockedBy,
    suggestedOwner: suggestedOwnerFor(task.key),
    assignee: toAssignee(task.assignee),
  };
}

export function isTaskDone(task: OnboardingTask): boolean {
  return task.verified || task.status === "done";
}

/** Progress over non-hidden tasks that the server counts. */
export function progressOf(tasks: OnboardingTask[]): OnboardingProgress {
  const counted = tasks.filter(
    (task) => !task.hidden && task.countsTowardProgress,
  );
  return { done: counted.filter(isTaskDone).length, total: counted.length };
}

export function assignedTo(
  task: OnboardingTask,
  user: { id: string; email: string },
): boolean {
  const owner = task.assignee;
  return Boolean(
    owner &&
    ((owner.kind === "user" && owner.userId === user.id) ||
      owner.email.trim().toLowerCase() === user.email.trim().toLowerCase()),
  );
}

/**
 * The single projection of a listSetupTasks response that both the board and
 * the wizard render. Workstream membership from the API is the canonical
 * order; the task array's own order is not used for display.
 */
export function buildOnboardingModel(
  setupTasks: SetupTask[],
  setupWorkstreams: SetupWorkstream[],
): OnboardingModel {
  const byId = new Map(setupTasks.map((task) => [task.key, toTask(task)]));
  const placed = new Set<string>();
  const workstreams = setupWorkstreams.map((workstream) => {
    const tasks = workstream.taskKeys.flatMap((key) => {
      const task = byId.get(key);
      if (!task || placed.has(key)) return [];
      placed.add(key);
      return [task];
    });
    return {
      id: workstream.id,
      title: workstream.title,
      ...workstreamPresentation(workstream.id),
      tasks,
    };
  });
  // The server partitions every task into a workstream; anything it misses
  // still appears, after the workstreams, instead of silently disappearing.
  const tasks = [
    ...workstreams.flatMap((workstream) => workstream.tasks),
    ...[...byId.values()].filter((task) => !placed.has(task.id)),
  ];
  return {
    tasks,
    visibleTasks: tasks.filter((task) => !task.hidden),
    workstreams,
    unsupportedTaskKeys: tasks
      .filter((task) => !task.supported)
      .map((task) => task.id),
    progress: progressOf(tasks),
    task: (id) => byId.get(id),
    titleFor: (key) => byId.get(key)?.title ?? fallbackTaskTitle(key),
  };
}
