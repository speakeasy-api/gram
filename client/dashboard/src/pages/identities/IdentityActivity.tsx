import { HumanizeDateTime } from "@/lib/dates";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { DailyActivityChart } from "@/components/chart/DailyActivityChart";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  useCanReadOthersChats,
  useIdentityAuditLogs,
  useIdentityChats,
  useIdentityProject,
  useIdentityWindow,
  useIsSelf,
} from "./useIdentityQueries";

// The lists are a recent sample, not an archive: the handoff owns the full
// history and a 20-row list only made this tab a longer Overview.
const RECENT_ROWS = 6;

export default function IdentityActivity(): JSX.Element {
  const { identity } = useIdentityOutlet();
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

  const auditQuery = useIdentityAuditLogs(identity, from, to);
  const chatsQuery = useIdentityChats(identity, from, to, 20);
  const hasChatRead = useCanReadOthersChats();
  const isSelf = useIsSelf(identity);
  const canReadOthersChats = hasChatRead || isSelf;

  const logs = auditQuery.data?.result.logs ?? [];
  const chats = chatsQuery.data?.chats ?? [];
  // isLoading rather than isPending: the chat query is held behind `enabled`
  // for a viewer without chat:read, and that is a refusal to fetch, not a wait.
  const activityLoading = auditQuery.isLoading || chatsQuery.isLoading;

  const logDates = logs.map((log) => new Date(log.createdAt));
  const chatDates = chats
    .map((chat) => chat.lastMessageTimestamp)
    .filter((ts): ts is Date => Boolean(ts))
    .map((ts) => new Date(ts));

  return (
    <IdentitySection
      title="Activity"
      meta={sectionMeta([
        { count: logs.length, singular: "change" },
        { count: chats.length, singular: "session" },
      ])}
    >
      {/* Overview already shows the most recent handful of both lists. What
          this tab can add is the shape of the window: two people with
          identical row lists can work in daily drips or in two long bursts,
          and only the columns tell them apart. */}
      {(activityLoading || logDates.length > 0 || chatDates.length > 0) && (
        <IdentityPanel
          title="Activity by day"
          loading={activityLoading}
          loadingVariant="block"
        >
          <div className="px-4 py-4">
            <DailyActivityChart
              from={from}
              to={to}
              series={[
                { key: "changes", label: "Changes", dates: logDates },
                { key: "sessions", label: "Sessions", dates: chatDates },
              ]}
            />
          </div>
        </IdentityPanel>
      )}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <IdentityPanel
          title="Audit trail"
          handoffLabel="Audit Logs"
          handoffHref={handoffs.auditLogs}
          loading={auditQuery.isLoading}
          loadingRows={RECENT_ROWS}
          footer={
            // Audit logs key on the Gram user id, so a subject with no
            // directory row has nothing here even when it has telemetry.
            identity.userIds.length === 0
              ? "This identity resolves to no Gram user, so no change is recorded under it."
              : `Actor filtered to ${identity.displayName}`
          }
        >
          {logs.length === 0 ? (
            <IdentityPanelEmpty>
              No recorded changes by this identity.
            </IdentityPanelEmpty>
          ) : (
            logs.slice(0, RECENT_ROWS).map((log) => (
              <IdentityPanelRow
                key={log.id}
                title={log.action}
                detail={[
                  log.subjectDisplayName ?? log.subjectType,
                  log.projectSlug,
                  // The API sends "unknown" when no surface was identified;
                  // printing that word says less than saying nothing.
                  log.actingSurface === "unknown" ? "" : log.actingSurface,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                trailing={<HumanizeDateTime date={log.createdAt} />}
              />
            ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Chat sessions"
          handoffLabel="Agent Sessions"
          handoffHref={handoffs.agentSessions}
          loading={chatsQuery.isLoading}
          loadingRows={RECENT_ROWS}
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
              .slice(0, RECENT_ROWS)
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
    </IdentitySection>
  );
}
