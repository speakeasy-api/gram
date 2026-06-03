import { useCallback, useEffect } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { OnboardingHeader } from "./onboarding-header";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingStepper, type Step } from "./onboarding-stepper";
import {
  ConnectIdpStep,
  DirectorySyncStep,
  CreateMarketplaceStep,
  InstrumentAgentsStep,
  ConfirmTrafficStep,
} from "./steps";

const STEPS: Step[] = [
  {
    id: "connect-idp",
    title: "Connect identity provider",
    description: "Link SSO for authentication",
  },
  {
    id: "directory-sync",
    title: "Directory sync",
    description: "Confirm users and roles",
  },
  {
    id: "create-marketplace",
    title: "Create plugin marketplace",
    description: "Publish observability plugins to GitHub",
  },
  {
    id: "instrument-agents",
    title: "Instrument agents",
    description: "Connect AI coding assistants",
  },
  {
    id: "confirm-traffic",
    title: "Confirm traffic",
    description: "Verify connectivity and compliance",
  },
];

export function SetupWizard() {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  // All steps are accessible — SSO and DSYNC are both skippable.
  const maxAllowedStep = STEPS.length - 1;

  const stepSlug = searchParams.get("step");

  useEffect(() => {
    if (!stepSlug) {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("step", STEPS[0].id);
          return next;
        },
        { replace: true },
      );
    }
  }, [stepSlug, setSearchParams]);

  const requestedStep = stepSlug
    ? Math.max(
        0,
        STEPS.findIndex((s) => s.id === stepSlug),
      )
    : 0;

  const currentStep = Math.min(requestedStep, maxAllowedStep);

  const setCurrentStep = useCallback(
    (index: number) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("step", STEPS[index].id);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const goToStep = useCallback(
    (index: number) => {
      if (index <= maxAllowedStep && index < currentStep) {
        setCurrentStep(index);
      }
    },
    [currentStep, maxAllowedStep, setCurrentStep],
  );

  const completeCurrentStep = useCallback(() => {
    const nextIndex = currentStep + 1;
    if (nextIndex < STEPS.length) {
      setCurrentStep(nextIndex);
    } else {
      navigate(`/${orgSlug}`);
    }
  }, [currentStep, navigate, orgSlug, setCurrentStep]);

  const goBack = useCallback(() => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  }, [currentStep, setCurrentStep]);

  const handleRestart = () => {
    setCurrentStep(0);
  };

  const handleLeave = () => {
    navigate(`/${orgSlug}`);
  };

  const renderStep = () => {
    switch (currentStep) {
      case 0:
        return <ConnectIdpStep onSkip={completeCurrentStep} />;
      case 1:
        return (
          <DirectorySyncStep onComplete={completeCurrentStep} onBack={goBack} />
        );
      case 2:
        return (
          <CreateMarketplaceStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 3:
        return (
          <InstrumentAgentsStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 4:
        return (
          <ConfirmTrafficStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      default:
        return null;
    }
  };

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <OnboardingHeader onRestart={handleRestart} onLeave={handleLeave} />

      <main className="flex flex-1 items-start justify-center px-8 py-16">
        <div className="flex w-full max-w-5xl gap-24">
          <div className="w-64 flex-shrink-0">
            <OnboardingStepper
              steps={STEPS}
              currentStep={currentStep}
              onStepClick={goToStep}
              maxAllowedStep={maxAllowedStep}
            />
          </div>

          <div className="max-w-xl flex-1">{renderStep()}</div>
        </div>
      </main>

      <OnboardingFooter />
    </div>
  );
}
