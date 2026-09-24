import type { SelectUseCaseRequestBodyUseCase } from "@gram/client/models/components/selectusecaserequestbody.js";
import {
  invalidateAllOnboarding,
  useOnboarding,
} from "@gram/client/react-query/onboarding.js";
import { useOnboardingReferenceData } from "@gram/client/react-query/onboardingReferenceData.js";
import { invalidateAllOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { useSaveOnboardingStackMutation } from "@gram/client/react-query/saveOnboardingStack.js";
import { useSelectOnboardingUseCaseMutation } from "@gram/client/react-query/selectOnboardingUseCase.js";
import { useVerifyOnboardingStepMutation } from "@gram/client/react-query/verifyOnboardingStep.js";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { useCallback, useState, type ReactNode } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RequireScope } from "@/components/require-scope";
import { JourneyLayout } from "@/pages/setup/components/journey-layout";
import { OnboardingFooter } from "@/pages/setup/components/onboarding-footer";
import { OnboardingHeader } from "@/pages/setup/components/onboarding-header";
import {
  OnboardingStepper,
  type Step as RailStep,
} from "@/pages/setup/components/onboarding-stepper";
import { MdmPicker } from "./components/mdm-picker";
import { NextStepPanel, type CheckResult } from "./components/next-step-panel";
import { ProductPicker } from "./components/product-picker";
import { ProviderPicker } from "./components/provider-picker";
import { StageProgress } from "./components/stage-progress";
import { UseCasePicker } from "./components/use-case-picker";
import { wizardSkippedStore } from "./onboarding-stores";
import {
  draftFromAnswers,
  draftProblem,
  firstSentence,
  productsProblem,
  providersProblem,
  resumeScreen,
  screenSpec,
  stackProgress,
  STAGE_ONE_SCREENS,
  stepsProgress,
  toSaveStackRequest,
  type Draft,
  type WizardScreen,
} from "./wizard-state";

/**
 * The onboarding wizard in two stages. Stage one is the organization's
 * stack: providers with their plans, their products, and the MDM vendor,
 * saved as one unit. Between the stages the admin picks the use case to
 * reach first, on its own; it decides what stage two shows. Stage two is
 * that use case's steps, one at a time, each checked against real traffic
 * until the use case is covered. Two unlabeled bars above the whole wizard
 * fill as each stage progresses; the rail lists the active stage's screens.
 */
export default function OrgOnboardingWizard(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <WizardInner />
    </RequireScope>
  );
}

function WizardInner(): JSX.Element {
  const { orgSlug } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const reference = useOnboardingReferenceData();
  const onboarding = useOnboarding();
  const saveStack = useSaveOnboardingStackMutation();
  const selectUseCase = useSelectOnboardingUseCaseMutation();
  const verify = useVerifyOnboardingStepMutation();

  const [screenOverride, setScreenOverride] = useState<WizardScreen | null>(
    null,
  );
  const [draftOverride, setDraftOverride] = useState<Draft | null>(null);
  const [useCaseOverride, setUseCaseOverride] = useState<string | null>(null);
  const [lastCheck, setLastCheck] = useState<CheckResult | null>(null);

  const state = onboarding.data;
  const providers = reference.data?.providers ?? [];
  const stackSaved = Boolean(state?.answers);
  const useCasePicked = Boolean(state?.answers?.useCase);

  const screen = screenOverride ?? resumeScreen(state);
  const draft = draftOverride ?? draftFromAnswers(state?.answers);
  const useCaseDraft = useCaseOverride ?? state?.answers?.useCase ?? null;

  const leave = () => {
    if (orgSlug && !stackSaved) wizardSkippedStore.write(orgSlug, true);
    void navigate(`/${orgSlug}`);
  };

  const refresh = async () => {
    await invalidateAllOnboarding(queryClient);
    await invalidateAllOnboardingUseCaseStatus(queryClient);
  };

  const submitStack = async () => {
    const problem = draftProblem(draft, providers);
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      await saveStack.mutateAsync({
        request: { saveStackRequestBody: toSaveStackRequest(draft) },
      });
      await refresh();
      setDraftOverride(null);
      setScreenOverride("use-case");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save your stack",
      );
    }
  };

  const submitUseCase = async () => {
    if (!useCaseDraft) {
      toast.error("Pick the use case to reach first.");
      return;
    }
    try {
      await selectUseCase.mutateAsync({
        request: {
          selectUseCaseRequestBody: {
            useCase: useCaseDraft as SelectUseCaseRequestBodyUseCase,
          },
        },
      });
      await refresh();
      setUseCaseOverride(null);
      setLastCheck(null);
      setScreenOverride("steps");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save the use case",
      );
    }
  };

  const nextSlug = state?.nextStep?.slug;
  const check = useCallback(async () => {
    if (!nextSlug || verify.isPending) return;
    try {
      const result = await verify.mutateAsync({
        request: { verifyStepRequestBody: { stepSlug: nextSlug } },
      });
      setLastCheck({ verified: result.verified, evidence: result.evidence });
      if (result.verified) {
        toast.success(
          result.state.done ? "Onboarding complete" : "Step verified",
        );
        setLastCheck(null);
      }
      await refresh();
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not check evidence",
      );
    }
    // refresh is stable enough: it only closes over the query client.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nextSlug, verify]);

  const goTo = (id: WizardScreen) => {
    if (id === "use-case" && !stackSaved) return;
    if (id === "steps" && !useCasePicked) return;
    setScreenOverride(id);
  };

  const useCaseName =
    reference.data?.useCases.find(
      (option) => option.slug === state?.answers?.useCase,
    )?.name ?? null;
  const stepsTitle = useCaseName
    ? `Set up ${useCaseName.toLowerCase()}`
    : "Set up your use case";

  // The rail is scoped to the active stage: the stack screens and the use
  // case while stage one is active, the use case's own steps once the reader
  // is on them. Steps are not screens, so their rows only inform.
  const stageTwo = screen === "steps";
  const stageOneRail: RailStep[] = STAGE_ONE_SCREENS.map((spec) => ({
    id: spec.id,
    title: spec.title,
    description: spec.description,
    status: railDone(spec.id, stackSaved, useCasePicked) ? "done" : undefined,
  }));
  const stageTwoRail: RailStep[] = (state?.steps ?? []).map((step) => ({
    id: step.slug,
    title: step.title,
    description: firstSentence(step.description),
    status: step.verifiedAt ? "done" : undefined,
  }));

  const failed = reference.isError || onboarding.isError;
  const loading = reference.isPending || onboarding.isPending;

  let content: ReactNode = null;
  if (failed) {
    content = (
      <Alert variant="error">
        <div>
          <AlertTitle>Could not load onboarding</AlertTitle>
          <AlertDescription>
            Try again, or go to the dashboard.
          </AlertDescription>
          <div className="mt-3 flex gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                void reference.refetch();
                void onboarding.refetch();
              }}
            >
              Retry
            </Button>
            <Button variant="tertiary" onClick={leave}>
              Go to dashboard
            </Button>
          </div>
        </div>
      </Alert>
    );
  } else if (screen === "providers") {
    content = (
      <WizardScreenFrame
        screen="providers"
        problem={providersProblem(draft, providers)}
        onContinue={() => goTo("products")}
      >
        <ProviderPicker
          providers={providers}
          draft={draft}
          onChange={setDraftOverride}
        />
      </WizardScreenFrame>
    );
  } else if (screen === "products") {
    content = (
      <WizardScreenFrame
        screen="products"
        problem={productsProblem(draft, providers)}
        onBack={() => goTo("providers")}
        onContinue={() => goTo("mdm")}
      >
        <ProductPicker
          providers={providers}
          draft={draft}
          onChange={setDraftOverride}
        />
      </WizardScreenFrame>
    );
  } else if (screen === "mdm") {
    content = (
      <WizardScreenFrame
        screen="mdm"
        problem={draftProblem(draft, providers)}
        onBack={() => goTo("products")}
        continueLabel={stackSaved ? "Save stack" : "Save stack and continue"}
        busy={saveStack.isPending}
        onContinue={() => void submitStack()}
      >
        <MdmPicker
          vendors={reference.data?.mdmVendors ?? []}
          value={draft.mdmVendor}
          onChange={(mdmVendor) => setDraftOverride({ ...draft, mdmVendor })}
        />
      </WizardScreenFrame>
    );
  } else if (screen === "use-case") {
    content = (
      <WizardScreenFrame
        screen="use-case"
        problem={useCaseDraft ? null : "Pick the use case to reach first."}
        onBack={() => goTo("mdm")}
        backLabel="Change stack"
        continueLabel={
          useCasePicked ? "Save and show my steps" : "Show my next step"
        }
        busy={selectUseCase.isPending}
        onContinue={() => void submitUseCase()}
      >
        <UseCasePicker
          useCases={reference.data?.useCases ?? []}
          value={useCaseDraft}
          onChange={setUseCaseOverride}
        />
      </WizardScreenFrame>
    );
  } else if (state) {
    content = (
      <WizardScreenFrame
        screen="steps"
        title={stepsTitle}
        onBack={() => goTo("use-case")}
        backLabel="Change use case"
        continueLabel="Go to dashboard"
        onContinue={leave}
      >
        <NextStepPanel
          state={state}
          onCheck={() => void check()}
          checking={verify.isPending}
          lastCheck={lastCheck}
        />
      </WizardScreenFrame>
    );
  }

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={leave} />
      <JourneyLayout
        top={
          failed || loading ? null : (
            <StageProgress
              stack={stackProgress(screen)}
              steps={stepsProgress(screen, state)}
            />
          )
        }
        rail={
          stageTwo ? (
            <OnboardingStepper
              steps={stageTwoRail}
              currentStep={stageTwoRail.findIndex(
                (step) => step.id === state?.nextStep?.slug,
              )}
              onStepClick={() => undefined}
              disabled
            />
          ) : (
            <OnboardingStepper
              steps={stageOneRail}
              currentStep={STAGE_ONE_SCREENS.findIndex(
                (spec) => spec.id === screen,
              )}
              onStepClick={(index) => {
                const spec = STAGE_ONE_SCREENS[index];
                if (spec) goTo(spec.id);
              }}
            />
          )
        }
        loading={loading}
        skeletonRows={STAGE_ONE_SCREENS.length}
      >
        {content}
      </JourneyLayout>
      <OnboardingFooter />
    </div>
  );
}

/** Whether a stage-one rail entry gets its check mark. */
function railDone(
  id: WizardScreen,
  stackSaved: boolean,
  useCasePicked: boolean,
): boolean {
  switch (id) {
    case "providers":
    case "products":
    case "mdm":
      return stackSaved;
    case "use-case":
      return useCasePicked;
    case "steps":
      // Never listed: stage two's rail is the use case's own steps.
      return false;
  }
}

/** One wizard screen: its title, body and the back/continue footer. */
function WizardScreenFrame({
  screen,
  title,
  problem = null,
  busy = false,
  backLabel = "Back",
  continueLabel = "Continue",
  onBack,
  onContinue,
  children,
}: {
  screen: WizardScreen;
  /** Overrides the screen's default title, e.g. with the use case's name. */
  title?: string;
  problem?: string | null;
  busy?: boolean;
  backLabel?: string;
  continueLabel?: string;
  onBack?: () => void;
  onContinue: () => void;
  children: ReactNode;
}): JSX.Element {
  const spec = screenSpec(screen);
  return (
    <div className="flex flex-col">
      <h1 className="text-foreground text-display-sm font-thin">
        {title ?? spec.title}
      </h1>
      <p className="text-muted-foreground mt-2 text-sm">{spec.description}</p>
      <div className="mt-8">{children}</div>
      <div className="bg-border mt-8 h-px" />
      <div className="mt-6 flex flex-wrap items-center gap-3">
        <div>
          {onBack ? (
            <Button
              variant="tertiary"
              onClick={onBack}
              className="text-muted-foreground hover:text-foreground gap-1.5"
            >
              <ArrowLeft className="h-4 w-4" aria-hidden="true" />
              {backLabel}
            </Button>
          ) : null}
        </div>
        <div className="ml-auto flex items-center gap-3">
          {problem ? (
            <span className="text-muted-foreground text-sm">{problem}</span>
          ) : null}
          <Button
            onClick={onContinue}
            disabled={Boolean(problem) || busy}
            className="gap-1.5"
          >
            {busy ? "Saving..." : continueLabel}
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>
  );
}
