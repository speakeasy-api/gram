import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { AGENT_PLATFORMS, platformSteps } from "../setup-data";
import type { PlatformSetupStatus } from "../types";
import { PlatformSetupStepBody } from "./platform-setup-steps";
import {
  type PlatformApiKeys,
  usePlatformApiKeys,
} from "./platform-setup-values";

interface PlatformSetupFlowProps {
  platformId: string;
  status: PlatformSetupStatus;
  onStatusChange: (status: PlatformSetupStatus) => void;
  /**
   * Why the instructions are being held back, if they are. Every platform's
   * snippets reference the published marketplace, so a card shows this until
   * its marketplace section is done rather than handing out empty URLs.
   */
  heldBack?: string;
  apiKeys?: PlatformApiKeys;
}

// One platform's whole setup, laid out top to bottom inside the section that
// connects it. The instructions used to live in a sheet that slid over the
// card and walked one instruction at a time; a section owns a single platform
// now, so the steps just stack where the reader already is.
export function PlatformSetupFlow({
  platformId,
  status,
  onStatusChange,
  heldBack,
  apiKeys: sharedApiKeys,
}: PlatformSetupFlowProps): JSX.Element | null {
  const platform = AGENT_PLATFORMS.find((p) => p.id === platformId);
  const localApiKeys = usePlatformApiKeys();
  const apiKeys = sharedApiKeys ?? localApiKeys;
  // A platform whose first step asks for the plan (the plan check)
  // shows nothing further until that is answered; "no" swaps in the per-user
  // steps for personal plans.
  const [eligible, setEligible] = useState<boolean | null>(null);
  const gate = platform?.setupSteps[0]?.eligibility;
  const unanswered = !!gate && eligible === null;

  // Mint the key the snippets need as soon as the flow is readable, and only
  // once — a failed mint leaves neither a key nor a pending flag, so a bare
  // effect would retry it forever.
  const { ensure } = apiKeys;
  const requestedKey = useRef(false);
  useEffect(() => {
    if (!platform || heldBack || unanswered) return;
    if (requestedKey.current) return;
    requestedKey.current = true;
    ensure(platform);
  }, [platform, heldBack, unanswered, ensure]);

  if (!platform) return null;
  if (heldBack) {
    return <p className="text-muted-foreground text-sm">{heldBack}</p>;
  }

  const steps = platformSteps(platform, eligible);
  const visibleSteps = unanswered ? steps.slice(0, 1) : steps;

  return (
    <div className="space-y-8">
      {visibleSteps.map((step, index) => (
        <PlatformSetupStepBody
          key={step.title}
          step={step}
          eyebrow={`Step ${index + 1}`}
          apiKey={apiKeys.keys[platform.id]}
          apiKeyPending={apiKeys.pending[platform.id]}
          apiKeyError={apiKeys.errors[platform.id]}
          onRetryApiKey={() => ensure(platform)}
          onEligibilityAnswer={(answer) => {
            setEligible(answer);
            onStatusChange("not_started");
          }}
        />
      ))}

      {unanswered ? null : status === "complete" ? (
        <div className="border-border bg-secondary/20 flex items-center justify-between border p-4">
          <p className="text-foreground flex items-center gap-2 text-sm">
            <Check className="text-default-success h-4 w-4" strokeWidth={3} />
            {platform.name} is connected.
          </p>
          <Button
            variant="tertiary"
            onClick={() => onStatusChange("not_started")}
          >
            Not yet
          </Button>
        </div>
      ) : (
        <Button variant="secondary" onClick={() => onStatusChange("complete")}>
          Mark {platform.name} as connected
        </Button>
      )}
    </div>
  );
}
