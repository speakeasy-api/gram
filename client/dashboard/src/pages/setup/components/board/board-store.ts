import type { OnboardingStatusResult } from "@gram/client/models/components/onboardingstatusresult.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { createScopedStorageStore } from "@/hooks/useDismissedCtaStore";
import {
  ONBOARDING_TASKS,
  type OnboardingTaskDefinition,
  type OnboardingTaskId,
  type TaskStatus,
} from "./tasks";

/**
 * Who a task is handed to: a member of the organization, or an email address
 * for someone who has not joined yet.
 */
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

/** Everything the board remembers about one task. Absent fields are defaults. */
export interface TaskRecord {
  status?: TaskStatus;
  assignee?: Assignee;
  /** Platform admins can take a task off the board for this organization. */
  hidden?: boolean;
  /** ISO timestamp of the last reminder sent for this task. */
  lastRemindedAt?: string;
}

export type BoardState = Partial<Record<OnboardingTaskId, TaskRecord>>;

const EMPTY_STATE: BoardState = {};

function decodeBoardState(stored: string | null): BoardState {
  if (!stored) return EMPTY_STATE;
  try {
    const parsed: unknown = JSON.parse(stored);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as BoardState;
    }
  } catch {
    // Corrupt entry — start over rather than wedge the board.
  }
  return EMPTY_STATE;
}

function encodeBoardState(value: BoardState): string | null {
  return Object.keys(value).length === 0 ? null : JSON.stringify(value);
}

/**
 * Board state is scoped to the organization slug and kept in localStorage for
 * now, the same way the wizard remembered its own progress. Moving it behind a
 * management API is the follow-up that makes assignments visible to the whole
 * team; nothing in the UI depends on where the state lives.
 */
export const onboardingBoardStore = createScopedStorageStore<BoardState>(
  "gram-onboarding-board",
  EMPTY_STATE,
  decodeBoardState,
  encodeBoardState,
);

export interface BoardTask extends OnboardingTaskDefinition {
  status: TaskStatus;
  /** Completion was confirmed by the server, so the status is locked to Done. */
  verified: boolean;
  hidden: boolean;
  assignee?: Assignee;
  lastRemindedAt?: Date;
}

/**
 * Tasks the server can vouch for. SSO and directory sync come from the
 * organization's WorkOS state and the marketplace from the project's GitHub
 * connection; the remaining tasks have no server signal and rely on the
 * status people set on the board.
 */
export function verifiedTaskIds(
  onboardingStatus: OnboardingStatusResult | undefined,
  publishStatus: PublishStatusResult | undefined,
): Set<OnboardingTaskId> {
  const ids = new Set<OnboardingTaskId>();
  if (onboardingStatus?.ssoConfigured) ids.add("connect-idp");
  if (onboardingStatus?.dsyncConfigured) ids.add("directory-sync");
  if (publishStatus?.connected) ids.add("create-marketplace");
  return ids;
}

export function resolveBoardTasks(
  state: BoardState,
  verifiedIds: ReadonlySet<OnboardingTaskId>,
): BoardTask[] {
  return ONBOARDING_TASKS.map((definition) => {
    const record = state[definition.id] ?? {};
    const verified = verifiedIds.has(definition.id);
    return {
      ...definition,
      status: verified ? "done" : (record.status ?? "todo"),
      verified,
      hidden: record.hidden === true,
      assignee: record.assignee,
      lastRemindedAt: record.lastRemindedAt
        ? new Date(record.lastRemindedAt)
        : undefined,
    };
  });
}
