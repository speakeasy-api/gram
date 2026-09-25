import { useNavigate, useParams, useSearchParams } from "react-router";
import {
  isTaskDone,
  progressOf,
  type OnboardingTask,
} from "./onboarding-model";
import type { TaskStatus } from "./onboarding-tasks";
import { useOnboarding, useOnboardingActions } from "./use-onboarding";
import { ArrowLeft, ArrowRight, Check } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RequireScope } from "@/components/require-scope";
import { showPylonChat } from "@/lib/pylon";
import { cn } from "@/lib/utils";
import { JourneyLayout } from "./components/journey-layout";
import { JourneyStepsProvider } from "./components/journey-steps-provider";
import { useJourneyView } from "./components/journey-steps";
import { OnboardingStepper, type Step } from "./components/onboarding-stepper";
import { SetupShell } from "./components/setup-shell";
import { SetupTaskContent } from "./components/setup-task-content";
import { setupTaskKeyForSlug, setupTaskSlug } from "./setup-cards";

/** Query parameter naming the card on screen, e.g. ?task=idp. */
const TASK_PARAM = "task";
/** The card's own sub-step parameter, owned by JourneyStepsProvider. */
const STEP_PARAM = "step";

// The way through setup: every selected card in order, one on screen at a
// time. Which cards appear is the organization's setup task selection, set by
// staff or by the onboarding survey through submitOnboardingSurvey.
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
  tasks: OnboardingTask[];
  currentKey: string | undefined;
  /** Mirrors WizardNav: no moves while a completion is settling. */
  disabled: boolean;
  onPick: (task: OnboardingTask) => void;
}): JSX.Element {
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
  const progress = progressOf(tasks);

  return (
    <div>
      <p className="text-eyebrow mb-4">
        {progress.done} of {progress.total} required tasks complete
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
// belongs to the card (Next step, Mark done, Get support), so the wizard's own
// moves stay out of it.
function WizardNav({
  previous,
  isLast,
  disabled,
  onPrevious,
  onSkip,
}: {
  previous: OnboardingTask | undefined;
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
  const onboarding = useOnboarding({ includeHidden: false });
  const actions = useOnboardingActions(onboarding);
  const tasks = onboarding.model.visibleTasks;

  // ?task= names the card on screen, as a URL slug. Without one — or with one that names nothing
  // here — resume at the first card still open; every card done lands on the
  // last so the reader can see they are finished.
  const requestedKey = setupTaskKeyForSlug(searchParams.get(TASK_PARAM) ?? "");
  const requested = tasks.find((task) => task.id === requestedKey);
  const firstOpen = tasks.find((task) => !isTaskDone(task));
  const current = requested ?? firstOpen ?? tasks[tasks.length - 1];
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
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set(TASK_PARAM, setupTaskSlug(task.id));
        params.delete(STEP_PARAM);
        return params;
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

  const leave = () => void navigate(`/${orgSlug}`);

  const advance = () => {
    if (next) goToTask(next);
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
          <AlertDescription>Try again in a moment.</AlertDescription>
          <Button
            className="mt-3"
            variant="secondary"
            onClick={() => void onboarding.retry()}
          >
            Retry
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
            No setup tasks are selected for this organization.
          </AlertDescription>
          <Button className="mt-3" variant="secondary" onClick={leave}>
            Go to dashboard
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
          taskKey={current.id}
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
    <SetupShell>
      {/* Keyed by the card: each one has its own sub-steps, so carrying the
          previous card's active step into the next would land the reader on
          an unrelated section. The rail lives inside the provider too, so it
          can nest the current card's sub-steps under it. */}
      <JourneyStepsProvider key={current?.id ?? "none"}>
        <JourneyLayout
          rail={
            <WizardRail
              tasks={tasks}
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
