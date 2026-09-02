import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { ShareBar } from "@/components/chart/ShareBar";
import { formatPlatform } from "@/lib/formatPlatform";
import { peerStanding, standingLabel } from "./identityPeers";
import { IdentitySection } from "./IdentitySection";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes, useRoutes } from "@/routes";
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
  useIdentityPeers,
  useIdentityRisk,
  useIdentityShadowServers,
  useIdentityWindow,
  useIsSelf,
} from "./useIdentityQueries";

export default function IdentityOverview(): JSX.Element {
  const { identity, urn } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
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
  const encodedUrn = encodeIdentityUrn(urn);

  const metricsQuery = useIdentityMetrics(identity, from, to);
  const chatsQuery = useIdentityChats(identity, from, to);
  const hasChatRead = useCanReadOthersChats();
  const isSelf = useIsSelf(identity);
  const canReadOthersChats = hasChatRead || isSelf;
  const canReadRisk = useCanReadRisk();
  const auditQuery = useIdentityAuditLogs(identity, from, to);
  const riskQuery = useIdentityRisk(identity, from, to);
  const challengesQuery = useIdentityChallenges(identity, from, to);
  const shadowQuery = useIdentityShadowServers(identity, from, to);
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

  const {
    peers,
    self,
    isPending: peersPending,
    isError: peersFailed,
    refetch: refetchPeers,
  } = useIdentityPeers(identity, from, to);

  // isLoading rather than isPending throughout: a query held behind `enabled`
  // — no agent identifier, no chat:read — stays pending forever, and showing
  // that as a wait would spin a panel that is never going to fetch.
  const attentionLoading =
    riskQuery.isLoading ||
    challengesQuery.isLoading ||
    shadowQuery.isLoading ||
    devicesQuery.isLoading ||
    peersPending;

  // Every feed below contributes items to one list, so a single failed read
  // silently shortens it — and an empty list here reads as "nothing
  // outstanding", which is the one thing a security summary must not say on
  // data it never received.
  const attentionFailed =
    riskQuery.isError ||
    challengesQuery.isError ||
    shadowQuery.isError ||
    devicesQuery.isError ||
    peersFailed;
  const retryAttention = () => {
    void riskQuery.refetch();
    void challengesQuery.refetch();
    void shadowQuery.refetch();
    void devicesQuery.refetch();
    refetchPeers();
  };

  // Which surfaces the work came through. Rides on the roster row, like the
  // accounts below it — the per-user summary carries neither.
  const platforms = [...(self?.hookSources ?? [])]
    .filter((source) => source.source && source.eventCount > 0)
    .sort((a, b) => b.eventCount - a.eventCount);

  // Personal-vs-team is recorded per linked account on the roster row; the
  // per-user summary does not carry accounts at all.
  const personalAccounts = (self?.accounts ?? []).filter(
    (account) => account.accountType === "personal",
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
      href: routes.identities.detail.security.href(encodedUrn),
    },
    personalAccounts.length > 0 && {
      key: "personal-account",
      // Work done through a personal subscription sits outside the team
      // workspace: it is not covered by org policy, its spend lands on
      // someone's card, and its transcripts are not the company's to audit.
      title: `Working through ${personalAccounts.length} personal account${
        personalAccounts.length === 1 ? "" : "s"
      }`,
      detail: personalAccounts
        .map((account) => account.provider)
        .filter(Boolean)
        .join(", "),
      trailing: "Devices",
      href: routes.identities.detail.devices.href(encodedUrn),
    },
    shadowServers.length > 0 && {
      key: "shadow",
      title: `Reached ${shadowServers.length} shadow MCP server${
        shadowServers.length === 1 ? "" : "s"
      }`,
      detail: shadowServers[0]?.urlHost,
      trailing: "Security",
      href: routes.identities.detail.security.href(encodedUrn),
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
      href: routes.identities.detail.access.href(encodedUrn),
    },
    staleDevices.length > 0 && {
      key: "devices",
      title: `${staleDevices.length} device${
        staleDevices.length === 1 ? "" : "s"
      } without an active agent`,
      detail: staleDevices[0]?.hostname ?? staleDevices[0]?.serialNumber,
      trailing: "Devices",
      href: routes.identities.detail.devices.href(encodedUrn),
    },
  ];
  const attention = attentionCandidates.filter(
    (item): item is AttentionItem => item !== false,
  );

  const chats = chatsQuery.data?.chats ?? [];
  const logs = auditQuery.data?.result.logs ?? [];

  // A bare figure cannot be read: $1,250 is alarming or unremarkable
  // depending entirely on what everyone else spent. Rank against the same
  // window the figure covers.
  const standingFor = (
    field: "totalCost" | "totalToolCalls" | "totalChats",
  ): string | undefined => {
    const value = metrics?.[field] ?? 0;
    const standing = peerStanding(
      peers.map((peer) => peer[field] ?? 0),
      value,
    );
    return standing ? standingLabel(standing, value) : undefined;
  };

  return (
    <IdentitySection>
      <div className="flex flex-col gap-6">
        <IdentityPanel
          title="Needs attention"
          loading={attentionLoading}
          loadingRows={2}
          error={attentionFailed}
          onRetry={retryAttention}
          footer={
            attention.length > 0
              ? `${attention.length} item${attention.length === 1 ? "" : "s"}`
              : undefined
          }
        >
          {attention.length === 0 ? (
            <IdentityPanelEmpty>
              Nothing outstanding for this identity.
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

        {/* Each tile reads a different endpoint, so they are held back
            individually: a spend figure that has landed is worth showing while
            findings are still counting. */}
        <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
          {metricsQuery.isLoading ? (
            <>
              <StatTileSkeleton />
              <StatTileSkeleton />
              <StatTileSkeleton />
            </>
          ) : (
            <>
              <StatTile
                title="Spend"
                subtext={standingFor("totalCost")}
                value={metrics?.totalCost ?? 0}
                format="currency"
                tone="neutral"
                icon="credit-card"
              />
              <StatTile
                title="Tool calls"
                subtext={standingFor("totalToolCalls")}
                value={metrics?.totalToolCalls ?? 0}
                format="compact"
                tone="information"
                icon="wrench"
              />
              <StatTile
                title="Chats"
                subtext={standingFor("totalChats")}
                value={metrics?.totalChats ?? 0}
                format="compact"
                tone="information"
                icon="message-square"
              />
            </>
          )}
          {canReadRisk &&
            (riskQuery.isLoading ? (
              <StatTileSkeleton />
            ) : (
              <StatTile
                title="Findings"
                value={findings}
                format="compact"
                tone={findings > 0 ? "destructive" : "neutral"}
                icon="flag"
              />
            ))}
          {devicesQuery.isLoading ? (
            <StatTileSkeleton />
          ) : (
            <StatTile
              title="Devices"
              value={devices.length}
              format="compact"
              tone="neutral"
              icon="laptop"
              tooltip="Current MDM inventory. Unlike the figures beside it, this is not filtered by the selected time range — a device is assigned or it is not."
            />
          )}
        </StatTileGroup>

        {/* Which platforms someone works through, ahead of the activity
            feeds: two people with the same tool count can be running one CLI
            or juggling four, and that changes what the rest of the page
            means. Usage carries the same reading beside its tool volumes. */}
        {(peersPending || peersFailed || platforms.length > 0) && (
          <IdentityPanel
            title="Platforms"
            handoffLabel="Usage"
            handoffHref={routes.identities.detail.usage.href(encodedUrn)}
            contentClassName="px-4 py-4"
            loading={peersPending}
            loadingVariant="block"
            error={peersFailed}
            onRetry={refetchPeers}
          >
            <ShareBar
              segments={platforms.map((platform) => ({
                key: platform.source,
                label: formatPlatform(platform.source),
                value: platform.eventCount,
              }))}
              ariaLabel="Share of activity by platform"
            />
          </IdentityPanel>
        )}

        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <IdentityPanel
            title="Recent activity"
            handoffLabel="Audit Logs"
            handoffHref={handoffs.auditLogs}
            loading={auditQuery.isLoading}
            error={auditQuery.isError}
            onRetry={() => void auditQuery.refetch()}
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
            loading={chatsQuery.isLoading}
            error={chatsQuery.isError}
            onRetry={() => void chatsQuery.refetch()}
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
