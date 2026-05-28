import { useCallback, useMemo } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { useOnboardingStatus } from "@gram/client/react-query";
import { OnboardingHeader } from "./onboarding-header";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingStepper, type Step } from "./onboarding-stepper";
import {
  ConnectIdpStep,
  DirectorySyncStep,
  InstrumentAgentsStep,
  AddSourcesStep,
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
    id: "instrument-agents",
    title: "Instrument agents",
    description: "Connect AI coding assistants",
  },
  {
    id: "add-sources",
    title: "Add MCP sources",
    description: "Configure tools and data access",
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

  const { data: onboardingStatus, isLoading: isStatusLoading } =
    useOnboardingStatus();

  // Only SSO is a hard gate. DSYNC is optional (skippable), so once SSO is
  // verified all subsequent steps are accessible.
  const maxAllowedStep = useMemo(() => {
    if (!onboardingStatus) return 0;
    if (!onboardingStatus.ssoConfigured) return 0;
    return STEPS.length - 1;
  }, [onboardingStatus]);

  const requestedStep = Math.min(
    Math.max(0, Number(searchParams.get("step") || 0)),
    STEPS.length - 1,
  );

  const currentStep = Math.min(requestedStep, maxAllowedStep);

  const setCurrentStep = useCallback(
    (index: number) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (index === 0) {
            next.delete("step");
          } else {
            next.set("step", String(index));
          }
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

  // Wait for onboarding status before rendering when a step param is present.
  // Prevents flash of step 0 before maxAllowedStep is computed.
  if (isStatusLoading && requestedStep > 0) {
    return null;
  }

  const renderStep = () => {
    switch (currentStep) {
      case 0:
        return <ConnectIdpStep onComplete={completeCurrentStep} />;
      case 1:
        return (
          <DirectorySyncStep onComplete={completeCurrentStep} onBack={goBack} />
        );
      case 2:
        return (
          <InstrumentAgentsStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 3:
        return (
          <AddSourcesStep onComplete={completeCurrentStep} onBack={goBack} />
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
