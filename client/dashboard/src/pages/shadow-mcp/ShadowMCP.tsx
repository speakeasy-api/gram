import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import type { ShadowMCPPolicy } from "@/components/shadow-mcp/ShadowMCPInventoryActions";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { ShadowMCPPolicyStatus } from "@/components/shadow-mcp/ShadowMCPPolicyStatus";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  shadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Outlet } from "react-router";

export function ShadowMCPRoot(): JSX.Element {
  return <Outlet />;
}

function ShadowMCPLoadingState(): JSX.Element {
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

export default function ShadowMCP(): JSX.Element {
  // Keep the scope gate OUTSIDE the data-owning component so the risk-policy /
  // members / roles queries never fire for unauthorized visitors.
  return (
    <RequireScope scope="org:admin" level="page">
      <ShadowMCPInner />
    </RequireScope>
  );
}

function ShadowMCPInner(): JSX.Element {
  const pageTitle = "Shadow MCP";
  const project = useProject();
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
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
    <ResourceListPage
      breadcrumbSubstitutions={{ ["shadow-mcp"]: pageTitle }}
      title={pageTitle}
      stage="beta"
      description="Manage the Shadow MCP server inventory, allow decisions, and requests."
      primaryAction={
        policyDataReady ? (
          <ShadowMCPPolicyStatus
            disposition={disposition}
            policyState={policyState}
          />
        ) : undefined
      }
      isLoading={!policyDataReady}
      loadingFallback={<ShadowMCPLoadingState />}
      fullHeight
    >
      <div className="flex min-h-0 flex-1 flex-col pb-8">
        <ShadowMCPInventoryTable
          members={membersQuery.data?.members ?? []}
          onOpenServer={(server) =>
            routes.shadowMCP.detail.goTo(server.serverSlug)
          }
          policyState={policyState}
          projectID={project.id}
          roles={rolesQuery.data?.roles ?? []}
          shadowMCPPolicies={shadowMCPPolicies}
        />
      </div>
    </ResourceListPage>
  );
}
