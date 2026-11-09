import { useEffect, useMemo, useRef } from "react";
import { getAgentPlatforms } from "../setup-data";
import type { PlatformSetupStep } from "../types";
import { PlatformSetupStepBody } from "./platform-setup-steps";
import {
  type PlatformApiKeys,
  usePlatformApiKeys,
} from "./platform-setup-values";

interface PlatformStepListProps {
  /** The platform whose API key the steps' snippets use. */
  platformId: string;
  /** A slice of the platform's setup steps, shown in order. */
  steps: PlatformSetupStep[];
  /** Why the steps are held back, shown in their place. */
  heldBack?: string;
  apiKeys?: PlatformApiKeys;
}

/**
 * Part of one platform's setup, for a card that walks only some of its steps.
 * Unlike PlatformSetupFlow it has no plan check and no "connected" toggle: the
 * card around it owns completion.
 */
export function PlatformStepList({
  platformId,
  steps,
  heldBack,
  apiKeys: sharedApiKeys,
}: PlatformStepListProps): JSX.Element {
  const platform = useMemo(
    () => getAgentPlatforms().find((p) => p.id === platformId),
    [platformId],
  );
  const localApiKeys = usePlatformApiKeys();
  const apiKeys = sharedApiKeys ?? localApiKeys;

  // Mint the key once, and only when a shown step needs it.
  const { ensure } = apiKeys;
  const needsKey = steps.some((step) => step.requiresApiKey);
  const requestedKey = useRef(false);
  useEffect(() => {
    if (!platform || heldBack || !needsKey || requestedKey.current) return;
    requestedKey.current = true;
    ensure(platform);
  }, [platform, heldBack, needsKey, ensure]);

  if (heldBack) {
    return <p className="text-muted-foreground text-sm">{heldBack}</p>;
  }

  return (
    <div className="space-y-8">
      {steps.map((step, index) => (
        <PlatformSetupStepBody
          key={step.title}
          step={step}
          eyebrow={`Step ${index + 1}`}
          apiKey={apiKeys.keys[platformId]}
          apiKeyPending={apiKeys.pending[platformId]}
          apiKeyError={apiKeys.errors[platformId]}
          onRetryApiKey={() => {
            if (platform) ensure(platform);
          }}
          onEligibilityAnswer={() => {}}
        />
      ))}
    </div>
  );
}
