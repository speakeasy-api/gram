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
} from "../steps";
import type { OnboardingTaskId } from "./tasks";

export interface TaskStepProps {
  taskId: OnboardingTaskId;
  projectSlug?: string;
  /** The step's own Continue / Finish control was used. */
  onComplete: () => void;
  /** The step's Back or Skip control was used. */
  onClose: () => void;
  /** A step wants to hand off to another task's dialog. */
  onOpenTask: (id: OnboardingTaskId) => void;
}

/**
 * Renders the setup step behind a board task. The steps were written for a
 * linear wizard, so their Back and Skip controls map to closing the dialog and
 * Continue maps to marking the task done.
 */
export function TaskStep({
  taskId,
  projectSlug,
  onComplete,
  onClose,
  onOpenTask,
}: TaskStepProps): JSX.Element {
  switch (taskId) {
    case "connect-idp":
      return <ConnectIdpStep onSkip={onClose} onComplete={onComplete} />;
    case "directory-sync":
      return <DirectorySyncStep onComplete={onComplete} onBack={onClose} />;
    case "create-marketplace":
      return <CreateMarketplaceStep onComplete={onComplete} onBack={onClose} />;
    case "instrument-agents":
      return <InstrumentAgentsStep onComplete={onComplete} onBack={onClose} />;
    case "additional-agent-config":
      return (
        <AdditionalAgentConfigStep
          onComplete={onComplete}
          onSkip={onClose}
          onBack={onClose}
        />
      );
    case "confirm-traffic":
      return <ConfirmTrafficStep onComplete={onComplete} onBack={onClose} />;
    case "distribute-servers":
      return (
        <DistributeServersStep
          onComplete={onComplete}
          onSkip={onClose}
          onBack={onClose}
          onSetupPlatformMCP={() => onOpenTask("platform-mcp")}
        />
      );
    case "configure-policies":
      return <ConfigurePoliciesStep onComplete={onComplete} onBack={onClose} />;
    case "platform-mcp":
      return (
        <PlatformMCPSetupStep
          onComplete={onComplete}
          onBack={onClose}
          onSkip={onClose}
          currentProjectSlug={projectSlug}
          continueLabel="Mark as done"
        />
      );
  }
}
