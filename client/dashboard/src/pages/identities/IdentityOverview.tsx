import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { Text } from "@/components/ui/Text";
import { IdentitySection } from "./IdentitySection";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes, useRoutes } from "@/routes";
import { Info } from "lucide-react";
import { Link, useLocation } from "react-router";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import {
  useCanReadOthersChats,
  useCanReadRisk,
  useIdentityAuditLogs,
  useIdentityChallenges,
  useIdentityChats,
  useIdentityDevices,
  useIdentityMetrics,
  useIdentityProject,
  useIdentityRisk,
  useIdentityShadowServers,
  useIdentityWindow,
  useIsSelf,
} from "./useIdentityQueries";

export default function IdentityOverview(): JSX.Element {
  const { identity, urn } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
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
  const encodedUrn = encodeIdentityUrn(urn);

  const metricsQuery = useIdentityMetrics(identity, from, to);
  const chatsQuery = useIdentityChats(identity, from, to);
  const hasChatRead = useCanReadOthersChats();
  const isSelf = useIsSelf(identity);
  const canReadOthersChats = hasChatRead || isSelf;
  const canReadRisk = useCanReadRisk();
  const auditQuery = useIdentityAuditLogs(identity);
  const riskQuery = useIdentityRisk(identity, from, to);
  const challengesQuery = useIdentityChallenges(identity);
  const shadowQuery = useIdentityShadowServers(identity);
  const devicesQuery = useIdentityDevices(identity);

  const metrics = metricsQuery.data?.metrics;
  const findings = (riskQuery.data?.categories ?? []).reduce(
    (sum, c) => sum + Number(c.findings),
    0,
  );
  // Only unresolved denials are worth surfacing: a resolved one already had an
  // admin look at it.
  const deniedChallenges = (challengesQuery.data?.challenges ?? []).filter(
    (c) => c.outcome === "deny" && !c.resolvedAt,
  );
  const shadowServers = shadowQuery.data?.servers ?? [];
  const devices = devicesQuery.data?.result.devices ?? [];
  const staleDevices = devices.filter(
    (d) =>
      d.coverageBucket === "agent_stale" || d.coverageBucket === "no_agent",
  );

  type AttentionItem = {
    key: string;
    title: string;
    detail: string | undefined;
    trailing: string;
    href: string;
  };
  const attentionCandidates: Array<AttentionItem | false> = [
    findings > 0 && {
      key: "findings",
      title: `${findings} risk finding${findings === 1 ? "" : "s"}`,
      detail: (riskQuery.data?.categories ?? [])
        .slice(0, 2)
        .map((c) => c.category)
        .join(", "),
      trailing: "Security",
      href: orgRoutes.identities.detail.security.href(encodedUrn),
    },
    shadowServers.length > 0 && {
      key: "shadow",
      title: `Reached ${shadowServers.length} shadow MCP server${
        shadowServers.length === 1 ? "" : "s"
      }`,
      detail: shadowServers[0]?.urlHost,
      trailing: "Security",
      href: orgRoutes.identities.detail.security.href(encodedUrn),
    },
    deniedChallenges.length > 0 && {
      key: "challenges",
      title: `${deniedChallenges.length} denied authorization challenge${
        deniedChallenges.length === 1 ? "" : "s"
      }`,
      detail: deniedChallenges
        .slice(0, 3)
        .map((c) => c.scope)
        .join(", "),
      trailing: "Access",
      href: orgRoutes.identities.detail.access.href(encodedUrn),
    },
    staleDevices.length > 0 && {
      key: "devices",
      title: `${staleDevices.length} device${
        staleDevices.length === 1 ? "" : "s"
      } without an active agent`,
      detail: staleDevices[0]?.hostname ?? staleDevices[0]?.serialNumber,
      trailing: "Devices",
      href: orgRoutes.identities.detail.devices.href(encodedUrn),
    },
  ];
  const attention = attentionCandidates.filter(
    (item): item is AttentionItem => item !== false,
  );

  const chats = chatsQuery.data?.chats ?? [];
  const logs = auditQuery.data?.result.logs ?? [];

  return (
    <IdentitySection>
      <div className="flex flex-col gap-6">
        <div className="text-muted-foreground flex items-start gap-2">
          <Info className="mt-0.5 size-3.5 shrink-0" />
          <Text variant="small" muted className="text-xs">
            Each panel shows this identity&rsquo;s slice. Open in links lead to
            the page that owns the data.
          </Text>
        </div>

        <IdentityPanel
          title="Needs attention"
          footer={
            attention.length > 0
              ? `${attention.length} item${attention.length === 1 ? "" : "s"}`
              : undefined
          }
        >
          {attention.length === 0 ? (
            <IdentityPanelEmpty>
              Nothing outstanding in this window.
            </IdentityPanelEmpty>
          ) : (
            attention.map((item) => (
              <Link key={item.key} to={item.href} className="block">
                <IdentityPanelRow
                  accent="destructive"
                  title={item.title}
                  detail={item.detail}
                  trailing={item.trailing}
                />
              </Link>
            ))
          )}
        </IdentityPanel>

        <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
          <StatTile
            title="Spend"
            value={metrics?.totalCost ?? 0}
            format="currency"
            tone="neutral"
            icon="credit-card"
          />
          <StatTile
            title="Tool calls"
            value={metrics?.totalToolCalls ?? 0}
            format="compact"
            tone="information"
            icon="wrench"
          />
          <StatTile
            title="Chats"
            value={metrics?.totalChats ?? 0}
            format="compact"
            tone="information"
            icon="message-square"
          />
          {canReadRisk && (
            <StatTile
              title="Findings"
              value={findings}
              format="compact"
              tone={findings > 0 ? "destructive" : "neutral"}
              icon="flag"
            />
          )}
          <StatTile
            title="Devices"
            value={devices.length}
            format="compact"
            tone="neutral"
            icon="laptop"
          />
        </StatTileGroup>

        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <IdentityPanel
            title="Recent activity"
            handoffLabel="Audit Logs"
            handoffHref={handoffs.auditLogs}
            footer={
              logs.length > 0
                ? `Actor filtered to ${identity.displayName}`
                : undefined
            }
          >
            {logs.length === 0 ? (
              <IdentityPanelEmpty>
                No recorded changes by this identity.
              </IdentityPanelEmpty>
            ) : (
              logs
                .slice(0, 3)
                .map((log) => (
                  <IdentityPanelRow
                    key={log.id}
                    title={log.action}
                    detail={log.subjectDisplayName ?? log.subjectType}
                    trailing={<HumanizeDateTime date={log.createdAt} />}
                  />
                ))
            )}
          </IdentityPanel>

          <IdentityPanel
            title="Recent chats"
            handoffLabel="Agent Sessions"
            handoffHref={handoffs.agentSessions}
            footer={
              chatsQuery.data
                ? `${chatsQuery.data.total ?? chats.length} session${
                    (chatsQuery.data.total ?? chats.length) === 1 ? "" : "s"
                  } this period`
                : undefined
            }
          >
            {!canReadOthersChats ? (
              <IdentityPanelEmpty>
                Listing someone else&rsquo;s sessions needs the chat:read
                permission.
              </IdentityPanelEmpty>
            ) : chats.length === 0 ? (
              <IdentityPanelEmpty>
                No chat sessions in this window.
              </IdentityPanelEmpty>
            ) : (
              chats
                .slice(0, 3)
                .map((chat) => (
                  <IdentityPanelRow
                    key={chat.id}
                    title={chat.title || "Untitled chat"}
                    detail={
                      chat.lastMessageTimestamp ? (
                        <HumanizeDateTime date={chat.lastMessageTimestamp} />
                      ) : undefined
                    }
                    trailing={`${chat.numMessages ?? 0} msgs`}
                  />
                ))
            )}
          </IdentityPanel>
        </div>
      </div>
    </IdentitySection>
  );
}
