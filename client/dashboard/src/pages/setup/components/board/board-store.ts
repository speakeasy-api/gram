import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { ONBOARDING_TASKS, type OnboardingTaskDefinition } from "./tasks";

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

export interface BoardTask extends OnboardingTaskDefinition {
  title: string;
  description: string;
  status: SetupTask["status"];
  verified: boolean;
  hidden: boolean;
  blockedBy: string[];
  assignee?: Assignee;
}

export function resolveBoardTasks(tasks: SetupTask[]): BoardTask[] {
  return tasks.map((task) => {
    const metadata = ONBOARDING_TASKS.find((item) => item.id === task.key);
    if (!metadata) throw new Error(`Unsupported setup task: ${task.key}`);
    const assignee = task.assignee;
    let owner: Assignee | undefined;
    if (assignee?.userId) {
      owner = {
        kind: "user",
        userId: assignee.userId,
        name: assignee.name ?? assignee.email,
        email: assignee.email,
        photoUrl: assignee.photoUrl,
      };
    } else if (assignee) {
      owner = { kind: "email", email: assignee.email };
    }
    return {
      ...metadata,
      title: task.title,
      description: task.description,
      status: task.status,
      verified: task.completedByFact,
      hidden: task.hidden,
      blockedBy: task.blockedBy,
      assignee: owner,
    };
  });
}

export function assignedTo(
  task: BoardTask,
  user: { id: string; email: string },
): boolean {
  const owner = task.assignee;
  return Boolean(
    owner &&
    ((owner.kind === "user" && owner.userId === user.id) ||
      owner.email.trim().toLowerCase() === user.email.trim().toLowerCase()),
  );
}
