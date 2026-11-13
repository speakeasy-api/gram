import { ResourceListPage } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { MCPApprovalsTab } from "@/pages/security/MCPApprovalsTab";
import { useShadowAIPolicySlot } from "@/pages/security/use-shadow-ai-policy-slot";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

/**
 * Shadow MCP: every MCP server the project's agents used or asked for, and
 * which of them may connect. It sits beside Guardrails rather than inside
 * it: Guardrails holds the policies that scan agent sessions for risk, while
 * this page is an inventory the admin decides on row by row.
 *
 * The inventory reads and every decision require org:admin, so the page is
 * gated there, the same as Guardrails.
 */
export default function ShadowMCP(): JSX.Element {
  return (
    <ResourceListPage
      scope="org:admin"
      title="Shadow MCP"
      description="See the MCP servers your team's agents use, and decide which may connect."
      primaryAction={<ShadowMCPPolicyAction />}
      hideToolbar
      fullHeight
    >
      <MCPApprovalsTab />
    </ResourceListPage>
  );
}

/** The project has one policy that blocks unapproved MCP servers. The header
 *  opens it, or the editor that creates it, so setting it up no longer starts
 *  from Guardrails. */
function ShadowMCPPolicyAction(): JSX.Element | null {
  const routes = useRoutes();
  const slot = useShadowAIPolicySlot();
  // A pending or failed read is not proof the slot is free, so the header
  // offers nothing rather than a create that the server would refuse.
  if (slot.pending || slot.failed) return null;
  if (slot.href) {
    return (
      <Button variant="secondary" asChild>
        <Link to={slot.href}>
          <Button.Text>Edit policy</Button.Text>
        </Link>
      </Button>
    );
  }
  return (
    <Button asChild>
      <Link to={`${routes.policyCenter.new.href()}?kind=shadow-ai`}>
        <Button.Text>Set up policy</Button.Text>
      </Link>
    </Button>
  );
}
