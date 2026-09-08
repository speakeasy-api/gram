import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { ShadowMCPPolicyStatus } from "@/components/shadow-mcp/ShadowMCPPolicyStatus";
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
  const pageTitle = "Shadow MCP";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ ["shadow-mcp"]: pageTitle }}
        />
      </Page.Header>
      <Page.Body fullHeight className="pb-8">
        <RequireScope scope="org:admin" level="page">
          <ShadowMCPInventory pageTitle={pageTitle} />
        </RequireScope>
      </Page.Body>
    </Page>
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
  const policyState = policiesQuery.isError
    ? "unavailable"
    : shadowMCPPolicyState(policiesQuery.data?.policies);
  const shadowMCPPolicies: ShadowMCPPolicy[] =
    eligibleShadowMCPAllowRulePolicies(policiesQuery.data?.policies);
  const disposition = shadowMCPBlockingPolicyDisposition(shadowMCPPolicies);

  return (
    <Page.Section>
      <Page.Section.Title stage="beta" area="">
        {pageTitle}
      </Page.Section.Title>
      <Page.Section.Description>
        Every MCP server this project knows about — observed in agent traffic or
        raised in an access request — with its review state. Click a server for
        its evidence, requesters, and decision history.
      </Page.Section.Description>
      {policyDataReady ? (
        <Page.Section.CTA>
          <ShadowMCPPolicyStatus
            disposition={disposition}
            policyState={policyState}
          />
        </Page.Section.CTA>
      ) : null}
      <Page.Section.Body>
        {policyDataReady ? (
          <div className="flex flex-col pb-8">
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
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
