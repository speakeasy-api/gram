import { useEffect, useRef } from "react";
import { useParams } from "react-router";
import type {
  SetupTask,
  SetupTaskStatus,
} from "@gram/client/models/components/setuptask.js";
import type { UpdateSetupTaskRequestBody } from "@gram/client/models/components/updatesetuptaskrequestbody.js";
import { invalidateAllListSetupTasks } from "@gram/client/react-query/listSetupTasks.js";
import { useUpdateSetupTaskMutation } from "@gram/client/react-query/updateSetupTask.js";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RequireScope } from "@/components/require-scope";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";
import { useOrganizationSetupTasks } from "@/hooks/useOrganizationSetupTasks";
import { showPylonChat } from "@/lib/pylon";
import { useOrgRoutes } from "@/routes";
import { JourneyLayout } from "./components/journey-layout";
import { OnboardingStepper, type Step } from "./components/onboarding-stepper";
import { SetupShell } from "./components/setup-shell";
import { SetupTaskContent } from "./components/setup-task-content";

// Every board card opens here instead of in a modal: the task's content sits
// in the wizard's linear frame, and the rail is a timeline of the whole
// journey drawn from the board's statuses rather than from position, so a
// task done out of order still shows as done and an owner still shows who.
export default function SetupTaskPage(): JSX.Element {
  return (
    <RequireScope scope="org:read" level="page">
      <SetupTaskPageInner />
    </RequireScope>
  );
}

function ownerLabel(task: SetupTask): string | undefined {
  if (!task.assignee) return undefined;
  return task.assignee.name ?? task.assignee.email;
}

function timelineStep(task: SetupTask): Step {
  return {
    id: task.key,
    title: task.title,
    description: task.description,
    status: task.status,
    meta: ownerLabel(task),
    badge: task.hidden ? "Hidden" : undefined,
  };
}

function SetupTaskPageInner(): JSX.Element {
  const { taskKey = "" } = useParams<{ taskKey: string }>();
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const isPlatformAdmin = useIsPlatformAdmin();
  const queryClient = useQueryClient();
  // Platform admins can arrive here from a hidden card, so their timeline
  // includes hidden tasks (marked as such); everyone else sees the board's
  // default set.
  const setupTasks = useOrganizationSetupTasks(
    organization.id,
    isPlatformAdmin,
    { retry: false },
  );
  const updateTask = useUpdateSetupTaskMutation();
  // The complete and support handlers await a round trip; a second click
  // while the first is in flight must not fire it again.
  const actionInFlight = useRef(false);

  const tasks = setupTasks.data?.tasks ?? [];
  const index = tasks.findIndex((task) => task.key === taskKey);
  const task = index === -1 ? undefined : tasks[index];

  const goToTask = (nextIndex: number) => {
    const next = tasks[nextIndex];
    if (next) orgRoutes.setupTask.goTo(next.key);
  };
  const goToBoard = () => orgRoutes.setup.goTo();
  const advance = () => {
    if (index + 1 < tasks.length) goToTask(index + 1);
    else goToBoard();
  };
  const goBack = () => {
    if (index > 0) goToTask(index - 1);
    else goToBoard();
  };

  const mutate = async (body: UpdateSetupTaskRequestBody) => {
    await updateTask.mutateAsync({
      request: { updateSetupTaskRequestBody: body },
    });
    await invalidateAllListSetupTasks(queryClient);
  };

  const guarded = async (action: () => Promise<void>) => {
    if (updateTask.isPending || actionInFlight.current) return;
    actionInFlight.current = true;
    try {
      await action();
    } finally {
      actionInFlight.current = false;
    }
  };

  const setStatus = async (
    status: SetupTaskStatus,
    fallback: string,
  ): Promise<boolean> => {
    if (!task) return false;
    try {
      await mutate({ taskKey: task.key, status });
      return true;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : fallback);
      return false;
    }
  };

  const complete = () =>
    guarded(async () => {
      if (!task) return;
      if (await setStatus("done", "Failed to complete setup task")) {
        toast.success(`${task.title} completed`);
        advance();
      }
    });

  const requestSupport = () =>
    guarded(async () => {
      if (await setStatus("awaiting_support", "Failed to request support")) {
        showPylonChat();
      }
    });

  // Deep links may name a task that no longer exists; send those to the board
  // once the list has loaded rather than leaving an empty frame.
  const listLoaded = setupTasks.isSuccess;
  useEffect(() => {
    if (listLoaded && !task) goToBoard();
    // goToBoard is a fresh closure each render; the effect only needs to run
    // when the list settles or the resolved task changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listLoaded, task]);

  const doneCount = tasks.filter((t) => t.status === "done").length;

  const rail = (
    <div>
      <orgRoutes.setup.Link className="text-muted-foreground hover:text-foreground mb-6 inline-flex items-center gap-1.5 text-sm">
        <ArrowLeft className="h-4 w-4" />
        Setup board
      </orgRoutes.setup.Link>
      <p className="text-eyebrow mb-4">
        {doneCount} of {tasks.length} complete
      </p>
      <OnboardingStepper
        steps={tasks.map(timelineStep)}
        currentStep={index}
        onStepClick={goToTask}
        allowJumpAhead
      />
    </div>
  );

  let content: JSX.Element | null = null;
  if (setupTasks.isError) {
    content = (
      <Alert variant="error">
        <div>
          <AlertTitle>Could not load setup tasks</AlertTitle>
          <AlertDescription>
            Try again, or return to the board.
          </AlertDescription>
          <div className="mt-3 flex gap-2">
            <Button
              variant="secondary"
              onClick={() => void setupTasks.refetch()}
            >
              Retry
            </Button>
            <Button variant="tertiary" onClick={goToBoard}>
              Setup board
            </Button>
          </div>
        </div>
      </Alert>
    );
  } else if (task) {
    content = (
      <SetupTaskContent
        // Remount when the route changes so per-task state (sheets, statuses)
        // starts fresh for the next task.
        key={task.key}
        taskKey={task.key}
        projectSlug="default"
        onComplete={() => void complete()}
        onSupport={() => void requestSupport()}
        onSkip={advance}
        onBack={goBack}
      />
    );
  }

  return (
    <SetupShell view="board">
      <JourneyLayout
        rail={rail}
        loading={setupTasks.isPending}
        skeletonRows={7}
      >
        {content}
      </JourneyLayout>
    </SetupShell>
  );
}
