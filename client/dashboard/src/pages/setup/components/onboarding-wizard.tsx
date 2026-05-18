import { useState, useCallback } from "react";
import { useNavigate } from "react-router";
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

export function EnterpriseSetupWizard() {
  const navigate = useNavigate();
  const [currentStep, setCurrentStep] = useState(0);

  const goToStep = useCallback(
    (index: number) => {
      if (index < currentStep) {
        setCurrentStep(index);
      }
    },
    [currentStep],
  );

  const completeCurrentStep = useCallback(() => {
    const nextIndex = currentStep + 1;
    if (nextIndex < STEPS.length) {
      setCurrentStep(nextIndex);
    } else {
      // All steps completed - redirect to org home
      navigate("..");
    }
  }, [currentStep, navigate]);

  const goBack = useCallback(() => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  }, [currentStep]);

  const handleRestart = () => {
    setCurrentStep(0);
  };

  const handleLeave = () => {
    navigate("..");
  };

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
      {/* Header */}
      <OnboardingHeader onRestart={handleRestart} onLeave={handleLeave} />

      {/* Main content */}
      <main className="flex flex-1 items-start justify-center px-8 py-16">
        <div className="flex w-full max-w-5xl gap-24">
          {/* Left: Stepper */}
          <div className="w-64 flex-shrink-0">
            <OnboardingStepper
              steps={STEPS}
              currentStep={currentStep}
              onStepClick={goToStep}
            />
          </div>

          {/* Right: Step content */}
          <div className="max-w-xl flex-1">{renderStep()}</div>
        </div>
      </main>

      {/* Footer */}
      <OnboardingFooter />
    </div>
  );
}
