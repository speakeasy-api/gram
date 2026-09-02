import { ConnectionsListSection } from "@/components/connections/ConnectionsListSection";
import { IdentityDataFlowGraphCard } from "@/components/observe/employee-data-flow";
import { fetchIdentityDataFlowGraph } from "@/components/observe/identity-data-flow-query";
import { ErrorAlert } from "@/components/ui/Alert";
import { Skeleton } from "@/components/ui/Skeleton";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useUserSessions } from "@gram/client/react-query/userSessions.js";
import { useQuery } from "@tanstack/react-query";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import { identityHandoffs } from "./identityHandoffs";
import { IdentityPanel } from "./IdentityPanel";
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import { useIdentityProject, useIdentityWindow } from "./useIdentityQueries";

/**
 * What this person reaches, and how they got there.
 *
 * The two panels answer the same question over different clocks: the graph is
 * the path their traffic took across the window, the connection list is what
 * is open right now. Every other tab counts events; this one shows their
 * shape.
 */
export default function IdentityConnections(): JSX.Element {
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const client = useGramContext();
  const project = useIdentityProject();
  const location = useLocation();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  // No handoff on this page filters by principal, so the member list this
  // would otherwise fetch is not worth the request.
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    undefined,
    new URLSearchParams(location.search),
  );

  // Telemetry keys the graph on the Gram user id or on the id an agent
  // reported, the same way the metric panels do, so an identity with no
  // directory row still resolves.
  const userId = identity.userIds[0] ?? identity.externalUserIds[0];
  const graphQuery = useQuery({
    queryKey: [
      "identities",
      "data-flow",
      project.slug,
      userId,
      from.getTime(),
      to.getTime(),
    ],
    queryFn: () => fetchIdentityDataFlowGraph(client, from, to, userId!, ""),
    enabled: userId != null,
    throwOnError: false,
  });

  // Sessions are keyed on the directory user, which is the only subject the
  // session store records; an identity we only know from telemetry has none.
  const subjectUserId = identity.userIds[0];
  const sessionsQuery = useUserSessions(
    { subjectUrn: `user:${subjectUserId}`, status: "active" },
    undefined,
    { enabled: subjectUserId != null, throwOnError: false },
  );
  const sessions = sessionsQuery.data?.result.items ?? [];
  const graph = graphQuery.data;

  return (
    <IdentitySection
      title="Connections"
      meta={sectionMeta([
        { count: graph?.nodes.length ?? 0, singular: "node" },
        { count: sessions.length, singular: "live connection" },
      ])}
    >
      <div className="flex flex-col gap-4">
        {graphQuery.error ? (
          <ErrorAlert
            title="Unable to load this identity's data flow"
            error={graphQuery.error}
          />
        ) : graphQuery.isLoading ? (
          <Skeleton className="h-[360px]" />
        ) : (
          <IdentityDataFlowGraphCard
            graph={graph ?? { nodes: [], edges: [] }}
            userName={identity.displayName}
            userPhotoUrl={identity.photoUrl ?? undefined}
          />
        )}

        {/* The same component the organization page and the MCP server detail
            tab render, already scoped to this person — so a connection reads
            identically wherever an admin meets it. Grouped by MCP server:
            once the person is a given, which of our servers they hold a
            connection to is the question, not which upstream that server
            happens to broker. */}
        <IdentityPanel
          title="Active MCP connections"
          handoffLabel="MCP Sessions"
          handoffHref={handoffs.mcpSessions}
        >
          <ConnectionsListSection
            sessions={sessions}
            isPending={subjectUserId != null && sessionsQuery.isPending}
            isError={sessionsQuery.isError}
            onRetry={() => void sessionsQuery.refetch()}
            onRevoked={() => void sessionsQuery.refetch()}
            defaultGrouping="issuer"
            // The panel is already a bordered box with a heading; a second
            // frame around the table reads as a table inside a table.
            bordered={false}
            // One person's page: grouping by person would sort a list of one,
            // and the control's own band reads as an empty toolbar inside the
            // panel header that already names the section.
            showGroupingControl={false}
            emptyHeading="No active connections"
            emptyDescription="This identity has no live MCP connections."
          />
        </IdentityPanel>
      </div>
    </IdentitySection>
  );
}
