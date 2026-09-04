import { useState } from "react";
import type { SetupTaskStatus } from "@gram/client/models/components/setuptask.js";
import type { UpdateSetupTaskRequestBody } from "@gram/client/models/components/updatesetuptaskrequestbody.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useSendInviteMutation } from "@gram/client/react-query/sendInvite.js";
import { invalidateAllListSetupTasks } from "@gram/client/react-query/listSetupTasks.js";
import { useUpdateSetupTaskMutation } from "@gram/client/react-query/updateSetupTask.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { showPylonChat } from "@/lib/pylon";
import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { useOrganizationSetupTasks } from "@/hooks/useOrganizationSetupTasks";
import { useRBAC } from "@/hooks/useRBAC";
import { SetupBoardColumns } from "./components/setup-board-columns";
import { SetupTaskAssignmentDialog } from "./components/setup-task-assignment-dialog";
import { SetupTaskDialog } from "./components/setup-task-dialog";
import { SetupShell } from "./components/setup-shell";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";

type FailedInvite = { email: string; roleId: string };

function BoardPage({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <SetupShell>
      <main className="flex min-h-0 flex-1 overflow-hidden">
        <div className="@container/main mx-auto flex h-full min-h-0 w-full max-w-7xl flex-col gap-4 px-4 py-6 sm:px-6 lg:px-8 [&>div]:mb-0 [&>div]:min-h-0 [&>div]:flex-1">
          <Page.Section>
            <Page.Section.Title area="">Organization setup</Page.Section.Title>
            <Page.Section.Description>
              Assign and track the work required to prepare your organization.
            </Page.Section.Description>
            <Page.Section.Body>
              <div className="flex min-h-0 flex-1 flex-col">{children}</div>
            </Page.Section.Body>
          </Page.Section>
        </div>
      </main>
    </SetupShell>
  );
}

function BoardLoading(): JSX.Element {
  return (
    <BoardPage>
      <Skeleton>
        <div className="grid min-w-[1120px] grid-cols-4 gap-4 overflow-hidden">
          {[0, 1, 2, 3].map((column) => (
            <div key={column} className="h-80 border" />
          ))}
        </div>
      </Skeleton>
    </BoardPage>
  );
}

export default function SetupBoard(): JSX.Element {
  return (
    <RequireScope scope="org:read" level="page">
      <SetupBoardInner />
    </RequireScope>
  );
}

function SetupBoardInner(): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const [includeHidden, setIncludeHidden] = useState(false);
  const [showMine, setShowMine] = useState(false);
  const [selectedTask, setSelectedTask] = useState<SetupTask | null>(null);
  const [assignmentTask, setAssignmentTask] = useState<SetupTask | null>(null);
  const [failedInvites, setFailedInvites] = useState<
    Record<string, FailedInvite>
  >({});
  const setupTasks = useOrganizationSetupTasks(
    organization.id,
    isPlatformAdmin && includeHidden,
    { retry: false },
  );
  const members = useMembers(undefined, undefined, { retry: false });
  const roles = useRoles(undefined, undefined, { retry: false });
  const updateTask = useUpdateSetupTaskMutation();
  const sendInvite = useSendInviteMutation();
  const pending = updateTask.isPending || sendInvite.isPending;

  const refreshTasks = () => invalidateAllListSetupTasks(queryClient);

  const mutateTask = async (body: UpdateSetupTaskRequestBody) => {
    const task = await updateTask.mutateAsync({
      request: { updateSetupTaskRequestBody: body },
    });
    await refreshTasks();
    return task;
  };

  const handleMutationError = (error: unknown, fallback: string) => {
    toast.error(error instanceof Error ? error.message : fallback);
  };

  const updateStatus = async (task: SetupTask, status: SetupTaskStatus) => {
    try {
      await mutateTask({ taskKey: task.key, status });
    } catch (error) {
      handleMutationError(error, "Failed to update task status");
    }
  };

  const completeSelectedTask = async () => {
    if (!selectedTask) return;
    try {
      await mutateTask({ taskKey: selectedTask.key, status: "done" });
      setSelectedTask(null);
      toast.success(`${selectedTask.title} completed`);
    } catch (error) {
      handleMutationError(error, "Failed to complete setup task");
    }
  };

  const requestSupportForSelectedTask = async () => {
    if (!selectedTask) return;
    try {
      await mutateTask({
        taskKey: selectedTask.key,
        status: "awaiting_support",
      });
      setSelectedTask(null);
      showPylonChat();
    } catch (error) {
      handleMutationError(error, "Failed to request support");
    }
  };

  const assignMember = async (userId: string) => {
    if (!assignmentTask) return;
    try {
      await mutateTask({ taskKey: assignmentTask.key, assignee: { userId } });
      setAssignmentTask(null);
      toast.success("Task assigned");
    } catch (error) {
      handleMutationError(error, "Failed to assign task");
    }
  };

  const sendTaskInvite = async (taskKey: string, invite: FailedInvite) => {
    try {
      await sendInvite.mutateAsync({
        request: {
          sendInviteRequestBody: { email: invite.email, roleId: invite.roleId },
        },
      });
      setFailedInvites((current) => {
        const next = { ...current };
        delete next[taskKey];
        return next;
      });
      toast.success(`Invite sent to ${invite.email}`);
    } catch (error) {
      setFailedInvites((current) => ({ ...current, [taskKey]: invite }));
      toast.error(
        `Task assigned, but the invite failed. Retry the invite from the task card.${
          error instanceof Error ? ` ${error.message}` : ""
        }`,
      );
    }
  };

  const assignEmail = async (email: string, inviteRoleId?: string) => {
    if (!assignmentTask) return;
    const task = assignmentTask;
    try {
      await mutateTask({ taskKey: task.key, assignee: { email } });
      setAssignmentTask(null);
      toast.success("Task assigned");
      if (inviteRoleId)
        await sendTaskInvite(task.key, { email, roleId: inviteRoleId });
    } catch (error) {
      handleMutationError(error, "Failed to assign task");
    }
  };

  const unassign = async () => {
    if (!assignmentTask) return;
    try {
      await mutateTask({ taskKey: assignmentTask.key, clearAssignee: true });
      setAssignmentTask(null);
      toast.success("Task unassigned");
    } catch (error) {
      handleMutationError(error, "Failed to unassign task");
    }
  };

  const changeHidden = async (task: SetupTask, hidden: boolean) => {
    try {
      await mutateTask({ taskKey: task.key, hidden });
      toast.success(hidden ? "Task hidden" : "Task restored");
    } catch (error) {
      handleMutationError(
        error,
        hidden ? "Failed to hide task" : "Failed to restore task",
      );
    }
  };

  if (setupTasks.isPending) return <BoardLoading />;

  if (setupTasks.isError) {
    return (
      <BoardPage>
        <Alert variant="error">
          <div>
            <AlertTitle>Could not load setup tasks</AlertTitle>
            <AlertDescription>Refresh the board to try again.</AlertDescription>
            <Button
              className="mt-3"
              variant="secondary"
              onClick={() => void setupTasks.refetch()}
            >
              Retry
            </Button>
          </div>
        </Alert>
      </BoardPage>
    );
  }

  const tasks = setupTasks.data?.tasks ?? [];
  const visibleTasks = showMine
    ? tasks.filter(
        (task) =>
          task.assignee?.userId === session.user.id ||
          task.assignee?.email.toLowerCase() ===
            session.user.email.toLowerCase(),
      )
    : tasks;
  const completedCount = tasks.filter((task) => task.status === "done").length;
  return (
    <BoardPage>
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Text small className="whitespace-nowrap">
            {completedCount} of {tasks.length} tasks complete
          </Text>
          <div className="flex flex-wrap items-center gap-5">
            <div className="flex items-center gap-3">
              <label id="my-tasks-label" className="text-sm font-medium">
                My tasks
              </label>
              <Switch
                checked={showMine}
                onCheckedChange={setShowMine}
                aria-labelledby="my-tasks-label"
              />
            </div>
            {isPlatformAdmin ? (
              <div className="flex items-center gap-3">
                <label
                  id="include-hidden-label"
                  className="text-sm font-medium"
                >
                  Show hidden tasks
                </label>
                <Switch
                  checked={includeHidden}
                  onCheckedChange={setIncludeHidden}
                  aria-labelledby="include-hidden-label"
                  disabled={pending}
                />
              </div>
            ) : null}
          </div>
        </div>
        <SetupBoardColumns
          tasks={visibleTasks}
          allTasks={tasks}
          currentUserId={session.user.id}
          currentUserEmail={session.user.email}
          canAdmin={canAdmin}
          isPlatformAdmin={isPlatformAdmin}
          pending={pending}
          retryInviteKeys={new Set(Object.keys(failedInvites))}
          onOpen={setSelectedTask}
          onStatusChange={(task, status) => void updateStatus(task, status)}
          onAssign={setAssignmentTask}
          onRemind={() => {
            toast.info("Reminder delivery is not available yet");
          }}
          onHiddenChange={(task, hidden) => void changeHidden(task, hidden)}
          onRetryInvite={(task) => {
            const invite = failedInvites[task.key];
            if (invite) void sendTaskInvite(task.key, invite);
          }}
        />
      </div>
      <SetupTaskDialog
        task={selectedTask}
        pending={pending}
        onClose={() => setSelectedTask(null)}
        onComplete={completeSelectedTask}
        onSupport={requestSupportForSelectedTask}
        onSkip={() => setSelectedTask(null)}
      />
      <SetupTaskAssignmentDialog
        key={assignmentTask?.key ?? "closed"}
        task={assignmentTask}
        members={members.data?.members ?? []}
        roles={roles.data?.roles ?? []}
        pending={pending}
        onClose={() => setAssignmentTask(null)}
        onAssignMember={(userId) => void assignMember(userId)}
        onAssignEmail={(email, roleId) => void assignEmail(email, roleId)}
        onUnassign={() => void unassign()}
      />
    </BoardPage>
  );
}
