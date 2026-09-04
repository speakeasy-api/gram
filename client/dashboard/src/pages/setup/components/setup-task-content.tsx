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

type SetupTaskContentProps = {
  taskKey: string;
  projectSlug: string;
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
};

export function SetupTaskContent({
  taskKey,
  projectSlug,
  onComplete,
  onSkip,
  onBack,
}: SetupTaskContentProps): JSX.Element | null {
  switch (taskKey) {
    case "connect-idp":
      return (
        <ConnectIdpStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
    case "directory-sync":
      return <DirectorySyncStep onComplete={onComplete} onBack={onBack} />;
    case "create-marketplace":
      return <CreateMarketplaceStep onComplete={onComplete} onBack={onBack} />;
    case "instrument-agents":
      return <InstrumentAgentsStep onComplete={onComplete} onBack={onBack} />;
    case "additional-agent-config":
      return (
        <AdditionalAgentConfigStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
    case "confirm-traffic":
      return <ConfirmTrafficStep onComplete={onComplete} onBack={onBack} />;
    case "distribute-servers":
      return (
        <DistributeServersStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
        />
      );
    case "configure-policies":
      return <ConfigurePoliciesStep onComplete={onComplete} onBack={onBack} />;
    case "platform-mcp":
      return (
        <PlatformMCPSetupStep
          onComplete={onComplete}
          onSkip={onSkip}
          onBack={onBack}
          currentProjectSlug={projectSlug}
          continueLabel="Complete task"
        />
      );
    default:
      return null;
  }
}
