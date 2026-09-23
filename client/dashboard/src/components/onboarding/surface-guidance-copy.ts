import type { OnboardingUseCaseStatus } from "@gram/client/models/components/onboardingusecasestatus.js";

export type UseCaseSlug =
  | "observability"
  | "cost-tracking"
  | "security"
  | "mcp-gateway";

export interface SurfaceGuidance {
  heading: string;
  description: string;
  /**
   * "step": the org picked this use case and has a next step to do.
   * "onboarding": send the admin into onboarding with this use case chosen.
   */
  action: "step" | "onboarding";
}

const NOT_SET_UP: Record<
  UseCaseSlug,
  { heading: string; description: string }
> = {
  observability: {
    heading: "Observability is not set up yet",
    description:
      "No sessions or tool calls have arrived in the last 30 days. Onboarding picks the quickest way to get them flowing for the products your people use.",
  },
  "cost-tracking": {
    heading: "Cost tracking is not set up yet",
    description:
      "No token usage or cost has arrived in the last 30 days. Onboarding picks the quickest source of usage for the products and plans you have.",
  },
  security: {
    heading: "Security is not set up yet",
    description:
      "There is no enabled risk policy with scans or detections behind it in the last 30 days. Onboarding starts with a policy, then the traffic to scan.",
  },
  "mcp-gateway": {
    heading: "The MCP gateway is not set up yet",
    description:
      "No tool call has gone through a gateway and no plugin has been distributed in the last 30 days. Onboarding walks through one gateway and one distribution.",
  },
};

/**
 * What a product page says when its use case has no evidence behind it. A
 * page whose use case the admin picked names the computed next step; any
 * other page gives the generic reason and points into onboarding.
 */
export function surfaceGuidance(
  useCase: UseCaseSlug,
  status: OnboardingUseCaseStatus | undefined,
): SurfaceGuidance {
  const base = NOT_SET_UP[useCase];
  if (status?.selected && status.nextStep) {
    return {
      heading: base.heading,
      description: `Your next step: ${status.nextStep.title}. ${status.nextStep.description}`,
      action: "step",
    };
  }
  return { ...base, action: "onboarding" };
}
