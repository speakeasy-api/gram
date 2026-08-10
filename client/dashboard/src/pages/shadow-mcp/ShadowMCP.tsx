import { ApprovalQueue } from "@/components/mcp-approvals/ApprovalQueue";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
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
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Outlet, useSearchParams } from "react-router";

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

const SHADOW_MCP_TAB_PARAM = "tab";

export default function ShadowMCP(): JSX.Element {
  const pageTitle = "Shadow MCP";
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab =
    searchParams.get(SHADOW_MCP_TAB_PARAM) === "requests"
      ? "requests"
      : "inventory";

  const selectTab = (value: string) => {
    setSearchParams(
      (params) => {
        if (value === "requests") {
          params.set(SHADOW_MCP_TAB_PARAM, "requests");
        } else {
          params.delete(SHADOW_MCP_TAB_PARAM);
        }
        return params;
      },
      { replace: true },
    );
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ ["shadow-mcp"]: pageTitle }}
        />
      </Page.Header>
      {/* The tab views live in TabsContent inside the same Tabs root as the
          triggers, so assistive tech gets the trigger→panel association. */}
      <Tabs
        value={activeTab}
        onValueChange={selectTab}
        className="flex min-h-0 flex-1 flex-col"
      >
        <div className="border-border shrink-0 border-b px-8">
          <PageTabsList className="h-auto gap-6 bg-transparent p-0">
            <PageTabsTrigger value="inventory">Inventory</PageTabsTrigger>
            <PageTabsTrigger value="requests">
              <span className="inline-flex items-center gap-2">
                Access Requests
                <ReleaseStageBadge stage="preview" noTooltip />
              </span>
            </PageTabsTrigger>
          </PageTabsList>
        </div>
        <Page.Body fullHeight className="pb-8">
          <TabsContent value="inventory">
            <RequireScope scope="org:admin" level="page">
              <ShadowMCPInventory pageTitle={pageTitle} />
            </RequireScope>
          </TabsContent>
          <TabsContent value="requests">
            <RequireScope scope="mcp_approval:read" level="page">
              <Page.Section>
                {/* area="" — the Secure area eyebrow already sits over the
                    page via the tab strip; repeating it under a secondary
                    heading reads as a stutter. */}
                <Page.Section.Title area="">
                  MCP Access Requests
                </Page.Section.Title>
                <Page.Section.Description>
                  Your team's requests to use MCP servers. Evidence is gathered
                  for each request — the decision stays yours.
                </Page.Section.Description>
                <Page.Section.Body>
                  <ApprovalQueue />
                </Page.Section.Body>
              </Page.Section>
            </RequireScope>
          </TabsContent>
        </Page.Body>
      </Tabs>
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
        Manage the Shadow MCP server inventory, allow decisions, and requests.
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
              policyState={policyState}
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
