import { useCallback, useEffect, useMemo } from "react";
import { Lock } from "lucide-react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useProductFeatures } from "@gram/client/react-query/productFeatures";
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

  // The identity steps (SSO / directory sync) are entitlement-gated: a
  // self-signup enterprise trial gets everything except sso/scim, so those two
  // steps are dropped from the wizard entirely (skip, not lock) until the org
  // is entitled. A fully-entitled enterprise customer still sees all 8 steps.
  const { data: features } = useProductFeatures();
  const steps = useMemo(
    () =>
      STEPS.filter((step) => {
        if (step.id === "connect-idp") return features?.ssoEnabled ?? false;
        if (step.id === "directory-sync") return features?.scimEnabled ?? false;
        return true;
      }),
    [features],
  );
  const showUpsell = !features?.ssoEnabled || !features?.scimEnabled;

  // All visible steps are accessible — SSO and DSYNC are both skippable.
  const maxAllowedStep = steps.length - 1;

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
    // Resume by step id, not index: the visible `steps` set can be shorter than
    // STEPS, so the target is resolved against `steps` and clamped to a visible
    // step (a hidden target → findIndex -1 → 0 → create-marketplace).
    let resumeId = steps[0]!.id;
    if (publishStatus?.connected) {
      resumeId = "distribute-servers"; // marketplace done → distribute-servers
    } else if (onboardingStatus?.dsyncConfigured) {
      resumeId = "create-marketplace"; // dsync done → create-marketplace
    } else if (onboardingStatus?.ssoConfigured) {
      resumeId = "directory-sync"; // sso done → directory-sync
    }
    const resumeIndex = Math.max(
      0,
      steps.findIndex((s) => s.id === resumeId),
    );
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("step", steps[resumeIndex]!.id);
        return next;
      },
      { replace: true },
    );
  }, [
    stepSlug,
    statusLoading,
    onboardingStatus,
    publishStatus,
    steps,
    setSearchParams,
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
          next.set("step", steps[index]!.id);
          return next;
        },
        { replace: true },
      );
    },
    [steps, setSearchParams],
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
      void navigate(`/${orgSlug}`);
    }
  }, [currentStep, navigate, orgSlug, steps.length, setCurrentStep]);

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

  // Render by step id, not index: filtering `steps` shifts indices, so a
  // numeric switch would map to the wrong component.
  const renderStep = () => {
    switch (steps[currentStep]?.id) {
      case undefined:
        return null;
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
      case "distribute-servers":
        return (
          <DistributeServersStep
            onComplete={completeCurrentStep}
            onSkip={completeCurrentStep}
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
      case "configure-policies":
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
              steps={steps}
              currentStep={currentStep}
              onStepClick={goToStep}
              maxAllowedStep={maxAllowedStep}
              allowJumpAhead
            />
          </div>

          <div className="min-w-0 flex-1">
            {renderStep()}
            {showUpsell && (
              <div className="border-border text-muted-foreground mt-6 flex items-center gap-2 rounded-lg border border-dashed px-4 py-3.5 text-sm">
                <Lock className="size-4 shrink-0" strokeWidth={1.75} />
                <span>
                  SSO &amp; directory sync are available on an enterprise plan.
                </span>
                <a
                  href="mailto:gram@speakeasyapi.dev?subject=Unlock%20SSO%20and%20Directory%20Sync"
                  className="text-foreground font-medium whitespace-nowrap underline underline-offset-2"
                >
                  Contact sales to unlock →
                </a>
              </div>
            )}
          </div>
        </div>
      </main>

      <OnboardingFooter />
    </div>
  );
}
