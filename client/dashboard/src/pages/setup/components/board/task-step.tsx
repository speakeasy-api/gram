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
  DomainVerificationStep,
  InstrumentAgentsStep,
  LiteLLMSetupStep,
  PlatformMCPSetupStep,
} from "../steps";
import { AnthropicInferenceHooksStep } from "../steps/anthropic-inference-hooks-step";
import type { OnboardingTaskId } from "../../onboarding-tasks";
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

type TaskRenderer = (props: TaskStepProps) => JSX.Element;

// Exhaustive over the registry: adding a task id without a renderer is a type
// error rather than a blank card.
const TASK_RENDERERS: Record<OnboardingTaskId, TaskRenderer> = {
  "enable-logging": ({ onComplete }) => (
    <StepContainer
      title="Enable logging"
      description="Enable logging and session capture to observe your team's AI usage."
      onContinue={onComplete}
      markDoneLabel="Continue"
    >
      <EnableLoggingSection index={1} />
    </StepContainer>
  ),
  "domain-verification": ({ onComplete }) => (
    <DomainVerificationStep onComplete={onComplete} />
  ),
  "identity-provider": ({ onComplete }) => (
    <IdentityProviderStep onComplete={onComplete} />
  ),
  "anthropic-observability": ({ onComplete }) => (
    <AnthropicInferenceHooksStep onComplete={onComplete} />
  ),
  litellm: ({ onComplete }) => <LiteLLMSetupStep onComplete={onComplete} />,
  "anthropic-admin-controls": ({ onComplete }) => (
    <AnthropicAdminControlsStep onComplete={onComplete} />
  ),
  "connect-idp": ({ onComplete, onClose }) => (
    <ConnectIdpStep onSkip={onClose} onComplete={onComplete} />
  ),
  "directory-sync": ({ onComplete, onClose }) => (
    <DirectorySyncStep
      onComplete={onComplete}
      onSkip={onClose}
      onBack={onClose}
    />
  ),
  "create-marketplace": ({ onComplete, onClose }) => (
    <CreateMarketplaceStep onComplete={onComplete} onBack={onClose} />
  ),
  "instrument-agents": ({ onComplete }) => (
    <InstrumentAgentsStep onComplete={onComplete} />
  ),
  "additional-agent-config": ({ onComplete }) => (
    <AdditionalAgentConfigStep onComplete={onComplete} />
  ),
  "confirm-traffic": ({ onComplete }) => (
    <ConfirmTrafficStep onComplete={onComplete} />
  ),
  "distribute-servers": ({ onComplete }) => (
    <DistributeServersStep onComplete={onComplete} />
  ),
  "configure-policies": ({ onComplete }) => (
    <ConfigurePoliciesStep onComplete={onComplete} />
  ),
  "platform-mcp": ({ onComplete, projectSlug }) => (
    <PlatformMCPSetupStep
      onComplete={onComplete}
      currentProjectSlug={projectSlug}
    />
  ),
};

// Renderers hold no hooks, so they are called directly rather than mounted.
export function TaskStepContent(props: TaskStepProps): JSX.Element {
  return TASK_RENDERERS[props.taskId](props);
}
