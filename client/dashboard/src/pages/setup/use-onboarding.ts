import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAssignSetupWorkstreamMutation } from "@gram/client/react-query/assignSetupWorkstream.js";
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
  buildOnboardingModel,
  type Assignee,
  type OnboardingModel,
  type OnboardingTask,
} from "./onboarding-model";
import type { TaskStatus } from "./onboarding-tasks";

export interface Onboarding {
  model: OnboardingModel;
  error: string | undefined;
  isLoading: boolean;
  retry: () => Promise<unknown>;
  /** Organization admins open tasks and assign workstreams. */
  canAssign: boolean;
  /** Platform staff read and change hidden tasks. */
  canInspectHidden: boolean;
}

export function useOnboarding({
  includeHidden,
}: { includeHidden?: boolean } = {}): Onboarding {
  const session = useSession();
  const organizationId = session.organization.id;
  const client = useGramContext();
  const { hasScope } = useRBAC();
  const canInspectHidden = session.user.isAdmin;
  const query = useQuery(
    buildOrganizationSetupTasksQuery(
      client,
      organizationId,
      includeHidden ?? canInspectHidden,
      {
        retry: false,
      },
    ),
  );
  return {
    model: buildOnboardingModel(
      query.data?.tasks ?? [],
      query.data?.workstreams ?? [],
    ),
    error: query.error?.message,
    isLoading: query.isPending,
    retry: () => query.refetch(),
    canAssign: hasScope("org:admin", organizationId),
    canInspectHidden,
  };
}

/**
 * What happened to a write. `rejected` means nothing was sent; `failed` may
 * still have committed server-side, so the task list was refreshed anyway;
 * `saved_stale` committed but the refresh after it failed.
 */
export type OnboardingWriteOutcome =
  | { status: "saved" }
  | { status: "saved_stale"; message: string }
  | { status: "failed"; message: string }
  | {
      status: "rejected";
      reason: "busy" | "unavailable" | "forbidden" | "blocked";
    };

export interface OnboardingActions {
  /** A write, including the refresh after it, is in flight. */
  isPending: boolean;
  canSetStatus: (task: OnboardingTask) => boolean;
  setStatus: (
    id: string,
    status: TaskStatus,
  ) => Promise<OnboardingWriteOutcome>;
  setHidden: (id: string, hidden: boolean) => Promise<OnboardingWriteOutcome>;
  assignWorkstream: (
    workstreamId: string,
    owner: Assignee | undefined,
  ) => Promise<OnboardingWriteOutcome>;
}

const REFRESH_FAILED =
  "Could not refresh setup tasks. Reload to see the current state.";

function messageOf(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

export function useOnboardingActions(
  onboarding: Onboarding,
): OnboardingActions {
  const session = useSession();
  const organizationId = session.organization.id;
  const queryClient = useQueryClient();
  const updateTask = useUpdateSetupTaskMutation();
  const assign = useAssignSetupWorkstreamMutation();
  // The ref blocks re-entry within a render; the state is what controls read,
  // and it spans the mutation and the refresh after it.
  const inFlight = useRef(false);
  const [isPending, setIsPending] = useState(false);
  const { model, canAssign, canInspectHidden } = onboarding;

  const canSetStatus = (task: OnboardingTask) =>
    !task.verified && (canAssign || assignedTo(task, session.user));

  const run = async (
    write: () => Promise<unknown>,
    fallback: string,
  ): Promise<OnboardingWriteOutcome> => {
    if (inFlight.current) return { status: "rejected", reason: "busy" };
    if (onboarding.isLoading || onboarding.error)
      return { status: "rejected", reason: "unavailable" };
    inFlight.current = true;
    setIsPending(true);
    try {
      let failure: string | undefined;
      try {
        await write();
      } catch (cause) {
        failure = messageOf(cause, fallback);
      }
      // Refresh even after a failed write: the server may have committed
      // before the response was lost.
      try {
        await invalidateOrganizationSetupTasks(queryClient, organizationId);
      } catch {
        if (!failure) return { status: "saved_stale", message: REFRESH_FAILED };
      }
      return failure
        ? { status: "failed", message: failure }
        : { status: "saved" };
    } finally {
      inFlight.current = false;
      setIsPending(false);
    }
  };

  const update = (body: UpdateSetupTaskRequestBody) =>
    run(
      () =>
        updateTask.mutateAsync({
          request: { updateSetupTaskRequestBody: body },
        }),
      "Could not save task. Try again.",
    );

  return {
    isPending: isPending || updateTask.isPending || assign.isPending,
    canSetStatus,
    setStatus: async (id, status) => {
      const task = model.task(id);
      if (!task || !canSetStatus(task))
        return { status: "rejected", reason: "forbidden" };
      if (status !== "todo" && task.blockedBy.length > 0)
        return { status: "rejected", reason: "blocked" };
      return update({ taskKey: id, status });
    },
    setHidden: async (id, hidden) =>
      canInspectHidden
        ? update({ taskKey: id, hidden })
        : { status: "rejected", reason: "forbidden" },
    assignWorkstream: async (workstreamId, owner) => {
      if (!canAssign) return { status: "rejected", reason: "forbidden" };
      return run(
        () =>
          assign.mutateAsync({
            request: {
              assignSetupWorkstreamRequestBody: {
                workstream: workstreamId,
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
          }),
        "Could not assign workstream. Try again.",
      );
    },
  };
}
