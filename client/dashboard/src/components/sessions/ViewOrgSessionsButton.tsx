import { useProject } from "@/contexts/Auth";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";

/** Link to the active project’s MCP sessions, matching the destination gates. */
export function ViewOrgSessionsButton(): JSX.Element | null {
  const routes = useRoutes();
  const { id: projectId } = useProject();
  const { hasScope } = useRBAC();

  if (!hasScope("project:read", projectId)) return null;

  return (
    <routes.mcpSessions.Link className="hover:no-underline">
      <Button variant="secondary" size="sm">
        <Button.Text>View all project sessions</Button.Text>
        <Button.RightIcon>
          <Icon name="arrow-right" size="small" />
        </Button.RightIcon>
      </Button>
    </routes.mcpSessions.Link>
  );
}
