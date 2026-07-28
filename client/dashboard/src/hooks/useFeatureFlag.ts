import { useTelemetry } from "@/contexts/Telemetry";

export type FeatureFlag =
  | "gram-rbac"
  | "gram-device-agent"
  | "gram-device-integrations"
  | "org-memory"
  | "assistants"
  | "gram-functions"
  | "gram-tunneled-mcp"
  | "user-sessions-dashboard"
  | "gram-deployments-page"
  | "gram-budgets"
  | "gram-new-costs-page"
  | "gram-prompt-policies"
  | "gram-experimental-chat"
  | "onboard-external-mcp-to-user-sessions";

type WhileLoading = "off" | "on" | "unresolved";

/**
 * Read a typed PostHog feature flag.
 *
 * Once PostHog resolves the flag, this hook always returns its actual boolean
 * value. `whileLoading` controls only the value returned before then:
 *
 * - `"off"` (default) returns `false`, hiding an opt-in feature until PostHog
 *   confirms that it is enabled.
 * - `"on"` returns `true`, keeping a kill-switched feature visible unless
 *   PostHog confirms that it is disabled.
 * - `"unresolved"` returns `undefined`, allowing the caller to distinguish
 *   loading from an enabled or disabled flag and render its own loading state.
 *
 * The underlying telemetry hook re-renders when PostHog resolves or reloads
 * flags.
 *
 * In local development, the telemetry provider enables every feature flag.
 */
export function useFeatureFlag(
  flag: FeatureFlag,
  whileLoading: "unresolved",
): boolean | undefined;
export function useFeatureFlag(
  flag: FeatureFlag,
  whileLoading?: "off" | "on",
): boolean;
export function useFeatureFlag(
  flag: FeatureFlag,
  whileLoading: WhileLoading,
): boolean | undefined;
export function useFeatureFlag(
  flag: FeatureFlag,
  whileLoading: WhileLoading = "off",
): boolean | undefined {
  const enabled = useTelemetry().isFeatureEnabled(flag);

  if (enabled !== undefined) {
    return enabled;
  }

  switch (whileLoading) {
    case "off":
      return false;
    case "on":
      return true;
    case "unresolved":
      return undefined;
  }
}
