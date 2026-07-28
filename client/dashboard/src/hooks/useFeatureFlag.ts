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
 * PostHog returns `undefined` while flags are loading, then `true` or `false`
 * once the requested flag has resolved. `whileLoading` does not affect the
 * flag itself; it selects the temporary value this hook returns in place of
 * that initial `undefined`:
 *
 * - `"off"` (default) temporarily returns `false`. Use it for new or opt-in
 *   features that must remain hidden until PostHog explicitly enables them.
 *   The feature may briefly appear hidden while the flag loads.
 * - `"on"` temporarily returns `true`. Use it when the flag is a rollback
 *   switch for an established feature that should remain visible unless
 *   PostHog explicitly disables it. The feature may briefly appear visible
 *   while the flag loads.
 * - `"unresolved"` preserves `undefined`. Use it when showing either enabled
 *   or disabled UI before the flag loads would be incorrect. The caller must
 *   handle `undefined`, typically by rendering a placeholder or nothing.
 *
 * After the flag resolves, all modes return its actual boolean value. The
 * underlying telemetry hook re-renders when PostHog resolves or reloads flags.
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
