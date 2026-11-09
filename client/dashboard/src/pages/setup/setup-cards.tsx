import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { EnableLoggingSection } from "./components/enable-logging-section";
import { StepContainer } from "./components/step-container";
import { AdditionalAgentConfigStep } from "./components/steps/additional-agent-config-step";
import { AnthropicAdminControlsStep } from "./components/steps/anthropic-admin-controls-step";
import { AnthropicInferenceHooksStep } from "./components/steps/anthropic-inference-hooks-step";
import { ConfigurePoliciesStep } from "./components/steps/configure-policies-step";
import { ClaudeOrgInstructionsStep } from "./components/steps/claude-org-instructions-step";
import { ClaudePluginRegistrationStep } from "./components/steps/claude-plugin-registration-step";
import { ConfirmTrafficStep } from "./components/steps/confirm-traffic-step";
import { CursorAdminControlsStep } from "./components/steps/cursor-admin-controls-step";
import { DistributeServersStep } from "./components/steps/distribute-servers-step";
import { IdentityProviderStep } from "./components/steps/identity-provider-step";
import { InstrumentAgentsStep } from "./components/steps/instrument-agents-step";
import { LiteLLMSetupStep } from "./components/steps/litellm-setup-step";
import { PlatformMCPSetupStep } from "./components/steps/platform-mcp-setup-step";
import { PrivateNetworkSetupStep } from "./components/steps/private-network-setup-step";

export interface SetupCardProps {
  projectSlug?: string;
  /** The card's own Continue / Mark done control was used. */
  onComplete: () => void;
  /** The card's own Back or Skip control was used. */
  onClose: () => void;
}

export interface SetupCard {
  /** URL name for ?task=. Defaults to the task key. */
  slug?: string;
  /**
   * Project scopes the card also needs, all checked against the request
   * project. Every card already requires org:admin.
   */
  projectScopes?: Scope[];
  Step: (props: SetupCardProps) => JSX.Element;
}

/**
 * What each setup card renders, keyed by the server's setupTaskCatalog key
 * (server/internal/organizations/setup_tasks.go). The server owns which cards
 * exist, their order, titles, and prerequisites; this owns their content.
 * Adding a card is a catalog entry there and an entry here.
 */
export const SETUP_CARDS: Record<string, SetupCard> = {
  "private-network": {
    Step: ({ onComplete }) => (
      <PrivateNetworkSetupStep onComplete={onComplete} />
    ),
  },
  "enable-logging": {
    Step: ({ onComplete }) => (
      <StepContainer
        title="Enable logging"
        description="Enable logging and session capture to observe your team's AI usage."
        onContinue={onComplete}
        markDoneLabel="Continue"
      >
        <EnableLoggingSection index={1} />
      </StepContainer>
    ),
  },
  "identity-provider": {
    slug: "idp",
    Step: ({ onComplete }) => <IdentityProviderStep onComplete={onComplete} />,
  },
  "anthropic-observability": {
    projectScopes: ["project:read"],
    Step: ({ onComplete }) => (
      <AnthropicInferenceHooksStep onComplete={onComplete} />
    ),
  },
  "anthropic-admin-controls": {
    Step: ({ onComplete }) => (
      <AnthropicAdminControlsStep onComplete={onComplete} />
    ),
  },
  "claude-plugin-registration": {
    slug: "claude-plugins",
    Step: ({ onComplete }) => (
      <ClaudePluginRegistrationStep onComplete={onComplete} />
    ),
  },
  "claude-org-instructions": {
    slug: "claude-instructions",
    Step: ({ onComplete }) => (
      <ClaudeOrgInstructionsStep onComplete={onComplete} />
    ),
  },
  "cursor-admin-controls": {
    Step: ({ onComplete }) => (
      <CursorAdminControlsStep onComplete={onComplete} />
    ),
  },
  "instrument-agents": {
    slug: "other-platforms",
    Step: ({ onComplete }) => <InstrumentAgentsStep onComplete={onComplete} />,
  },
  litellm: {
    Step: ({ onComplete }) => <LiteLLMSetupStep onComplete={onComplete} />,
  },
  "additional-agent-config": {
    slug: "integrations",
    Step: ({ onComplete }) => (
      <AdditionalAgentConfigStep onComplete={onComplete} />
    ),
  },
  "confirm-traffic": {
    Step: ({ onComplete }) => <ConfirmTrafficStep onComplete={onComplete} />,
  },
  "distribute-servers": {
    projectScopes: ["project:write", "mcp:write"],
    Step: ({ onComplete }) => <DistributeServersStep onComplete={onComplete} />,
  },
  "configure-policies": {
    slug: "policies",
    Step: ({ onComplete }) => <ConfigurePoliciesStep onComplete={onComplete} />,
  },
  "platform-mcp": {
    Step: ({ onComplete, projectSlug }) => (
      <PlatformMCPSetupStep
        onComplete={onComplete}
        currentProjectSlug={projectSlug}
      />
    ),
  },
};

/**
 * URL names for the server's groups (setupTaskGroups). A group has no card;
 * a link to one opens its first open card.
 */
const SETUP_GROUP_SLUGS: Record<string, string> = {
  "agent-observability": "agent-observability",
  "claude-desktop": "claude-desktop",
};

/**
 * ?task= values of retired cards and groups, mapped to the card that now does
 * their job so older links still land somewhere useful. The server moves
 * saved playbooks the same way (retiredStepSuccessors in onboarding_steps.go).
 */
const RETIRED_TASK_SLUGS: Record<string, string> = {
  // The Create marketplace card folded into Distribute MCP servers, and its
  // group went with it.
  "mcp-distribution": "distribute-servers",
  "create-marketplace": "distribute-servers",
};

export function setupCard(taskKey: string): SetupCard | undefined {
  return Object.hasOwn(SETUP_CARDS, taskKey) ? SETUP_CARDS[taskKey] : undefined;
}

function setupGroupSlug(taskKey: string): string | undefined {
  return Object.hasOwn(SETUP_GROUP_SLUGS, taskKey)
    ? SETUP_GROUP_SLUGS[taskKey]
    : undefined;
}

export function setupTaskSlug(taskKey: string): string {
  return setupCard(taskKey)?.slug ?? setupGroupSlug(taskKey) ?? taskKey;
}

/**
 * Resolves a ?task= value to a card or group key. Keys still work, and so do
 * retired cards and groups, so older links resolve.
 */
export function setupTaskKeyForSlug(slug: string): string | undefined {
  const card = Object.entries(SETUP_CARDS).find(([, c]) => c.slug === slug);
  if (card) return card[0];
  const group = Object.entries(SETUP_GROUP_SLUGS).find(([, s]) => s === slug);
  if (group) return group[0];
  if (Object.hasOwn(RETIRED_TASK_SLUGS, slug)) return RETIRED_TASK_SLUGS[slug];
  return setupCard(slug) || setupGroupSlug(slug) ? slug : undefined;
}
