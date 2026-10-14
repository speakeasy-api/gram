import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { ShadowMCPPolicyStatus } from "@/components/shadow-mcp/ShadowMCPPolicyStatus";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  type ShadowMCPPolicy,
  shadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
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
 * The MCP approvals tab body: every MCP server the project knows about,
 * observed in agent traffic or raised in an access request, with its review
 * state and pending requests first. A URL target opens its server page,
 * where the evidence and the decision live; a local command has no page, so
 * its review opens in a sheet on the row. The Shadow AI policy decides about
 * harnesses and assistants; MCP servers are decided here.
 *
 * The inventory read and every decision require org:admin, which the
 * Guardrails page already gates at page level.
 */
export function MCPApprovalsTab(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
  // The table's Decide Access sheet writes grants against the policies it
  // can see, so the table waits for the policy, member and role reads to
  // settle rather than deciding against an empty set.
  const policyDataReady =
    (policiesQuery.isError || !!policiesQuery.data) &&
    (membersQuery.isError || !!membersQuery.data) &&
    (rolesQuery.isError || !!rolesQuery.data);
  const policyState = policiesQuery.isError
    ? "unavailable"
    : shadowMCPPolicyState(policiesQuery.data?.policies);
  const shadowMCPPolicies: ShadowMCPPolicy[] =
    eligibleShadowMCPAllowRulePolicies(policiesQuery.data?.policies);
  const disposition = shadowMCPBlockingPolicyDisposition(shadowMCPPolicies);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-2xl">
          <Heading variant="h4" className="mb-1">
            MCP approvals
          </Heading>
          <Text muted small>
            Every MCP server this project knows about, observed in agent traffic
            or raised in an access request, with its review state. Click a
            server for its evidence, requesters, and decision history.
          </Text>
        </div>
        {policyDataReady ? (
          <ShadowMCPPolicyStatus
            disposition={disposition}
            policyState={policyState}
          />
        ) : null}
      </div>
      {policyDataReady ? (
        <ShadowMCPInventoryTable
          className="flex-1"
          members={membersQuery.data?.members ?? []}
          onOpenServer={(server) =>
            routes.shadowAI.mcps.detail.goTo(server.serverSlug)
          }
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
