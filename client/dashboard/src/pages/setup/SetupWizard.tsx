import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router";
import { ArrowLeft, ArrowRight, Check } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RequireScope } from "@/components/require-scope";
import { showPylonChat } from "@/lib/pylon";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { JourneyLayout } from "./components/journey-layout";
import { JourneyStepsProvider } from "./components/journey-steps-provider";
import { useJourneyView } from "./components/journey-steps";
import { OnboardingStepper, type Step } from "./components/onboarding-stepper";
import { SetupViewButton } from "./components/setup-view-button";
import { SetupShell } from "./components/setup-shell";
import { SetupTaskContent } from "./components/setup-task-content";
import {
  isTaskDone,
  type OnboardingModel,
  type OnboardingTask,
} from "./onboarding-model";
import type { TaskStatus } from "./onboarding-tasks";
import { setupTaskKeyForSlug, setupTaskSlug } from "./task-slugs";
import { useOnboarding, useOnboardingActions } from "./use-onboarding";

/** Query parameter naming the card on screen, e.g. ?task=idp. */
const TASK_PARAM = "task";
/** The card's own sub-step parameter, owned by JourneyStepsProvider. */
const STEP_PARAM = "step";

// The linear way through setup: every board card in order, one on screen at
// a time, for the single owner who wants to do it all in a sitting. The board
// at /setup stays the default and the map; this page is the same cards walked
// front to back. Nothing here is a second copy of a card — each one renders
// through SetupTaskContent exactly as it does on its own page, and only what
// "done" leads to depends on whether the reader arrived from Workstreams.
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
  progress,
  currentKey,
  disabled,
  onPick,
}: {
  tasks: OnboardingTask[];
  progress: OnboardingModel["progress"];
  currentKey: string | undefined;
  /** Mirrors WizardNav: no moves while a completion is settling. */
  disabled: boolean;
  onPick: (task: OnboardingTask) => void;
}): JSX.Element {
  const [searchParams] = useSearchParams();
  const railSteps: Step[] = tasks.map((task) => ({
    id: task.id,
    title: task.title,
    description: task.description,
    status: isTaskDone(task) ? "done" : undefined,
    detail:
      task.id === currentKey ? (
        <CurrentTaskSteps disabled={disabled} />
      ) : undefined,
  }));
  const currentStep = tasks.findIndex((task) => task.id === currentKey);

  return (
    <div>
      {searchParams.get("from") === "workstreams" && (
        <div className="mb-4">
          <SetupViewButton wizard disabled={disabled} edge="start" />
        </div>
      )}
      <p className="text-eyebrow mb-4">
        {progress.done} of {progress.total} required tasks complete
      </p>
      <OnboardingStepper
        steps={railSteps}
        currentStep={currentStep}
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
  returnsToBoard,
  disabled,
  onPrevious,
  onSkip,
}: {
  previous: OnboardingTask | undefined;
  isLast: boolean;
  returnsToBoard: boolean;
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
        {returnsToBoard
          ? "Return to Workstreams"
          : isLast
            ? "Skip to dashboard"
            : "Skip task"}
        <ArrowRight className="h-4 w-4" />
      </Button>
    </div>
  );
}

function SetupWizardInner(): JSX.Element {
  const { orgSlug } = useParams();
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();
  // Platform admins can inspect hidden tasks, but they never join the walk.
  const onboarding = useOnboarding();
  const actions = useOnboardingActions(onboarding);
  const { model, canInspectHidden } = onboarding;

  // The walk is the board's visible tasks in workstream order.
  const tasks = model.visibleTasks;
  // An explicit selector must never silently open a different task.
  const requestedSlug = searchParams.get(TASK_PARAM) ?? "";
  const requested = model.task(
    setupTaskKeyForSlug(requestedSlug) ?? requestedSlug,
  );
  const firstOpen = tasks.find((task) => !isTaskDone(task));
  const current = searchParams.has(TASK_PARAM)
    ? requested && (!requested.hidden || canInspectHidden)
      ? requested
      : undefined
    : (firstOpen ?? tasks[tasks.length - 1]);
  const currentIndex = current
    ? tasks.findIndex((task) => task.id === current.id)
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
  const goToTask = (task: OnboardingTask) => {
    const params = new URLSearchParams(searchParams);
    params.set(TASK_PARAM, setupTaskSlug(task.id));
    params.delete(STEP_PARAM);
    void navigate(
      {
        pathname: location.pathname,
        search: params.toString(),
        hash: location.hash,
      },
      { replace: true },
    );
  };

  // Completing a card advances once its mutation and refetch land. A
  // reader's own move in that window (Previous, Skip, a rail click) would be
  // overwritten a moment later, so those wait until it has settled.
  const settling = actions.isPending;
  const pick = (task: OnboardingTask) => {
    if (settling) return;
    goToTask(task);
  };

  const exitTo = (pathname: string) => {
    const params = new URLSearchParams(searchParams);
    for (const selector of [TASK_PARAM, STEP_PARAM, "from", "view"])
      params.delete(selector);
    void navigate({ pathname, search: params.toString(), hash: location.hash });
  };
  const returnToBoard = () => exitTo(orgRoutes.setup.href());
  const leave = () => exitTo(`/${orgSlug}`);

  const advance = () => {
    if (searchParams.get("from") === "workstreams" || current?.hidden)
      returnToBoard();
    else if (next) goToTask(next);
    else leave();
  };

  /** Saves a status and reports the outcome; true once it committed. */
  const setStatus = async (
    status: TaskStatus,
    fallback: string,
  ): Promise<boolean> => {
    if (!current) return false;
    const outcome = await actions.setStatus(current.id, status);
    switch (outcome.status) {
      case "saved":
        return true;
      case "saved_stale":
        toast.warning(outcome.message);
        return true;
      case "failed":
        toast.error(outcome.message || fallback);
        return false;
      case "rejected":
        // A second click while the first is saving is simply dropped.
        if (outcome.reason === "blocked")
          toast.error("Complete this task's prerequisites first.");
        else if (outcome.reason !== "busy") toast.error(fallback);
        return false;
    }
  };

  const complete = async () => {
    if (!current || settling) return;
    if (current.verified) return advance();
    if (await setStatus("done", "Failed to complete setup task")) {
      toast.success(`${current.title} completed`);
      advance();
    }
  };

  const requestSupport = async () => {
    if (settling) return;
    if (current?.verified) return showPylonChat();
    if (await setStatus("awaiting_support", "Failed to request support")) {
      showPylonChat();
    }
  };

  let content: JSX.Element | null = null;
  if (onboarding.error) {
    content = (
      <Alert variant="error">
        <div>
          <AlertTitle>Could not load setup tasks</AlertTitle>
          <AlertDescription>
            Try again, or return to the board.
          </AlertDescription>
          <div className="mt-3 flex gap-2">
            <Button variant="secondary" onClick={() => void onboarding.retry()}>
              Retry
            </Button>
            <Button variant="tertiary" onClick={returnToBoard}>
              Setup board
            </Button>
          </div>
        </div>
      </Alert>
    );
  } else if (
    !onboarding.isLoading &&
    searchParams.has(TASK_PARAM) &&
    !current
  ) {
    content = (
      <Alert variant="info">
        <div>
          <AlertTitle>Setup task unavailable</AlertTitle>
          <AlertDescription>
            This task is not available. Return to Workstreams to choose a task.
          </AlertDescription>
          <Button className="mt-3" variant="secondary" onClick={returnToBoard}>
            Workstreams
          </Button>
        </div>
      </Alert>
    );
  } else if (!onboarding.isLoading && !current) {
    content = (
      <Alert variant="info">
        <div>
          <AlertTitle>Nothing to set up</AlertTitle>
          <AlertDescription>
            No setup tasks are available. Return to Workstreams to review your
            organization setup.
          </AlertDescription>
          <Button className="mt-3" variant="secondary" onClick={returnToBoard}>
            Setup board
          </Button>
        </div>
      </Alert>
    );
  } else if (current) {
    content = (
      <>
        {current.hidden && (
          <Alert variant="info">
            <div>
              <AlertTitle>Inspecting a hidden task</AlertTitle>
              <AlertDescription>
                This task is hidden from the setup walk. Restore it from
                Workstreams to include it.
              </AlertDescription>
            </div>
          </Alert>
        )}
        <WizardNav
          previous={previous}
          isLast={!next}
          returnsToBoard={
            searchParams.get("from") === "workstreams" || current.hidden
          }
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
          taskKey={current.id}
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
      <JourneyStepsProvider key={current?.id ?? "none"}>
        <JourneyLayout
          rail={
            <WizardRail
              tasks={tasks}
              progress={model.progress}
              currentKey={current?.id}
              disabled={settling}
              onPick={pick}
            />
          }
          loading={onboarding.isLoading}
          skeletonRows={6}
        >
          {content}
        </JourneyLayout>
      </JourneyStepsProvider>
    </SetupShell>
  );
}
