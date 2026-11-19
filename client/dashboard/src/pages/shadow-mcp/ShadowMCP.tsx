import { ResourceListPage } from "@/components/page-templates";
import { ShadowMCPPolicySummary } from "@/components/shadow-mcp/ShadowMCPPolicySummary";
import { MCPApprovalsTab } from "@/pages/security/MCPApprovalsTab";

/**
 * Shadow MCP: every MCP server the project's agents used or asked for, and
 * which of them may connect. It sits beside Guardrails rather than inside
 * it: Guardrails holds the policies that scan agent sessions for risk, while
 * this page is an inventory the admin decides on row by row.
 *
 * The policy summary at the top states the one setting the page needs:
 * whether a server nobody decided on is allowed or blocked by default, with
 * an "Edit policy" action that reviews which servers a change affects.
 * Allowed is built in and needs no policy; there is no Shadow MCP policy to
 * set up in Guardrails.
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
      hideToolbar
      fullHeight
    >
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        <ShadowMCPPolicySummary />
        <MCPApprovalsTab />
      </div>
    </ResourceListPage>
  );
}
