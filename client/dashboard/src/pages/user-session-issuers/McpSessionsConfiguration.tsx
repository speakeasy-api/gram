import { TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import {
  MCP_SESSIONS_DESCRIPTION,
  MCP_SESSIONS_TITLE,
  useMcpSessionsTabs,
} from "@/pages/org/mcpSessionsTabs";
import { useRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useMemo, useState } from "react";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";
import { consolidationTargets } from "./issuerDisplay";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";
import { IssuerDetailContent } from "./UserSessionIssuerDetail";
import { IssuerTable } from "./UserSessionIssuers";
import {
  retryFailedTiers,
  useIssuerTier,
  type IssuerTier,
} from "./useIssuerTier";

/**
 * MCP Sessions' Configuration tab: every MCP Authentication Provider this
 * project's servers can use, in one list. The automatic session refresh
 * policy lives on Authorization Servers.
 */
export function McpSessionsConfiguration(): JSX.Element {
  const tabs = useMcpSessionsTabs();
  return (
    <TabbedPage
      title={MCP_SESSIONS_TITLE}
      description={MCP_SESSIONS_DESCRIPTION}
      tabs={tabs}
      activeTab="configuration"
    >
      {/* Gate the reads too: the providers are the organization's. */}
      <RequireScope scope={["org:read", "org:admin"]} level="section">
        <ProviderList />
      </RequireScope>
    </TabbedPage>
  );
}

function ProviderList(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const organizationTier = useIssuerTier("organization");
  const allProjectsTier = useIssuerTier("project");
  const [consolidateSource, setConsolidateSource] =
    useState<UserSessionIssuer | null>(null);
  const [promoteTarget, setPromoteTarget] = useState<UserSessionIssuer | null>(
    null,
  );

  // The ones this project's servers can use, in one list by name: the
  // organization-wide ones and this project's own.
  const usable = useMemo<IssuerTier>(() => {
    const items = [
      ...organizationTier.items,
      ...allProjectsTier.items.filter(
        (issuer) => issuer.projectId === project.id,
      ),
    ].sort((a, b) => a.slug.localeCompare(b.slug));
    return {
      items,
      isLoading: organizationTier.isLoading || allProjectsTier.isLoading,
      isError: organizationTier.isError || allProjectsTier.isError,
      retry:
        retryFailedTiers([organizationTier, allProjectsTier]) ??
        (() => undefined),
    };
  }, [organizationTier, allProjectsTier, project.id]);
  // Consolidation may target any project's, as on the organization page.
  const pool = useMemo(
    () => [...organizationTier.items, ...allProjectsTier.items],
    [organizationTier.items, allProjectsTier.items],
  );

  return (
    <>
      <Stack gap={4}>
        <div>
          <Heading variant="h4" className="mb-2">
            MCP Authentication Providers
          </Heading>
          <Text muted small className="max-w-2xl">
            Where people sign in to this project&apos;s MCP servers. Each one
            sets how sign-in works for every server that uses it.
          </Text>
        </div>
        <IssuerTable
          tier={usable}
          showProject={false}
          scopeLabel={(issuer) =>
            issuer.projectId === "" ? "Organization-wide" : "This project"
          }
          emptyHeading="No MCP Authentication Providers yet"
          emptyDescription="One is added when an MCP server in this project starts requiring sign-in."
          detailHref={routes.mcpSessions.configuration.issuerDetail.href}
          onMakeOrganizationWide={setPromoteTarget}
          onConsolidate={setConsolidateSource}
        />
      </Stack>

      {consolidateSource && (
        <ConsolidateIssuerDialog
          source={consolidateSource}
          candidates={consolidationTargets(consolidateSource, pool)}
          candidatesLoading={
            organizationTier.isLoading || allProjectsTier.isLoading
          }
          onRetryCandidates={retryFailedTiers([
            organizationTier,
            allProjectsTier,
          ])}
          onClose={() => setConsolidateSource(null)}
        />
      )}

      {promoteTarget && (
        <MakeOrganizationWideDialog
          issuer={promoteTarget}
          onClose={() => setPromoteTarget(null)}
        />
      )}
    </>
  );
}

/** One provider, opened from the Configuration tab. */
export function McpSessionsProviderDetail(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <IssuerDetailContent
        listHref={routes.mcpSessions.configuration.href()}
        detailHref={routes.mcpSessions.configuration.issuerDetail.href}
        editableProjectId={project.id}
      />
    </RequireScope>
  );
}
