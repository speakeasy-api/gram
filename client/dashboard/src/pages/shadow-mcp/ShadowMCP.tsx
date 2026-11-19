import { useHideInsightsDock } from "@/components/insights-context";
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
 *
 * The Project Assistant dock stays off here: it cannot read the inventory or
 * its requests, so it could only answer around the page's one job.
 */
export default function ShadowMCP(): JSX.Element {
  useHideInsightsDock();
  return (
    <ResourceListPage
      scope="org:admin"
      title="Shadow MCP"
      description="Decide which MCP servers your team's agents may connect to."
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
