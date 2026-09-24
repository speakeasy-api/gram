import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAssignSetupWorkstreamMutation } from "@gram/client/react-query/assignSetupWorkstream.js";
import { isOnboardingTaskId, resolveWorkstreams } from "./tasks";
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
import type {
  OnboardingWorkstreamDefinition,
  OnboardingTaskId,
  TaskStatus,
} from "./tasks";

export interface OnboardingBoardState {
  tasks: BoardTask[];
  workstreams: OnboardingWorkstreamDefinition[];
  unsupportedTaskKeys: string[];
  error: string | undefined;
  writeError: string | null;
  writeErrorTaskId: string | null;
  isLoading: boolean;
  isPending: boolean;
  retry: () => Promise<unknown>;
  canAssign: boolean;
  canHideTasks: boolean;
  canSetStatus: (task: BoardTask) => boolean;
  setStatus: (id: OnboardingTaskId, status: TaskStatus) => Promise<boolean>;
  assignWorkstream: (
    workstream: OnboardingWorkstreamDefinition,
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
  const assignMutation = useAssignSetupWorkstreamMutation();
  const inFlight = useRef(false);
  const [isPending, setIsPending] = useState(false);
  const [writeError, setWriteError] = useState<string | null>(null);
  const [writeErrorTaskId, setWriteErrorTaskId] = useState<string | null>(null);
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
    setIsPending(true);
    setWriteError(null);
    setWriteErrorTaskId(null);
    try {
      try {
        await mutation.mutateAsync({
          request: { updateSetupTaskRequestBody: body },
        });
      } catch (cause) {
        // The server may have committed before the response was lost. Refresh
        // without replacing the original write error if that read also fails.
        await invalidateOrganizationSetupTasks(
          queryClient,
          organizationId,
        ).catch(() => undefined);
        throw cause;
      }
      await invalidateOrganizationSetupTasks(queryClient, organizationId);
      return true;
    } catch (cause) {
      setWriteErrorTaskId(body.taskKey);
      setWriteError(
        cause instanceof Error
          ? cause.message
          : "Could not save task. Try again.",
      );
      return false;
    } finally {
      inFlight.current = false;
      setIsPending(false);
    }
  };
  return {
    tasks,
    workstreams: resolveWorkstreams(query.data?.workstreams ?? []),
    unsupportedTaskKeys: (query.data?.tasks ?? [])
      .filter((task) => !isOnboardingTaskId(task.key))
      .map((task) => task.key),
    error,
    writeError,
    writeErrorTaskId,
    isLoading: query.isPending,
    isPending,
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
    assignWorkstream: async (workstream, owner) => {
      if (!canAssign || inFlight.current || error || query.isPending)
        return false;
      inFlight.current = true;
      setIsPending(true);
      setWriteError(null);
      setWriteErrorTaskId(null);
      try {
        let saved = false;
        let message: string | null = null;
        try {
          await assignMutation.mutateAsync({
            request: {
              assignSetupWorkstreamRequestBody: {
                workstream: workstream.id,
                ...(owner
                  ? {
                      assignee:
                        owner.kind === "user"
                          ? { userId: owner.userId }
                          : { email: owner.email },
                    }
                  : { clearAssignee: true }),
              },
            },
          });
          saved = true;
        } catch (cause) {
          message =
            cause instanceof Error
              ? cause.message
              : "Could not assign workstream. Try again.";
        }
        // Refresh even after an ambiguous network failure; the server may have saved.
        try {
          await invalidateOrganizationSetupTasks(queryClient, organizationId);
        } catch {
          message = `${message ?? "Assignment saved."} Could not refresh the board. Reload to see current assignments.`;
        }
        setWriteError(message);
        return saved;
      } finally {
        inFlight.current = false;
        setIsPending(false);
      }
    },
    setHidden: (id: OnboardingTaskId, hidden: boolean) =>
      canHideTasks ? update({ taskKey: id, hidden }) : Promise.resolve(false),
  };
}
