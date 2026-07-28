import { useTelemetryContext } from "@/contexts/Telemetry";
import type { FeatureFlag } from "@/lib/featureFlags";

export type { FeatureFlag } from "@/lib/featureFlags";

export type FeatureFlagResult =
  | {
      status: "loading" | "missing" | "error";
      enabled: undefined;
    }
  | {
      status: "ready";
      enabled: boolean;
    };

const LOADING_RESULT = { status: "loading", enabled: undefined } as const;
const MISSING_RESULT = { status: "missing", enabled: undefined } as const;
const ERROR_RESULT = { status: "error", enabled: undefined } as const;

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
 * - `"ready"`: `enabled` contains the fresh boolean value from PostHog.
 * - `"missing"`: flags loaded successfully, but PostHog has no value for this
 *   registered key. Treat this as a configuration error rather than as off.
 * - `"error"`: PostHog reported that its flag request failed.
 *
 * Local development uses a deterministic provider where every flag is ready
 * and enabled. PostHog flags are rollout controls only; never use them as
 * authorization checks or entitlement enforcement.
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

      return { status: "ready", enabled };
    }
    default:
      return assertNever(status);
  }
}
