import { useTelemetryContext } from "@/contexts/Telemetry";
import type { FeatureFlag } from "@/lib/featureFlags";

export type FeatureFlagVariantResult =
  | { status: "loading" }
  | {
      status: "resolved";
      /** The variant key PostHog assigned, or undefined for a boolean flag. */
      variant: string | undefined;
      /** PostHog's boolean read of the same key. True for every variant of a
       *  multivariate flag, including one named `off`, so never gate on it
       *  alone once a flag has variants. */
      enabled: boolean;
    }
  | { status: "missing" }
  | { status: "error" };

const LOADING_RESULT = { status: "loading" } as const;
const MISSING_RESULT = { status: "missing" } as const;
const ERROR_RESULT = { status: "error" } as const;

function assertNever(value: never): never {
  throw new Error(`Unhandled feature flag status: ${String(value)}`);
}

/**
 * Read a typed multivariate PostHog feature flag.
 *
 * Mirrors `useFeatureFlag` and shares its flag snapshot, so a consumer
 * re-renders when PostHog resolves or reloads flags. The result makes each
 * state explicit:
 *
 * - `"loading"`: PostHog has not completed its first flag request.
 * - `"resolved"`: PostHog has a value for the key. `variant` is the variant
 *   key for a multivariate flag and undefined for a boolean flag; `enabled`
 *   is the boolean read of the same key.
 * - `"missing"`: flags loaded successfully, but PostHog has no value for this
 *   registered key. Treat this as a configuration error rather than as off.
 * - `"error"`: PostHog reported that its flag request failed.
 *
 * Converting a boolean flag to a multivariate one in place changes what the
 * boolean read means, so callers that outlive such a conversion should map
 * `variant === undefined && enabled` to the pre-conversion behaviour and
 * otherwise switch on `variant`.
 */
export function useFeatureFlagVariant(
  flag: FeatureFlag,
): FeatureFlagVariantResult {
  const { telemetry, featureFlags } = useTelemetryContext();
  const status = featureFlags.status;

  switch (status) {
    case "loading":
      return LOADING_RESULT;
    case "error":
      return ERROR_RESULT;
    case "ready": {
      const value = telemetry.getFeatureFlag(flag, { fresh: true });
      const enabled = telemetry.isFeatureEnabled(flag, { fresh: true });
      if (value === undefined && enabled === undefined) {
        return MISSING_RESULT;
      }

      return {
        status: "resolved",
        variant: typeof value === "string" ? value : undefined,
        enabled: enabled === true,
      };
    }
    default:
      return assertNever(status);
  }
}
