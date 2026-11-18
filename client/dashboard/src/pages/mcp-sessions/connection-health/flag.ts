import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

/** Connection health flag; "pending" until PostHog answers, off when missing. */
export function useConnectionHealthFlag(): "pending" | "on" | "off" {
  const flag = useFeatureFlag(FEATURE_FLAGS.mcpConnectionHealth);
  if (flag.status === "loading") return "pending";
  return flag.status === "enabled" ? "on" : "off";
}
