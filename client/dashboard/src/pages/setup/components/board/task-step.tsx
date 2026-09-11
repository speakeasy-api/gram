import {
  AdditionalAgentConfigStep,
  IdentityProviderStep,
  AnthropicAdminControlsStep,
  ConfigurePoliciesStep,
  ConfirmTrafficStep,
  ConnectIdpStep,
  CreateMarketplaceStep,
  DirectorySyncStep,
  DistributeServersStep,
  InstrumentAgentsStep,
  PlatformMCPSetupStep,
} from "../steps";
import { AnthropicInferenceHooksStep } from "../steps/anthropic-inference-hooks-step";
import type { OnboardingTaskId } from "./tasks";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { EnableLoggingSection } from "../enable-logging-section";
import { StepContainer } from "../step-container";

export interface TaskStepProps {
  taskId: OnboardingTaskId;
  projectSlug?: string;
  /** The step's own Continue / Finish control was used. */
  onComplete: () => void;
  /** The step's Back or Skip control was used. */
  onClose: () => void;
}

/**
 * Renders the setup step behind a board task. The steps were written for a
 * linear wizard, so their Back and Skip controls map to closing the dialog and
 * Continue maps to marking the task done.
 */
export function TaskStep(props: TaskStepProps): JSX.Element {
  const organization = useOrganization();
  const requestProjectSlug = useProjectSlugForRequests();
  const project = organization.projects.find(
    (candidate) => candidate.slug === requestProjectSlug,
  );
  const handoff = (
    <p>
      Ask an organization administrator with the required product permissions to
      complete this setup. Task assignment does not grant configuration access.
    </p>
  );
  let content = <TaskStepContent {...props} />;
  if (props.taskId === "distribute-servers") {
    if (!project) return handoff;
    content = (
      <RequireScope
        scope={["project:write", "mcp:write"]}
        all
        resourceId={project.id}
        level="section"
        fallback={handoff}
      >
        {content}
      </RequireScope>
    );
  }
  if (props.taskId === "anthropic-observability") {
    if (!project) return handoff;
    content = (
      <RequireScope
        scope="project:read"
        resourceId={project.id}
        level="section"
        fallback={handoff}
      >
        {content}
      </RequireScope>
    );
  }
  return (
    <RequireScope
      scope="org:admin"
      resourceId={organization.id}
      level="section"
      fallback={handoff}
    >
      {content}
    </RequireScope>
  );
}

export function TaskStepContent({
  taskId,
  projectSlug,
  onComplete,
  onClose,
}: TaskStepProps): JSX.Element {
  switch (taskId) {
    case "enable-logging":
      return (
        <StepContainer
          icon={null}
          title="Enable logging"
          description="Enable logging and session capture to observe your team's AI usage."
          onContinue={onComplete}
          markDoneLabel="Continue"
        >
          <EnableLoggingSection index={1} />
        </StepContainer>
      );
    case "identity-provider":
      return <IdentityProviderStep onComplete={onComplete} />;
    case "anthropic-observability":
      return <AnthropicInferenceHooksStep onComplete={onComplete} />;
    case "anthropic-admin-controls":
      return <AnthropicAdminControlsStep onComplete={onComplete} />;
    case "connect-idp":
      return <ConnectIdpStep onSkip={onClose} onComplete={onComplete} />;
    case "directory-sync":
      return (
        <DirectorySyncStep
          onComplete={onComplete}
          onSkip={onClose}
          onBack={onClose}
        />
      );
    case "create-marketplace":
      return <CreateMarketplaceStep onComplete={onComplete} onBack={onClose} />;
    case "instrument-agents":
      return <InstrumentAgentsStep onComplete={onComplete} />;
    case "additional-agent-config":
      return <AdditionalAgentConfigStep onComplete={onComplete} />;
    case "confirm-traffic":
      return <ConfirmTrafficStep onComplete={onComplete} />;
    case "distribute-servers":
      return <DistributeServersStep onComplete={onComplete} />;
    case "configure-policies":
      return <ConfigurePoliciesStep onComplete={onComplete} />;
    case "platform-mcp":
      return (
        <PlatformMCPSetupStep
          onComplete={onComplete}
          currentProjectSlug={projectSlug}
        />
      );
    default:
      return <p role="alert">Unsupported setup task: {taskId}</p>;
  }
}
