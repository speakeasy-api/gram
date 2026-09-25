import { useEffect, useRef } from "react";
import { useNavigate, useParams } from "react-router";
import type { SetupTaskStatus } from "@gram/client/models/components/setuptask.js";
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
import { cn } from "@/lib/utils";
import { JourneyLayout } from "./components/journey-layout";
import { JourneyStepsProvider } from "./components/journey-steps-provider";
import { useJourneyView } from "./components/journey-steps";
import { OnboardingStepper, type Step } from "./components/onboarding-stepper";
import { SetupShell } from "./components/setup-shell";
import { SetupTaskContent } from "./components/setup-task-content";
import { setupTaskKeyForSlug } from "./task-slugs";

// Admin-only, like the board: every flow on these cards ends in an action
// that needs org:admin (launching the WorkOS portal, publishing the
// marketplace, distributing servers), so a reader without it could open a
// card but never finish one.
//
// One board card, on its own page. The rail lists only this card's own steps
// (the sections it renders), each ticking off as its outcome lands. The
// board stays the map of the whole journey; this page is one stop on it.
export default function SetupTaskPage(): JSX.Element {
  const { taskSlug = "" } = useParams<{ taskSlug: string }>();

  return (
    <RequireScope scope="org:admin" level="page">
      {/* Keyed by the card: each one has its own steps, so carrying the
          previous card's active step into the next would land the reader on
          an unrelated section. */}
      <JourneyStepsProvider key={taskSlug}>
        <SetupTaskPageInner />
      </JourneyStepsProvider>
    </RequireScope>
  );
}

// The rail is hidden below md, and it holds the only way back to the board,
// so the link is rendered again above the content there.
function BoardLink({ className }: { className?: string }): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <orgRoutes.setup.Link
      className={cn(
        "text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm",
        className,
      )}
    >
      <ArrowLeft className="h-4 w-4" />
      Setup board
    </orgRoutes.setup.Link>
  );
}

function StepsRail({
  taskTitle,
  taskComplete,
}: {
  taskTitle: string;
  taskComplete: boolean;
}): JSX.Element {
  const { steps, activeIndex, setActiveIndex } = useJourneyView();
  // A card with no sub-steps still gets a rail entry so the page reads the
  // same way as its siblings.
  const railSteps: Step[] =
    steps.length > 0
      ? steps.map((step) => ({
          id: String(step.index),
          title: step.title,
          description: step.complete ? "Done" : "",
          badge: step.badge,
          status: step.complete ? "done" : undefined,
        }))
      : [
          {
            id: "task",
            title: taskTitle,
            description: taskComplete ? "Done" : "",
            status: taskComplete ? ("done" as const) : undefined,
          },
        ];
  const currentStep = steps.findIndex((step) => step.index === activeIndex);

  return (
    <div>
      <BoardLink className="mb-6" />
      <p className="text-eyebrow mb-4">
        {railSteps.filter((step) => step.status === "done").length} of{" "}
        {railSteps.length} complete
      </p>
      <OnboardingStepper
        steps={railSteps}
        currentStep={currentStep === -1 ? 0 : currentStep}
        onStepClick={(position) => {
          const step = steps[position];
          if (step) setActiveIndex(step.index);
        }}
      />
    </div>
  );
}

function SetupTaskPageInner(): JSX.Element {
  const { taskSlug = "" } = useParams<{ taskSlug: string }>();
  const taskKey = setupTaskKeyForSlug(taskSlug);
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const isPlatformAdmin = useIsPlatformAdmin();
  const queryClient = useQueryClient();
  const setupTasks = useOrganizationSetupTasks(
    organization.id,
    isPlatformAdmin,
    { retry: false },
  );
  const updateTask = useUpdateSetupTaskMutation();
  // Complete and support await a round trip; a second click while the first
  // is in flight must not fire it again.
  const actionInFlight = useRef(false);

  const task = setupTasks.data?.tasks.find((t) => t.key === taskKey);
  const navigate = useNavigate();
  const goToBoard = () => orgRoutes.setup.goTo();

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
      if (task.completedByFact) return goToBoard();
      if (await setStatus("done", "Failed to complete setup task")) {
        toast.success(`${task.title} completed`);
        goToBoard();
      }
    });

  const requestSupport = () =>
    guarded(async () => {
      if (task?.completedByFact) return showPylonChat();
      if (await setStatus("awaiting_support", "Failed to request support")) {
        showPylonChat();
      }
    });

  // A link to a task that no longer exists lands on the board once the list
  // has loaded, rather than on an empty frame.
  const listLoaded = setupTasks.isSuccess;
  useEffect(() => {
    // Replace rather than push: a slug that resolves to nothing should not
    // sit in history, where Back would land on it and redirect again.
    if (listLoaded && !task) {
      void navigate(orgRoutes.setup.href(), { replace: true });
    }
    // goToBoard is a fresh closure each render; only the resolved task and
    // the list settling matter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listLoaded, task]);

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
        taskKey={task.key}
        projectSlug="default"
        onComplete={() => void complete()}
        onSupport={() => void requestSupport()}
        onClose={goToBoard}
      />
    );
  }

  return (
    <SetupShell view="board">
      <JourneyLayout
        rail={
          <StepsRail
            taskTitle={task?.title ?? ""}
            taskComplete={task?.status === "done"}
          />
        }
        loading={setupTasks.isPending}
        skeletonRows={3}
      >
        <BoardLink className="mb-6 md:hidden" />
        {content}
      </JourneyLayout>
    </SetupShell>
  );
}
