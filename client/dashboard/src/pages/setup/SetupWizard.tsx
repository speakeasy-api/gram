import { useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import type {
  SetupTask,
  SetupTaskStatus,
} from "@gram/client/models/components/setuptask.js";
import type { UpdateSetupTaskRequestBody } from "@gram/client/models/components/updatesetuptaskrequestbody.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useUpdateSetupTaskMutation } from "@gram/client/react-query/updateSetupTask.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Check } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import {
  buildOrganizationSetupTasksQuery,
  invalidateOrganizationSetupTasks,
} from "@/hooks/useOrganizationSetupTasks";
import { showPylonChat } from "@/lib/pylon";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { JourneyLayout } from "./components/journey-layout";
import { JourneyStepsProvider } from "./components/journey-steps-provider";
import { useJourneyView } from "./components/journey-steps";
import { OnboardingStepper, type Step } from "./components/onboarding-stepper";
import { SetupShell } from "./components/setup-shell";
import { SetupTaskContent } from "./components/setup-task-content";
import { setupTaskKeyForSlug, setupTaskSlug } from "./task-slugs";

/** Query parameter naming the card on screen, e.g. ?task=idp. */
const TASK_PARAM = "task";
/** The card's own sub-step parameter, owned by JourneyStepsProvider. */
const STEP_PARAM = "step";

// The linear way through setup: every board card in order, one on screen at
// a time, for the single owner who wants to do it all in a sitting. The board
// at /setup stays the default and the map; this page is the same cards walked
// front to back. Nothing here is a second copy of a card — each one renders
// through SetupTaskContent exactly as it does on its own page, and only what
// "done" leads to changes: the next card rather than the board.
export default function SetupWizard(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <SetupWizardInner />
    </RequireScope>
  );
}

// The current card's own sub-steps, nested under it in the rail. Cards with a
// single section have nothing to walk, so they show nothing extra.
function CurrentTaskSteps({
  disabled,
}: {
  disabled: boolean;
}): JSX.Element | null {
  const { steps, activeIndex, setActiveIndex } = useJourneyView();
  if (steps.length < 2) return null;

  return (
    <ol className="flex flex-col gap-1.5" aria-label="Steps in this task">
      {steps.map((step) => {
        const active = step.index === activeIndex;
        return (
          <li key={step.slug}>
            <button
              type="button"
              aria-current={active ? "step" : undefined}
              disabled={disabled}
              onClick={() => setActiveIndex(step.index)}
              className={cn(
                "flex w-full items-center gap-2 text-left text-sm leading-snug disabled:cursor-not-allowed",
                active
                  ? "text-foreground font-medium"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {step.complete ? (
                <Check
                  className="text-default-success h-3.5 w-3.5 flex-shrink-0"
                  strokeWidth={3}
                  aria-label="Done"
                />
              ) : (
                <span
                  aria-hidden="true"
                  className="w-3.5 flex-shrink-0 text-center font-mono text-xs"
                >
                  {step.index}
                </span>
              )}
              {step.title}
            </button>
          </li>
        );
      })}
    </ol>
  );
}

function WizardRail({
  tasks,
  currentKey,
  disabled,
  onPick,
}: {
  tasks: SetupTask[];
  currentKey: string | undefined;
  /** Mirrors WizardNav: no moves while a completion is settling. */
  disabled: boolean;
  onPick: (task: SetupTask) => void;
}): JSX.Element {
  const railSteps: Step[] = tasks.map((task) => ({
    id: task.key,
    title: task.title,
    description: task.description,
    status: task.status === "done" ? "done" : undefined,
    detail:
      task.key === currentKey ? (
        <CurrentTaskSteps disabled={disabled} />
      ) : undefined,
  }));
  const currentStep = tasks.findIndex((task) => task.key === currentKey);
  const doneCount = tasks.filter((task) => task.status === "done").length;

  return (
    <div>
      <p className="text-eyebrow mb-4">
        {doneCount} of {tasks.length} tasks complete
      </p>
      <OnboardingStepper
        steps={railSteps}
        currentStep={currentStep === -1 ? 0 : currentStep}
        disabled={disabled}
        onStepClick={(position) => {
          const task = tasks[position];
          if (task) onPick(task);
        }}
      />
    </div>
  );
}

// Previous / Skip sit above the card rather than in its footer: the footer
// belongs to the card (Next step, Mark done, Get support) and is shared with
// the task page, so the wizard's own moves stay out of it.
function WizardNav({
  previous,
  isLast,
  disabled,
  onPrevious,
  onSkip,
}: {
  previous: SetupTask | undefined;
  isLast: boolean;
  /** While a completion is in flight its own advance is about to land. */
  disabled: boolean;
  onPrevious: () => void;
  onSkip: () => void;
}): JSX.Element {
  return (
    <div className="mb-6 flex items-center justify-between gap-3">
      {previous ? (
        <Button
          variant="tertiary"
          size="sm"
          onClick={onPrevious}
          disabled={disabled}
          className="text-muted-foreground hover:text-foreground gap-1.5"
        >
          <ArrowLeft className="h-4 w-4" />
          Previous task
        </Button>
      ) : (
        <span />
      )}
      <Button
        variant="tertiary"
        size="sm"
        onClick={onSkip}
        disabled={disabled}
        className="text-muted-foreground hover:text-foreground gap-1.5"
      >
        {isLast ? "Skip to dashboard" : "Skip task"}
        <ArrowRight className="h-4 w-4" />
      </Button>
    </div>
  );
}

function SetupWizardInner(): JSX.Element {
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  // Same list the board shows by default: hidden cards stay out of the walk.
  const client = useGramContext();
  const setupTasks = useQuery(
    buildOrganizationSetupTasksQuery(client, organization.id, false, {
      retry: false,
    }),
  );
  const updateTask = useUpdateSetupTaskMutation();
  // Complete and support await a round trip; a second click while the first
  // is in flight must not fire it again. The ref blocks re-entry within a
  // render; the state is what the controls read, and it spans the whole
  // action — mutation and the refetch after it — where `isPending` alone
  // clears as soon as the mutation resolves.
  const actionInFlight = useRef(false);
  const [actionSettling, setActionSettling] = useState(false);

  const tasks = setupTasks.data?.tasks ?? [];

  // ?task= names the card on screen, as a URL slug so the address bar reads
  // like the card's own page. Without one — or with one that names nothing
  // here — resume at the first card still open; every card done lands on the
  // last so the reader can see they are finished.
  const requestedKey = setupTaskKeyForSlug(searchParams.get(TASK_PARAM) ?? "");
  const requested = tasks.find((task) => task.key === requestedKey);
  const firstOpen = tasks.find((task) => task.status !== "done");
  const current = requested ?? firstOpen ?? tasks[tasks.length - 1];
  const currentIndex = current
    ? tasks.findIndex((task) => task.key === current.key)
    : -1;
  const previous = currentIndex > 0 ? tasks[currentIndex - 1] : undefined;
  const next =
    currentIndex >= 0 && currentIndex < tasks.length - 1
      ? tasks[currentIndex + 1]
      : undefined;

  // Moving between cards rewrites ?task= and drops the outgoing card's
  // ?step=, which would otherwise be read as a link into the next card. It
  // replaces rather than pushes: Back belongs to wherever the reader came
  // from, not to each card passed through.
  const goToTask = (task: SetupTask) => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set(TASK_PARAM, setupTaskSlug(task.key));
        params.delete(STEP_PARAM);
        return params;
      },
      { replace: true },
    );
  };

  // Completing a card advances once its mutation and refetch land. A
  // reader's own move in that window (Previous, Skip, a rail click) would be
  // overwritten a moment later, so those wait until it has settled.
  const settling = actionSettling || updateTask.isPending;
  const pick = (task: SetupTask) => {
    if (settling) return;
    goToTask(task);
  };

  const leave = () => void navigate(`/${orgSlug}`);

  const advance = () => {
    if (next) goToTask(next);
    else leave();
  };

  const mutate = async (body: UpdateSetupTaskRequestBody) => {
    await updateTask.mutateAsync({
      request: { updateSetupTaskRequestBody: body },
    });
    await invalidateOrganizationSetupTasks(queryClient, organization.id);
  };

  const guarded = async (action: () => Promise<void>) => {
    if (updateTask.isPending || actionInFlight.current) return;
    actionInFlight.current = true;
    setActionSettling(true);
    try {
      await action();
    } finally {
      actionInFlight.current = false;
      setActionSettling(false);
    }
  };

  const setStatus = async (
    status: SetupTaskStatus,
    fallback: string,
  ): Promise<boolean> => {
    if (!current) return false;
    try {
      await mutate({ taskKey: current.key, status });
      return true;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : fallback);
      return false;
    }
  };

  const complete = () =>
    guarded(async () => {
      if (!current) return;
      if (current.completedByFact) return advance();
      if (await setStatus("done", "Failed to complete setup task")) {
        toast.success(`${current.title} completed`);
        advance();
      }
    });

  const requestSupport = () =>
    guarded(async () => {
      if (current?.completedByFact) return showPylonChat();
      if (await setStatus("awaiting_support", "Failed to request support")) {
        showPylonChat();
      }
    });

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
            <Button variant="tertiary" onClick={() => orgRoutes.setup.goTo()}>
              Setup board
            </Button>
          </div>
        </div>
      </Alert>
    );
  } else if (setupTasks.isSuccess && !current) {
    content = (
      <Alert variant="info">
        <div>
          <AlertTitle>Nothing to set up</AlertTitle>
          <AlertDescription>
            Every setup task is hidden. Restore one from the board to walk it
            here.
          </AlertDescription>
          <Button
            className="mt-3"
            variant="secondary"
            onClick={() => orgRoutes.setup.goTo()}
          >
            Setup board
          </Button>
        </div>
      </Alert>
    );
  } else if (current) {
    content = (
      <>
        <WizardNav
          previous={previous}
          isLast={!next}
          disabled={settling}
          onPrevious={() => {
            if (previous) pick(previous);
          }}
          onSkip={() => {
            if (settling) return;
            advance();
          }}
        />
        <SetupTaskContent
          taskKey={current.key}
          projectSlug={searchParams.get("projectSlug") ?? "default"}
          onComplete={() => void complete()}
          onSupport={() => void requestSupport()}
          onClose={() => {
            if (!settling) advance();
          }}
        />
      </>
    );
  }

  return (
    <SetupShell isPending={settling}>
      {/* Keyed by the card: each one has its own sub-steps, so carrying the
          previous card's active step into the next would land the reader on
          an unrelated section. The rail lives inside the provider too, so it
          can nest the current card's sub-steps under it. */}
      <JourneyStepsProvider key={current?.key ?? "none"}>
        <JourneyLayout
          rail={
            <WizardRail
              tasks={tasks}
              currentKey={current?.key}
              disabled={settling}
              onPick={pick}
            />
          }
          loading={setupTasks.isPending}
          skeletonRows={6}
        >
          {content}
        </JourneyLayout>
      </JourneyStepsProvider>
    </SetupShell>
  );
}
