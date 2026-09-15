import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes } from "@/routes";

/**
 * Deep link from a project-scoped sessions listing to the org-level MCP
 * Sessions page, where sessions are governed across every project.
 *
 * Hidden rather than disabled when the viewer can't reach that page. The
 * destination guards itself twice — it redirects to org home when the
 * `user-sessions-dashboard` flag is off, and wraps its body in a page-level
 * `org:read` check — so an ungated link would silently bounce exactly the
 * people this button is for. Both gates are mirrored here, matching how the
 * org sidebar decides to show the same destination.
 */
export function ViewOrgSessionsButton(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { hasAnyScope } = useRBAC();
  const flag = useFeatureFlag(FEATURE_FLAGS.userSessionsDashboard);

  // Opt-in: stay hidden while the flag is loading or unregistered, so the
  // button never appears and then navigates somewhere the user is redirected
  // away from.
  if (flag.status !== "enabled") return null;
  if (!hasAnyScope(["org:read", "org:admin"])) return null;

  return (
    <orgRoutes.mcpSessions.Link className="hover:no-underline">
      <Button variant="secondary" size="sm">
        <Button.Text>View all organization sessions</Button.Text>
        <Button.RightIcon>
          <Icon name="arrow-right" size="small" />
        </Button.RightIcon>
      </Button>
    </orgRoutes.mcpSessions.Link>
  );
}
