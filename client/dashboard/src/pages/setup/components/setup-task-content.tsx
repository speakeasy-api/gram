import {
  AdditionalAgentConfigStep,
  ConfigurePoliciesStep,
  ConfirmTrafficStep,
  ConnectIdpStep,
  CreateMarketplaceStep,
  DirectorySyncStep,
  DistributeServersStep,
  InstrumentAgentsStep,
  PlatformMCPSetupStep,
} from "./steps";
import { StepSupportProvider } from "./step-container";

type SetupTaskContentProps = {
  taskKey: string;
  projectSlug: string;
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
  onSupport: () => void;
};

export function SetupTaskContent({
  taskKey,
  projectSlug,
  onComplete,
  onSkip,
  onBack,
  onSupport,
}: SetupTaskContentProps): JSX.Element | null {
  let step: JSX.Element | null;
  switch (taskKey) {
    case "connect-idp":
      step = (
        <ConnectIdpStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
      break;
    case "directory-sync":
      step = (
        <DirectorySyncStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
      break;
    case "create-marketplace":
      step = <CreateMarketplaceStep onComplete={onComplete} onBack={onBack} />;
      break;
    case "instrument-agents":
      step = <InstrumentAgentsStep onComplete={onComplete} onBack={onBack} />;
      break;
    case "additional-agent-config":
      step = (
        <AdditionalAgentConfigStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
      break;
    case "confirm-traffic":
      step = <ConfirmTrafficStep onComplete={onComplete} onBack={onBack} />;
      break;
    case "distribute-servers":
      step = (
        <DistributeServersStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
      break;
    case "configure-policies":
      step = <ConfigurePoliciesStep onComplete={onComplete} onBack={onBack} />;
      break;
    case "platform-mcp":
      step = (
        <PlatformMCPSetupStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
          currentProjectSlug={projectSlug}
          continueLabel="Complete task"
        />
      );
      break;
    default:
      step = null;
  }

  return step ? (
    <StepSupportProvider onSupport={onSupport}>{step}</StepSupportProvider>
  ) : null;
}
