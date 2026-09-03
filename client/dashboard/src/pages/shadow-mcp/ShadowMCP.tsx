import { TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { ShadowMCPPolicyStatus } from "@/components/shadow-mcp/ShadowMCPPolicyStatus";
import { ShadowMCPPolicyUseCaseSection } from "@/components/shadow-mcp/ShadowMCPPolicyUseCaseSection";
import { ShadowMCPGatewayUseCaseSection } from "@/components/shadow-mcp/ShadowMCPGatewayUseCaseSection";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  type ShadowMCPPolicy,
  shadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Outlet } from "react-router";
import { useSearchParams } from "react-router";

export function ShadowMCPRoot(): JSX.Element {
  return <Outlet />;
}

const SHADOW_MCP_TABS = ["policy", "gateway", "inventory"] as const;
type ShadowMCPTab = (typeof SHADOW_MCP_TABS)[number];

function activeTabFromSearchParams(
  searchParams: URLSearchParams,
): ShadowMCPTab {
  const tab = searchParams.get("tab");
  return tab != null && SHADOW_MCP_TABS.includes(tab as ShadowMCPTab)
    ? (tab as ShadowMCPTab)
    : "policy";
}

function ShadowMCPLoadingState({
  label = "Loading Shadow MCP policies",
}: {
  label?: string;
}): JSX.Element {
  return (
    <div aria-label={label} className="flex flex-col gap-4 pb-8" role="status">
      <SkeletonTable />
    </div>
  );
}

export default function ShadowMCP(): JSX.Element {
  const [searchParams] = useSearchParams();
  const activeTab = activeTabFromSearchParams(searchParams);

  return (
    <RequireScope scope="org:admin" level="page">
      <ShadowMCPUseCases activeTab={activeTab} />
    </RequireScope>
  );
}

function ShadowMCPUseCases({
  activeTab,
}: {
  activeTab: ShadowMCPTab;
}): JSX.Element {
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
    <TabbedPage
      title="Shadow MCP Inventory"
      stage="beta"
      area=""
      description="Power-user inventory and use-case views for Shadow MCP servers observed outside managed gateways."
      activeTab={activeTab}
      tabs={[
        { value: "policy", label: "Policy Use Case", href: "?tab=policy" },
        { value: "gateway", label: "Gateway Use Case", href: "?tab=gateway" },
        {
          value: "inventory",
          label: "Full Inventory",
          href: "?tab=inventory",
        },
      ]}
    >
      {activeTab === "policy" && (
        <div className="py-6">
          <ShadowMCPPolicyUseCaseSection
            action={{
              label: "Open Guardrails",
              onClick: () => routes.policyCenter.goTo(),
            }}
          />
        </div>
      )}
      {activeTab === "gateway" && (
        <div className="py-6">
          <ShadowMCPGatewayUseCaseSection
            action={{
              label: "Open MCP & Gateways",
              onClick: () => routes.mcp.goTo(),
            }}
          />
        </div>
      )}
      {activeTab === "inventory" &&
        (policyDataReady ? (
          <div className="flex flex-col gap-4 pb-8 py-6">
            <ShadowMCPPolicyStatus
              disposition={disposition}
              policyState={policyState}
            />
            <ShadowMCPInventoryTable
              members={membersQuery.data?.members ?? []}
              onOpenServer={(server) =>
                routes.shadowMCP.detail.goTo(server.serverSlug)
              }
              projectID={project.id}
              roles={rolesQuery.data?.roles ?? []}
              shadowMCPPolicies={shadowMCPPolicies}
            />
          </div>
        ) : (
          <ShadowMCPLoadingState />
        ))}
    </TabbedPage>
  );
}
