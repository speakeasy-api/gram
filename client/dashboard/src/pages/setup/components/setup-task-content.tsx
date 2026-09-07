import {
  AdditionalAgentConfigStep,
  AnthropicObservabilityStep,
  ConfigurePoliciesStep,
  DistributeServersStep,
  IdentityProviderStep,
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
    case "identity-provider":
      step = <IdentityProviderStep onComplete={onComplete} onBack={onBack} />;
      break;
    case "anthropic-observability":
      step = (
        <AnthropicObservabilityStep onComplete={onComplete} onBack={onBack} />
      );
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
