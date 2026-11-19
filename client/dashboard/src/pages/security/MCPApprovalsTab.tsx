import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import {
  eligibleShadowMCPAllowRulePolicies,
  type ShadowMCPPolicy,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";

function MCPApprovalsLoadingState(): JSX.Element {
  return (
    <div
      aria-label="Loading Shadow MCP policies"
      className="flex flex-col gap-4 pb-8"
      role="status"
    >
      <SkeletonTable />
    </div>
  );
}

/**
 * The Shadow MCP page body: every MCP server the project knows about,
 * observed in agent traffic or raised in an access request, with its review
 * state and pending requests first. A URL target opens its server page,
 * where the evidence and the decision live; a local command has no page, so
 * its review opens in a sheet on the row. The Shadow AI policy decides about
 * harnesses and assistants; MCP servers are decided here.
 *
 * The inventory read and every decision require org:admin, which the
 * Shadow MCP page already gates at page level.
 */
export function MCPApprovalsTab(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
  // The table's Decide Access sheet writes grants against the policies it
  // can see, so the table waits for the policy, member and role reads to
  // settle rather than deciding against an empty set. A failed policy read
  // still shows the inventory, with deciding paused until the retry lands.
  const policyDataReady =
    (policiesQuery.isError || !!policiesQuery.data) &&
    (membersQuery.isError || !!membersQuery.data) &&
    (rolesQuery.isError || !!rolesQuery.data);
  const shadowMCPPolicies: ShadowMCPPolicy[] =
    eligibleShadowMCPAllowRulePolicies(policiesQuery.data?.policies);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 pb-8">
      {policiesQuery.isError ? (
        <Alert variant="error">
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>
              Policy data could not be loaded, so decisions are paused: an
              approval recorded now would enforce nothing. The inventory still
              shows.
            </span>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void policiesQuery.refetch()}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {policyDataReady ? (
        <ShadowMCPInventoryTable
          className="flex-1"
          members={membersQuery.data?.members ?? []}
          onOpenServer={(server) =>
            routes.shadowAI.mcps.detail.goTo(server.serverSlug)
          }
          policiesUnavailable={policiesQuery.isError}
          projectID={project.id}
          roles={rolesQuery.data?.roles ?? []}
          shadowMCPPolicies={shadowMCPPolicies}
        />
      ) : (
        <MCPApprovalsLoadingState />
      )}
    </div>
  );
}
