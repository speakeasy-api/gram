import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";

// Dashboard discovery requires both engineering-owned rollout flags. Neither
// flag grants runtime access: the server independently requires platform-mcp
// and the organization-admin platform_mcp entitlement.
export function usePlatformMcpDashboardVisibility(): {
  enabled: boolean;
  isLoading: boolean;
} {
  const platformMcp = useFeatureFlag(FEATURE_FLAGS.platformMcp);
  const dashboard = useFeatureFlag(FEATURE_FLAGS.platformMcpDashboard);

  return {
    enabled: platformMcp.status === "enabled" && dashboard.status === "enabled",
    isLoading:
      platformMcp.status === "loading" || dashboard.status === "loading",
  };
}
