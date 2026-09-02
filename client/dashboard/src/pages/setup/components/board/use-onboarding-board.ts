import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import {
  type Assignee,
  type BoardTask,
  onboardingBoardStore,
  resolveBoardTasks,
  type TaskRecord,
  verifiedTaskIds,
} from "./board-store";
import { sendTaskReminder } from "./reminders";
import type { OnboardingTaskId, TaskStatus } from "./tasks";

export interface OnboardingBoardActions {
  setStatus: (id: OnboardingTaskId, status: TaskStatus) => void;
  assign: (id: OnboardingTaskId, assignee: Assignee | undefined) => void;
  setHidden: (id: OnboardingTaskId, hidden: boolean) => void;
  remind: (id: OnboardingTaskId) => void;
}

export interface OnboardingBoard extends OnboardingBoardActions {
  tasks: BoardTask[];
  /** True while the server signals that lock verified tasks are loading. */
  isLoading: boolean;
  /** Whether the viewer may hide tasks from the board and see hidden ones. */
  canHideTasks: boolean;
  /** The task whose reminder is in flight, if any. */
  remindingTaskId: OnboardingTaskId | null;
}

export function useOnboardingBoard(
  orgSlug: string | undefined,
): OnboardingBoard {
  const state = onboardingBoardStore.useValue(orgSlug);
  const { data: onboardingStatus, isLoading: isOnboardingStatusLoading } =
    useOnboardingStatus();
  const { data: publishStatus, isLoading: isPublishStatusLoading } =
    usePublishStatus();
  const isPlatformAdmin = useIsPlatformAdmin();
  const [remindingTaskId, setRemindingTaskId] =
    useState<OnboardingTaskId | null>(null);

  const tasks = useMemo(
    () =>
      resolveBoardTasks(
        state,
        verifiedTaskIds(onboardingStatus, publishStatus),
      ),
    [state, onboardingStatus, publishStatus],
  );

  const updateTask = useCallback(
    (id: OnboardingTaskId, patch: TaskRecord) => {
      if (!orgSlug) return;
      // Read at write time rather than closing over `state`: `remind` patches
      // after an await, by which point the board may have moved on.
      const current = onboardingBoardStore.read(orgSlug);
      onboardingBoardStore.write(orgSlug, {
        ...current,
        [id]: { ...current[id], ...patch },
      });
    },
    [orgSlug],
  );

  const setStatus = useCallback(
    (id: OnboardingTaskId, status: TaskStatus) => updateTask(id, { status }),
    [updateTask],
  );

  const assign = useCallback(
    (id: OnboardingTaskId, assignee: Assignee | undefined) =>
      updateTask(id, { assignee }),
    [updateTask],
  );

  const setHidden = useCallback(
    (id: OnboardingTaskId, hidden: boolean) => updateTask(id, { hidden }),
    [updateTask],
  );

  const remind = useCallback(
    (id: OnboardingTaskId) => {
      const task = tasks.find((candidate) => candidate.id === id);
      if (!task?.assignee || remindingTaskId) return;
      setRemindingTaskId(id);
      void sendTaskReminder({ taskTitle: task.title, assignee: task.assignee })
        .then(({ recipient }) => {
          updateTask(id, { lastRemindedAt: new Date().toISOString() });
          toast.success(`Reminder sent to ${recipient}`);
        })
        .catch((error: unknown) => {
          toast.error(
            error instanceof Error ? error.message : "Failed to send reminder",
          );
        })
        .finally(() => setRemindingTaskId(null));
    },
    [tasks, remindingTaskId, updateTask],
  );

  return {
    tasks,
    isLoading: isOnboardingStatusLoading || isPublishStatusLoading,
    // Mirrors PlatformAdminGate: local dev always unlocks admin affordances so
    // the hidden-task flow can be exercised without a platform-admin account.
    canHideTasks: import.meta.env.DEV || isPlatformAdmin,
    remindingTaskId,
    setStatus,
    assign,
    setHidden,
    remind,
  };
}
