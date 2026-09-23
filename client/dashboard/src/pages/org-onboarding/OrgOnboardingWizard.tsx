import {
  invalidateAllOnboarding,
  useOnboarding,
} from "@gram/client/react-query/onboarding.js";
import { useOnboardingReferenceData } from "@gram/client/react-query/onboardingReferenceData.js";
import { invalidateAllOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { useSaveOnboardingAnswersMutation } from "@gram/client/react-query/saveOnboardingAnswers.js";
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
import { UseCasePicker } from "./components/use-case-picker";
import { wizardSkippedStore } from "./onboarding-stores";
import {
  draftFromAnswers,
  draftProblem,
  productsProblem,
  resumeStep,
  stepIndex,
  toSaveRequest,
  WIZARD_STEPS,
  type Draft,
  type WizardStepId,
} from "./wizard-state";

/**
 * The onboarding wizard: three questions, then the one next step, checked
 * against real traffic until the use case is covered. Answers are saved once,
 * after the third question, and can be edited from onboarding settings later.
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
  const save = useSaveOnboardingAnswersMutation();
  const verify = useVerifyOnboardingStepMutation();

  const [stepOverride, setStepOverride] = useState<WizardStepId | null>(null);
  const [draftOverride, setDraftOverride] = useState<Draft | null>(null);
  const [lastCheck, setLastCheck] = useState<CheckResult | null>(null);

  const state = onboarding.data;
  const current = stepOverride ?? resumeStep(state);
  const draft = draftOverride ?? draftFromAnswers(state?.answers);
  const products = reference.data?.products ?? [];
  const answered = Boolean(state?.answers);

  const leave = () => {
    if (orgSlug && !answered) wizardSkippedStore.write(orgSlug, true);
    void navigate(`/${orgSlug}`);
  };

  const refresh = async () => {
    await invalidateAllOnboarding(queryClient);
    await invalidateAllOnboardingUseCaseStatus(queryClient);
  };

  const saveAnswers = async () => {
    const problem = draftProblem(draft, products);
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      await save.mutateAsync({
        request: { saveAnswersRequestBody: toSaveRequest(draft) },
      });
      await refresh();
      setDraftOverride(null);
      setLastCheck(null);
      setStepOverride("next-step");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Could not save answers",
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

  const railSteps: RailStep[] = WIZARD_STEPS.map((spec) => ({
    id: spec.id,
    title: spec.title,
    description: spec.description,
    status:
      spec.id === "next-step"
        ? state?.done
          ? "done"
          : undefined
        : answered
          ? "done"
          : undefined,
  }));

  const goTo = (id: WizardStepId) => {
    if (id === "next-step" && !answered) return;
    setStepOverride(id);
  };

  let content: ReactNode = null;
  if (reference.isError || onboarding.isError) {
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
  } else if (current === "products") {
    content = (
      <WizardStepFrame
        step="products"
        problem={productsProblem(draft, products)}
        onContinue={() => goTo("mdm")}
      >
        <ProductPicker
          products={products}
          draft={draft}
          onChange={setDraftOverride}
        />
      </WizardStepFrame>
    );
  } else if (current === "mdm") {
    content = (
      <WizardStepFrame
        step="mdm"
        problem={draft.mdmVendor ? null : "Say whether you use an MDM."}
        onBack={() => goTo("products")}
        onContinue={() => goTo("use-case")}
      >
        <MdmPicker
          vendors={reference.data?.mdmVendors ?? []}
          value={draft.mdmVendor}
          onChange={(mdmVendor) => setDraftOverride({ ...draft, mdmVendor })}
        />
      </WizardStepFrame>
    );
  } else if (current === "use-case") {
    content = (
      <WizardStepFrame
        step="use-case"
        problem={draftProblem(draft, products)}
        onBack={() => goTo("mdm")}
        continueLabel={answered ? "Save and continue" : "Show my next step"}
        busy={save.isPending}
        onContinue={() => void saveAnswers()}
      >
        <UseCasePicker
          useCases={reference.data?.useCases ?? []}
          value={draft.useCase}
          onChange={(useCase) => setDraftOverride({ ...draft, useCase })}
        />
      </WizardStepFrame>
    );
  } else if (state) {
    content = (
      <WizardStepFrame
        step="next-step"
        onBack={() => goTo("use-case")}
        backLabel="Change answers"
        continueLabel="Go to dashboard"
        onContinue={leave}
      >
        <NextStepPanel
          state={state}
          onCheck={() => void check()}
          checking={verify.isPending}
          lastCheck={lastCheck}
        />
      </WizardStepFrame>
    );
  }

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={leave} />
      <JourneyLayout
        rail={
          <OnboardingStepper
            steps={railSteps}
            currentStep={stepIndex(current)}
            onStepClick={(index) => {
              const spec = WIZARD_STEPS[index];
              if (spec) goTo(spec.id);
            }}
          />
        }
        loading={reference.isPending || onboarding.isPending}
        skeletonRows={4}
      >
        {content}
      </JourneyLayout>
      <OnboardingFooter />
    </div>
  );
}

/** One wizard screen: its title, body and the back/continue footer. */
function WizardStepFrame({
  step,
  problem = null,
  busy = false,
  backLabel = "Back",
  continueLabel = "Continue",
  onBack,
  onContinue,
  children,
}: {
  step: WizardStepId;
  problem?: string | null;
  busy?: boolean;
  backLabel?: string;
  continueLabel?: string;
  onBack?: () => void;
  onContinue: () => void;
  children: ReactNode;
}): JSX.Element {
  const spec = WIZARD_STEPS[stepIndex(step)];
  return (
    <div className="flex h-full flex-col">
      <h1 className="text-foreground text-display-sm font-thin">
        {spec?.title}
      </h1>
      <p className="text-muted-foreground mt-2 text-sm">{spec?.description}</p>
      <div className="mt-8 flex-1">{children}</div>
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
