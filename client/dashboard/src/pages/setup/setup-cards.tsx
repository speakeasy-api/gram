import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { EnableLoggingSection } from "./components/enable-logging-section";
import { StepContainer } from "./components/step-container";
import { AdditionalAgentConfigStep } from "./components/steps/additional-agent-config-step";
import { AnthropicAdminControlsStep } from "./components/steps/anthropic-admin-controls-step";
import { AnthropicInferenceHooksStep } from "./components/steps/anthropic-inference-hooks-step";
import { ConfigurePoliciesStep } from "./components/steps/configure-policies-step";
import { ConfirmTrafficStep } from "./components/steps/confirm-traffic-step";
import { CreateMarketplaceStep } from "./components/steps/create-marketplace-step";
import { DistributeServersStep } from "./components/steps/distribute-servers-step";
import { IdentityProviderStep } from "./components/steps/identity-provider-step";
import { InstrumentAgentsStep } from "./components/steps/instrument-agents-step";
import { LiteLLMSetupStep } from "./components/steps/litellm-setup-step";
import { PlatformMCPSetupStep } from "./components/steps/platform-mcp-setup-step";

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
  "create-marketplace": {
    Step: ({ onComplete, onClose }) => (
      <CreateMarketplaceStep onComplete={onComplete} onBack={onClose} />
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

export function setupCard(taskKey: string): SetupCard | undefined {
  return Object.hasOwn(SETUP_CARDS, taskKey) ? SETUP_CARDS[taskKey] : undefined;
}

export function setupTaskSlug(taskKey: string): string {
  return setupCard(taskKey)?.slug ?? taskKey;
}

/** Resolves a ?task= value. Task keys still work, so older links resolve. */
export function setupTaskKeyForSlug(slug: string): string | undefined {
  const match = Object.entries(SETUP_CARDS).find(
    ([, card]) => card.slug === slug,
  );
  if (match) return match[0];
  return setupCard(slug) ? slug : undefined;
}
