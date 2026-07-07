import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import {
  shadowMCPPolicyBadgeVariant,
  shadowMCPPolicyLabel,
  shadowMCPPolicyState,
  type ShadowMCPPolicyState,
} from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { useProject } from "@/contexts/Auth";
import { useRiskListPolicies } from "@gram/client/react-query/index.js";
import { Badge } from "@speakeasy-api/moonshine";

const ShadowMCPInventoryTableWithPolicy =
  ShadowMCPInventoryTable as unknown as (props: {
    projectID: string;
    policyState: ShadowMCPPolicyState;
  }) => JSX.Element;

export default function ShadowMCP(): JSX.Element {
  const project = useProject();
  const policiesQuery = useRiskListPolicies();
  const policyState = policiesQuery.isError
    ? "unavailable"
    : shadowMCPPolicyState(policiesQuery.data?.policies);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullHeight overflowHidden className="pb-8">
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title stage="beta">Shadow MCP</Page.Section.Title>
            <Page.Section.Description>
              Manage project-scoped Shadow MCP server inventory and URL access
              rules.
            </Page.Section.Description>
            <Page.Section.CTA>
              <Badge variant={shadowMCPPolicyBadgeVariant(policyState)}>
                <Badge.Text>{shadowMCPPolicyLabel(policyState)}</Badge.Text>
              </Badge>
            </Page.Section.CTA>
            <Page.Section.Body>
              <ShadowMCPInventoryTableWithPolicy
                policyState={policyState}
                projectID={project.id}
              />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
