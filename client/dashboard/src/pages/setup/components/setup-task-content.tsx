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
  onSupport: () => void;
};

export function SetupTaskContent({
  taskKey,
  projectSlug,
  onComplete,
  onSupport,
}: SetupTaskContentProps): JSX.Element | null {
  let step: JSX.Element | null;
  switch (taskKey) {
    case "identity-provider":
      step = <IdentityProviderStep onComplete={onComplete} />;
      break;
    case "anthropic-observability":
      step = <AnthropicObservabilityStep onComplete={onComplete} />;
      break;
    case "instrument-agents":
      step = <InstrumentAgentsStep onComplete={onComplete} />;
      break;
    case "additional-agent-config":
      step = <AdditionalAgentConfigStep onComplete={onComplete} />;
      break;
    case "distribute-servers":
      step = <DistributeServersStep onComplete={onComplete} />;
      break;
    case "configure-policies":
      step = <ConfigurePoliciesStep onComplete={onComplete} />;
      break;
    case "platform-mcp":
      step = (
        <PlatformMCPSetupStep
          onComplete={onComplete}
          currentProjectSlug={projectSlug}
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
