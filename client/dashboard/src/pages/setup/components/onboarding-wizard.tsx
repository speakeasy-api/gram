import { useCallback, useEffect, useMemo } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { Skeleton } from "@/components/ui/Skeleton";
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
  PlatformMCPSetupStep,
} from "./steps";

const CORE_STEPS: Step[] = [
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
    id: "distribute-servers",
    title: "Distribute MCP servers",
    description: "Choose some MCP Servers to distribute to your organization",
  },
];

const CONFIGURE_POLICIES_STEP: Step = {
  id: "configure-policies",
  title: "Configure policies",
  description: "Pick the categories to flag in agent traffic",
};

const PLATFORM_MCP_STEP: Step = {
  id: "platform-mcp",
  title: "Set up Platform MCP",
  description: "Optional agent-assisted MCP setup",
  badge: "Optional",
};

function indexOfStep(steps: Step[], id: string): number {
  const index = steps.findIndex((step) => step.id === id);
  return index === -1 ? 0 : index;
}

export function SetupWizard(): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const { markSetupStarted } = useOrgSetupStarted(orgSlug);

  useEffect(() => {
    markSetupStarted();
  }, [markSetupStarted]);

  const setupProjectSlug = searchParams.get("projectSlug") ?? undefined;
  const setupPath = searchParams.get("setupPath");
  const usesPlatformMcpPath = setupPath === "platform-mcp";
  const steps = useMemo(
    () =>
      usesPlatformMcpPath
        ? [...CORE_STEPS, PLATFORM_MCP_STEP, CONFIGURE_POLICIES_STEP]
        : [...CORE_STEPS, CONFIGURE_POLICIES_STEP, PLATFORM_MCP_STEP],
    [usesPlatformMcpPath],
  );

  // All steps are accessible — SSO and DSYNC are both skippable.
  const maxAllowedStep = steps.length - 1;

  const stepSlug = searchParams.get("step");

  // Server-side onboarding signals used to resume at the right step on reload.
  // `onboardingStatus` covers SSO + DSYNC; `publishStatus` covers the
  // marketplace step. Steps after marketplace (instrument-agents,
  // additional-agent-config, confirm-traffic, distribute-servers) have no
  // server signal — once marketplace is published we land on instrument-agents
  // and let the user click forward.
  const { data: onboardingStatus, isLoading: isOnboardingStatusLoading } =
    useOnboardingStatus();
  const { data: publishStatus, isLoading: isPublishStatusLoading } =
    usePublishStatus();
  const statusLoading = isOnboardingStatusLoading || isPublishStatusLoading;

  useEffect(() => {
    if (stepSlug) return;
    if (statusLoading) return; // wait so we don't flash step 0 then jump
    // If either query errored, its data is undefined and the checks below all
    // fail — we fall back to step 0.
    let resumeStep = 0;
    if (publishStatus?.connected) {
      resumeStep = indexOfStep(steps, "instrument-agents");
    } else if (onboardingStatus?.dsyncConfigured) {
      resumeStep = indexOfStep(steps, "create-marketplace");
    } else if (onboardingStatus?.ssoConfigured) {
      resumeStep = indexOfStep(steps, "directory-sync");
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("step", steps[resumeStep]!.id!);
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
    steps,
  ]);

  const requestedStep = stepSlug
    ? Math.max(
        0,
        steps.findIndex((s) => s.id === stepSlug),
      )
    : 0;

  const currentStep = Math.min(requestedStep, maxAllowedStep);

  const setCurrentStep = useCallback(
    (index: number) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("step", steps[index]!.id!);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams, steps],
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
    if (nextIndex < steps.length) {
      setCurrentStep(nextIndex);
    } else {
      const projectSlug = searchParams.get("projectSlug") ?? "default";
      void navigate(`/${orgSlug}/projects/${projectSlug}`);
    }
  }, [
    currentStep,
    navigate,
    orgSlug,
    searchParams,
    setCurrentStep,
    steps.length,
  ]);

  const goBack = useCallback(() => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  }, [currentStep, setCurrentStep]);

  const leavePlatformMcpPath = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("setupPath");
        next.set("step", "distribute-servers");
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  // While we're still figuring out where to resume (no slug + queries in
  // flight), keep the page shell visible with skeletons rather than briefly
  // mounting step 0. The resume-step useEffect above will set the slug as soon
  // as the queries resolve (or error, which falls back to step 0).
  const resolvingResume = !stepSlug && statusLoading;

  const startPlatformMcpPath = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("setupPath", "platform-mcp");
        next.set("step", PLATFORM_MCP_STEP.id);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const renderStep = () => {
    switch (steps[currentStep]?.id) {
      case "connect-idp":
        return (
          <ConnectIdpStep
            onSkip={completeCurrentStep}
            onComplete={completeCurrentStep}
          />
        );
      case "directory-sync":
        return (
          <DirectorySyncStep onComplete={completeCurrentStep} onBack={goBack} />
        );
      case "create-marketplace":
        return (
          <CreateMarketplaceStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case "instrument-agents":
        return (
          <InstrumentAgentsStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case "additional-agent-config":
        return (
          <AdditionalAgentConfigStep
            onComplete={completeCurrentStep}
            onSkip={completeCurrentStep}
            onBack={goBack}
          />
        );
      case "confirm-traffic":
        return (
          <ConfirmTrafficStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case "distribute-servers":
        return (
          <DistributeServersStep
            onComplete={completeCurrentStep}
            onSkip={completeCurrentStep}
            onBack={goBack}
            onSetupPlatformMCP={startPlatformMcpPath}
          />
        );
      case "configure-policies":
        return (
          <ConfigurePoliciesStep
            onComplete={completeCurrentStep}
            onBack={goBack}
          />
        );
      case "platform-mcp":
        return (
          <PlatformMCPSetupStep
            onComplete={completeCurrentStep}
            onBack={usesPlatformMcpPath ? leavePlatformMcpPath : goBack}
            onSkip={completeCurrentStep}
            currentProjectSlug={setupProjectSlug}
            continueLabel={
              usesPlatformMcpPath
                ? "Continue to Configure policies"
                : "Finish setup"
            }
          />
        );
      case undefined:
        return null;
    }
  };

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <OnboardingHeader onLeave={handleLeave} />

      <main className="flex flex-1 items-start justify-center px-8 py-16">
        <div className="flex w-full max-w-5xl gap-24">
          <div className="w-64 flex-shrink-0">
            {resolvingResume ? (
              <Skeleton>
                {steps.map((step) => (
                  <div key={step.id} className="h-8 w-full" />
                ))}
              </Skeleton>
            ) : (
              <OnboardingStepper
                steps={steps}
                currentStep={currentStep}
                onStepClick={goToStep}
                maxAllowedStep={maxAllowedStep}
                allowJumpAhead
              />
            )}
          </div>

          <div className="min-w-0 flex-1">
            {resolvingResume ? (
              <Skeleton>
                <div className="h-12 w-2/3" />
                <div className="h-5 w-full" />
                <div className="h-64 w-full" />
              </Skeleton>
            ) : (
              renderStep()
            )}
          </div>
        </div>
      </main>

      <OnboardingFooter />
    </div>
  );
}
