import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

/**
 * Whether MCP Sessions shows connection health. "pending" until PostHog
 * answers, so a route can wait rather than redirect; missing and errored
 * flags read as off.
 */
export function useConnectionHealthFlag(): "pending" | "on" | "off" {
  const flag = useFeatureFlag(FEATURE_FLAGS.mcpConnectionHealth);
  if (flag.status === "loading") return "pending";
  return flag.status === "enabled" ? "on" : "off";
}
