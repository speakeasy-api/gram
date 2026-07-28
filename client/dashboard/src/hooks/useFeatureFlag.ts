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
 * Read a typed PostHog feature flag and explicitly choose its loading behavior.
 *
 * The default `"off"` mode is appropriate for opt-in gates. Use `"on"` for
 * kill switches, or `"unresolved"` when the caller needs to handle loading
 * separately. The underlying telemetry hook re-renders when PostHog resolves
 * or reloads flags.
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
