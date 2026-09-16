import { Page } from "@/components/page-layout";
import { ShadowAISection } from "@/pages/shadow-ai/ShadowAI";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import {
  eligibleShadowMCPAllowRulePolicies,
  type ShadowMCPPolicy,
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

// The MCP servers tab of the Shadow AI section. The page chrome and the
// project-read gate live in ShadowAISection, which both tabs share.
export default function ShadowMCP(): JSX.Element {
  return (
    <ShadowAISection activeTab="mcps">
      <ShadowMCPInventory pageTitle="MCPs" />
    </ShadowAISection>
  );
}

function ShadowMCPInventory({ pageTitle }: { pageTitle: string }): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
  const policyDataReady =
    (policiesQuery.isError || !!policiesQuery.data) &&
    (membersQuery.isError || !!membersQuery.data) &&
    (rolesQuery.isError || !!rolesQuery.data);
  const shadowMCPPolicies: ShadowMCPPolicy[] =
    eligibleShadowMCPAllowRulePolicies(policiesQuery.data?.policies);

  return (
    <Page.Section>
      <Page.Section.Title area="">{pageTitle}</Page.Section.Title>
      <Page.Section.Description>
        Every MCP server this project knows about — observed in agent traffic or
        raised in an access request — with its review state. Click a server for
        its evidence, requesters, and decision history.
      </Page.Section.Description>
      <Page.Section.Body>
        {policyDataReady ? (
          <div className="flex flex-col pb-8">
            <ShadowMCPInventoryTable
              members={membersQuery.data?.members ?? []}
              onOpenServer={(server) =>
                routes.shadowAI.mcps.detail.goTo(server.serverSlug)
              }
              projectID={project.id}
              roles={rolesQuery.data?.roles ?? []}
              shadowMCPPolicies={shadowMCPPolicies}
            />
          </div>
        ) : (
          <ShadowMCPLoadingState />
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
