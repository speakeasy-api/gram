import { useCallback, useEffect } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { OnboardingHeader } from "./onboarding-header";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingStepper, type Step } from "./onboarding-stepper";
import {
  ConnectIdpStep,
  DirectorySyncStep,
  CreateMarketplaceStep,
  DistributeServersStep,
  InstrumentAgentsStep,
  AdditionalAgentConfigStep,
  ConfirmTrafficStep,
  ConfigurePoliciesStep,
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
    description: "For distributing servers to your users",
  },
  {
    id: "distribute-servers",
    title: "Distribute MCP servers",
    description: "Choose some MCP Servers to distribute to your organization",
  },
  {
    id: "instrument-agents",
    title: "Instrument agents",
    description: "Connect AI coding assistants",
  },
  {
    id: "additional-agent-config",
    title: "Additional agent configuration",
    description: "Optional API keys for usage and compliance data",
  },
  {
    id: "confirm-traffic",
    title: "Confirm traffic",
    description: "Verify connectivity and compliance",
  },
  {
    id: "configure-policies",
    title: "Configure policies",
    description: "Pick the categories to flag in agent traffic",
  },
];

export function SetupWizard(): JSX.Element | null {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  // All steps are accessible — SSO and DSYNC are both skippable.
  const maxAllowedStep = STEPS.length - 1;

  const stepSlug = searchParams.get("step");

  // Server-side onboarding signals used to resume at the right step on reload.
  // `onboardingStatus` covers SSO + DSYNC; `publishStatus` covers the
  // marketplace step. Steps after marketplace (distribute-servers,
  // instrument-agents, additional-agent-config, confirm-traffic) have no server
  // signal — once marketplace is published we land on distribute-servers and
  // let the user click forward.
  const { data: onboardingStatus, isLoading: isOnboardingStatusLoading } =
    useOnboardingStatus();
  const { data: publishStatus, isLoading: isPublishStatusLoading } =
    usePublishStatus();
  const statusLoading = isOnboardingStatusLoading || isPublishStatusLoading;

  useEffect(() => {
    if (stepSlug) return;
    if (statusLoading) return; // wait so we don't flash step 0 then jump
    let resumeStep = 0;
    if (publishStatus?.connected) {
      resumeStep = 3; // marketplace done → distribute-servers
    } else if (onboardingStatus?.dsyncConfigured) {
      resumeStep = 2; // dsync done → create-marketplace
    } else if (onboardingStatus?.ssoConfigured) {
      resumeStep = 1; // sso done → directory-sync
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("step", STEPS[resumeStep]!.id!);
        return next;
      },
      { replace: true },
    );
  }, [
    stepSlug,
    statusLoading,
    onboardingStatus,
    publishStatus,
    setSearchParams,
  ]);

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
          next.set("step", STEPS[index]!.id!);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Clicking a step in the stepper previews that step (forward or back) by
  // swapping the `step` query param. It never advances the real onboarding
  // signals — those only move when the user completes a step via Continue — so
  // this is a pure preview, safe to jump anywhere in range.
  const goToStep = useCallback(
    (index: number) => {
      if (index >= 0 && index <= maxAllowedStep && index !== currentStep) {
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
      void navigate(`/${orgSlug}`);
    }
  }, [currentStep, navigate, orgSlug, setCurrentStep]);

  const goBack = useCallback(() => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  }, [currentStep, setCurrentStep]);

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  // While we're still figuring out where to resume (no slug + queries in
  // flight), render nothing rather than briefly mounting step 0. The
  // resume-step useEffect above will set the slug as soon as the queries
  // resolve.
  if (!stepSlug && statusLoading) {
    return null;
  }

  const renderStep = () => {
    switch (currentStep) {
      case 0:
        return (
          <ConnectIdpStep
            onSkip={completeCurrentStep}
            onComplete={completeCurrentStep}
          />
        );
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
          <DistributeServersStep
            onComplete={completeCurrentStep}
            onSkip={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 4:
        return (
          <InstrumentAgentsStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 5:
        return (
          <AdditionalAgentConfigStep
            onComplete={completeCurrentStep}
            onSkip={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 6:
        return (
          <ConfirmTrafficStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case 7:
        return (
          <ConfigurePoliciesStep
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
      <OnboardingHeader onLeave={handleLeave} />

      <main className="flex flex-1 items-start justify-center px-8 py-16">
        <div className="flex w-full max-w-5xl gap-24">
          <div className="w-64 flex-shrink-0">
            <OnboardingStepper
              steps={STEPS}
              currentStep={currentStep}
              onStepClick={goToStep}
              maxAllowedStep={maxAllowedStep}
              allowJumpAhead
            />
          </div>

          <div className="min-w-0 flex-1">{renderStep()}</div>
        </div>
      </main>

      <OnboardingFooter />
    </div>
  );
}
