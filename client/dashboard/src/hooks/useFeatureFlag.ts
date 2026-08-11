import { useTelemetryContext } from "@/contexts/Telemetry";
import type { FeatureFlag } from "@/lib/featureFlags";

export type { FeatureFlag } from "@/lib/featureFlags";

export type FeatureFlagResult =
  | { status: "loading" }
  | { status: "enabled" }
  | { status: "disabled" }
  | { status: "missing" }
  | { status: "error" };

const LOADING_RESULT = { status: "loading" } as const;
const ENABLED_RESULT = { status: "enabled" } as const;
const DISABLED_RESULT = { status: "disabled" } as const;
const MISSING_RESULT = { status: "missing" } as const;
const ERROR_RESULT = { status: "error" } as const;

function assertNever(value: never): never {
  throw new Error(`Unhandled feature flag status: ${String(value)}`);
}

/**
 * Read a typed PostHog feature flag without guessing when its value is
 * unavailable.
 *
 * The result makes each state explicit:
 *
 * - `"loading"`: PostHog has not completed its first flag request.
 * - `"enabled"`: PostHog resolved the flag to enabled.
 * - `"disabled"`: PostHog resolved the flag to disabled.
 * - `"missing"`: flags loaded successfully, but PostHog has no value for this
 *   registered key. Treat this as a configuration error rather than as off.
 * - `"error"`: PostHog reported that its flag request failed.
 *
 * Local development uses a deterministic provider where every flag is
 * immediately enabled. PostHog flags are rollout controls only; never use them
 * as authorization checks or entitlement enforcement.
 */
export function useFeatureFlag(flag: FeatureFlag): FeatureFlagResult {
  const { telemetry, featureFlags } = useTelemetryContext();
  const status = featureFlags.status;

  switch (status) {
    case "loading":
      return LOADING_RESULT;
    case "error":
      return ERROR_RESULT;
    case "ready": {
      const enabled = telemetry.isFeatureEnabled(flag, { fresh: true });
      if (enabled === undefined) {
        return MISSING_RESULT;
      }

      return enabled ? ENABLED_RESULT : DISABLED_RESULT;
    }
    default:
      return assertNever(status);
  }
}
