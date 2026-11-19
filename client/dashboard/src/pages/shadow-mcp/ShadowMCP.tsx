import { ResourceListPage } from "@/components/page-templates";
import { ShadowMCPModeControl } from "@/components/shadow-mcp/ShadowMCPModeControl";
import { MCPApprovalsTab } from "@/pages/security/MCPApprovalsTab";

/**
 * Shadow MCP: every MCP server the project's agents used or asked for, and
 * which of them may connect. It sits beside Guardrails rather than inside
 * it: Guardrails holds the policies that scan agent sessions for risk, while
 * this page is an inventory the admin decides on row by row.
 *
 * The header holds the one setting the page needs: whether a server nobody
 * decided on is allowed or blocked by default. Allowed is built in and needs
 * no policy; there is no Shadow MCP policy to set up in Guardrails.
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
      primaryAction={<ShadowMCPModeControl />}
      hideToolbar
    >
      <MCPApprovalsTab />
    </ResourceListPage>
  );
}
