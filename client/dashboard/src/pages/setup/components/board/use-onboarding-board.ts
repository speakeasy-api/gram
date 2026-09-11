import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useUpdateSetupTaskMutation } from "@gram/client/react-query/updateSetupTask.js";
import type { UpdateSetupTaskRequestBody } from "@gram/client/models/components/updatesetuptaskrequestbody.js";
import { useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import {
  buildOrganizationSetupTasksQuery,
  invalidateOrganizationSetupTasks,
} from "@/hooks/useOrganizationSetupTasks";
import {
  assignedTo,
  resolveBoardTasks,
  type Assignee,
  type BoardTask,
} from "./board-store";
import type { OnboardingTaskId, TaskStatus } from "./tasks";

export interface OnboardingBoardState {
  tasks: BoardTask[];
  error: string | undefined;
  writeError: string | null;
  isLoading: boolean;
  isPending: boolean;
  retry: () => Promise<unknown>;
  canAssign: boolean;
  canHideTasks: boolean;
  canSetStatus: (task: BoardTask) => boolean;
  setStatus: (id: OnboardingTaskId, status: TaskStatus) => Promise<boolean>;
  assign: (
    id: OnboardingTaskId,
    owner: Assignee | undefined,
  ) => Promise<boolean>;
  setHidden: (id: OnboardingTaskId, hidden: boolean) => Promise<boolean>;
}

export function useOnboardingBoard(): OnboardingBoardState {
  const session = useSession();
  const organizationId = session.organization.id;
  const client = useGramContext();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canAssign = hasScope("org:admin", organizationId);
  const canHideTasks = session.user.isAdmin;
  const query = useQuery(
    buildOrganizationSetupTasksQuery(client, organizationId, canHideTasks, {
      retry: false,
    }),
  );
  const mutation = useUpdateSetupTaskMutation();
  const inFlight = useRef(false);
  const [writeError, setWriteError] = useState<string | null>(null);
  let tasks: BoardTask[] = [];
  let error = query.error?.message;
  try {
    tasks = resolveBoardTasks(query.data?.tasks ?? []);
  } catch (cause) {
    error =
      cause instanceof Error ? cause.message : "Could not read setup tasks";
  }
  const canSetStatus = (task: BoardTask) =>
    !task.verified && (canAssign || assignedTo(task, session.user));
  const update = async (body: UpdateSetupTaskRequestBody): Promise<boolean> => {
    if (inFlight.current || error || query.isPending) return false;
    inFlight.current = true;
    setWriteError(null);
    try {
      await mutation.mutateAsync({
        request: { updateSetupTaskRequestBody: body },
      });
      await invalidateOrganizationSetupTasks(queryClient, organizationId);
      return true;
    } catch (cause) {
      setWriteError(
        cause instanceof Error
          ? cause.message
          : "Could not save task. Try again.",
      );
      return false;
    } finally {
      inFlight.current = false;
    }
  };
  return {
    tasks,
    error,
    writeError,
    isLoading: query.isPending,
    isPending: mutation.isPending,
    retry: () => query.refetch(),
    canAssign,
    canHideTasks,
    canSetStatus,
    setStatus: (id: OnboardingTaskId, status: TaskStatus) => {
      const task = tasks.find((item) => item.id === id);
      if (
        !task ||
        !canSetStatus(task) ||
        (status !== "todo" && task.blockedBy.length > 0)
      )
        return Promise.resolve(false);
      return update({ taskKey: id, status });
    },
    assign: (id: OnboardingTaskId, owner: Assignee | undefined) => {
      if (!canAssign) return Promise.resolve(false);
      if (!owner) return update({ taskKey: id, clearAssignee: true });
      const assignee =
        owner.kind === "user"
          ? { userId: owner.userId }
          : { email: owner.email };
      return update({ taskKey: id, assignee });
    },
    setHidden: (id: OnboardingTaskId, hidden: boolean) =>
      canHideTasks ? update({ taskKey: id, hidden }) : Promise.resolve(false),
  };
}
