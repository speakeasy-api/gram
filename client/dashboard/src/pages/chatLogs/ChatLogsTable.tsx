import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildLoadChatOverviewQuery } from "@gram/client/react-query/loadChatOverview.js";
import { buildListChatSessionLinksQuery } from "@gram/client/react-query/listChatSessionLinks.js";
import { SlackChannelLink } from "@/components/slack-channel-link";
import { ChatOwnerLabel } from "@/components/chat-owner-label";
import { chatOwnerLabel } from "@/lib/chat-owner";
import { AssistantActorLink } from "@/components/assistant-actor-link";
import { ActingPrincipal, PersonFace } from "@/components/ui/ActingPrincipal";
import type { Principal } from "@/components/ui/ActingPrincipal/text";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { personalAccountEmail } from "@/components/observe/account-display-utils";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { Dialog } from "@/components/ui/Dialog";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { formatChatSource } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { WorkUnitsRowMetrics } from "@/pages/chatLogs/WorkUnitsMetrics";
import { useSession } from "@/contexts/Auth";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { useChatSetPinnedMutation } from "@gram/client/react-query/chatSetPinned.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useListChatSessionLinks } from "@gram/client/react-query/listChatSessionLinks.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Table, type Column, type SortDescriptor } from "@/components/ui/Table";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import {
  ArrowUpRight,
  CircleDashed,
  GitBranch,
  History,
  Pin,
} from "lucide-react";
import {
  cloneElement,
  useCallback,
  useMemo,
  useState,
  type MouseEvent,
  type ReactElement,
} from "react";
import { toast } from "sonner";
import { formatPlatform } from "@/lib/formatPlatform";
import {
  groupSubsessions,
  summarizeLineage,
  type LineageSummary,
} from "./sessionLinks";

interface ChatLogsTableProps {
  chats: ChatOverview[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverview) => void;
  onDeleteChat: (chatId: string) => void;
  isLoading: boolean;
  error: Error | null;
  emptyState?: {
    title: string;
    description: string;
  };
  /**
   * Which column the server ordered by, and which way.
   *
   * Owned by the page, not by this table: the ordering is a query parameter
   * the API sorted on, so only the component that issues the query can honour
   * a change to it. A table that sorted its own rows would reorder the page
   * it happens to be showing and leave the rest of the result set behind it.
   */
  sort?: SortDescriptor | null;
  onSortChange?: (sort: SortDescriptor | null) => void;
}

/** One table row: a session and how deeply it is nested under its parent. */
type SessionRow = { session: ChatOverview; depth: number };

function getTraceId(chatId: string): string {
  return chatId.slice(0, 8);
}

/**
 * Who ran the session. Three shapes share the column: a conversation with
 * several people (the facepile), an assistant acting under one person's
 * authority (the two-line relation), and a person on their own.
 */
/**
 * A channel session's agent, linked only once it resolves through an agent
 * inventory the reader may read: an id on a chat row is not permission to open
 * the agent, and an agent deleted since capture has no name to resolve.
 */
function ChannelAgentPrincipal({
  agentId,
  agentName,
  participants,
}: {
  agentId: string;
  agentName: string | undefined;
  participants: Principal[];
}): JSX.Element {
  const agents = useReadableAgents(true);
  const readable =
    !agents.isError &&
    (agents.data ?? []).some((agent) => agent.id === agentId);
  return (
    <ActingPrincipal
      agent={{ id: agentId, name: agentName }}
      linked={Boolean(agentName) && readable}
      authorizer={participants}
    />
  );
}

function ActingPrincipalCell({
  chat,
  members,
  currentUser,
}: {
  chat: ChatOverview;
  members: AccessMember[] | undefined;
  currentUser: { id: string; email: string };
}): JSX.Element {
  // Participants are authorizers: a channel session is an agent acting for
  // the room it is in. ActingPrincipal takes the whole list and overlaps the
  // faces, so this cell no longer has a facepile branch of its own that
  // short-circuited before the agent was ever named.
  const participants = (chat.participants ?? []).map((participant) => ({
    // The mapped Speakeasy person when capture resolved one — that is the id
    // an identity link can key on. Without one the whole provider identity
    // stands in: `provider:user` alone collides between workspaces, which a
    // shared channel puts in the same conversation.
    id:
      participant.userId ??
      `${participant.provider}:${participant.providerTeamId ?? ""}:${participant.providerUserId}`,
    name: participant.displayName ?? participant.providerUserId,
    // Only a participant capture resolved to a person has an identity page;
    // the rest are provider handles the resolver cannot address, and they
    // render as a named face rather than a link to nothing.
    identity: participant.userId ? { userId: participant.userId } : undefined,
  }));

  if (chat.assistantName && participants.length > 0) {
    return (
      <ActingPrincipal
        agent={{ id: chat.assistantAgentId ?? "", name: chat.assistantName }}
        linked={Boolean(chat.assistantAgentId)}
        authorizer={participants}
      />
    );
  }

  // A channel session has no assistant record; the agent principal that
  // produced it stands in, acting for the room.
  if (!chat.assistantName && chat.agentId && participants.length > 0) {
    return (
      <ChannelAgentPrincipal
        agentId={chat.agentId}
        agentName={chat.agentName}
        participants={participants}
      />
    );
  }

  // A channel session with neither an assistant nor an agent: the people are
  // all there is to name.
  if (participants.length > 0) {
    return <ParticipantFacepile people={participants} />;
  }

  // The person an assistant acted for, by whichever id the session carries.
  // An assistant reached over a federated surface reports only the external
  // id, and guarding on `userId` alone dropped the person from those rows
  // entirely rather than naming them.
  const authorizerId = chat.userId ?? chat.externalUserId;

  if (chat.assistantName && authorizerId) {
    return (
      // No account-type icon: ActingPrincipal draws the agent's own face,
      // and the two marks side by side read as two actors.
      <div className="flex min-w-0 items-start gap-1.5">
        <ActingPrincipal
          agent={{
            id: chat.assistantAgentId ?? "",
            name: chat.assistantName,
          }}
          // Linked only when the assistant actually has an agent identity:
          // without one the urn resolves to nothing.
          linked={Boolean(chat.assistantAgentId)}
          authorizer={{
            id: authorizerId,
            name: chatOwnerLabel(
              members,
              chat,
              currentUser,
              personalAccountEmail(chat),
            ),
            // Keyed on whichever id the session holds: an external id is a
            // reference of its own kind, and sending it as a member id would
            // link to a person who does not exist.
            identity: chat.userId
              ? { userId: chat.userId }
              : { externalUserId: authorizerId },
          }}
        />
      </div>
    );
  }

  // An assistant (or a channel session's agent) with nobody behind it: named
  // on its own, because there is no person whose authority it borrowed.
  if (chat.assistantName || chat.agentId) {
    return <AssistantActorLink chat={chat} className="min-w-0" />;
  }

  // The same face ActingPrincipal gives the person on an agent row, so a
  // column holding both does not switch between a tinted initial and a
  // generic outline icon halfway down.
  const ownerLabel = chatOwnerLabel(
    members,
    chat,
    currentUser,
    personalAccountEmail(chat),
  );
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <PersonFace
        person={{ id: chat.userId ?? ownerLabel, name: ownerLabel }}
      />
      <span className="min-w-0 truncate">
        <ChatOwnerLabel
          members={members}
          chat={chat}
          currentUser={currentUser}
          accountEmail={personalAccountEmail(chat)}
        />
      </span>
    </span>
  );
}

/**
 * Everyone in a conversation, as overlapped faces.
 *
 * Each face carries its own name: `PersonFace` is `aria-hidden`, so a bare
 * row of them says nothing to a screen reader and nothing to a mouse either.
 * The people past the fifth are counted rather than dropped — five faces and
 * no marker reads as the whole room.
 */
function ParticipantFacepile({
  people,
}: {
  people: { id: string; name: string }[];
}): JSX.Element {
  const overflow = people.slice(5);
  return (
    <span
      role="list"
      aria-label="Conversation participants"
      className="flex shrink-0 -space-x-1"
    >
      {people.slice(0, 5).map((person) => (
        <SimpleTooltip key={person.id} tooltip={person.name}>
          <span role="listitem" className="flex">
            <PersonFace person={person} />
            <span className="sr-only">{person.name}</span>
          </span>
        </SimpleTooltip>
      ))}
      {overflow.length > 0 && (
        <SimpleTooltip
          tooltip={overflow.map((person) => person.name).join(", ")}
        >
          <span
            role="listitem"
            className="bg-card text-muted-foreground flex size-4 shrink-0 items-center justify-center rounded-full text-[8px]"
          >
            +{overflow.length}
            <span className="sr-only">{overflow.length} more participants</span>
          </span>
        </SimpleTooltip>
      )}
    </span>
  );
}

/**
 * How many risk findings a session has, beside its title.
 *
 * Beside the title rather than in a column of its own: every column is
 * already at its minimum width, and a risk column stands empty on every row
 * of an organization that runs no policies. Drawn only when the count is
 * non-zero — the fact worth seeing on a scan is that a session has any, and
 * a column of zeroes buries it.
 */
function SessionRiskMarker({ count }: { count: number }): JSX.Element {
  const label = `${count} risk finding${count === 1 ? "" : "s"} on this session`;
  return (
    <SimpleTooltip tooltip={label}>
      <span
        role="img"
        aria-label={label}
        className="text-destructive inline-flex shrink-0 items-center gap-0.5 tabular-nums"
      >
        <Icon name="triangle-alert" className="size-3.5" />
        {count}
      </span>
    </SimpleTooltip>
  );
}

/** The product the session ran in, and the Slack channel when it had one. */
function SurfaceCell({ chat }: { chat: ChatOverview }): JSX.Element | null {
  const source = chat.source;
  if (!source && !chat.slackChannelId) return null;

  return (
    <Cell
      primary={
        source ? (
          <>
            <AgentProviderIcon source={source} className="size-3.5 shrink-0" />
            <span className="min-w-0 truncate">
              {formatChatSource(source, chat)}
            </span>
          </>
        ) : null
      }
      secondary={
        chat.slackChannelId ? (
          <SlackChannelLink
            channelId={chat.slackChannelId}
            channelName={chat.slackChannelName}
            teamId={chat.slackTeamId}
          />
        ) : undefined
      }
    />
  );
}

/**
 * One cell of the session table: a first line every column shares a baseline
 * on, and an optional second line under it.
 *
 * Without a fixed first-line height the columns drifted apart — a `text-sm`
 * title, a `text-xs` mono timestamp and a 20px agent face each set their own
 * line box, so no two cells in a row lined up and the table read as a pile.
 */
function Cell({
  primary,
  secondary,
  className,
}: {
  primary: React.ReactNode;
  secondary?: React.ReactNode;
  className?: string;
}): JSX.Element {
  // Every column starts at its own left edge, numbers included, so a value
  // begins under the first letter of its header. Right-aligning the numeric
  // columns put the digits against the next column's border instead, which
  // read as belonging to it.
  //
  // `flex-1`, not `w-full`: the table's own `<td>` is itself a flex row with
  // an indentation sibling, so a width percentage only shrink-wrapped.
  return (
    <div className={cn("flex min-w-0 flex-1 flex-col gap-0.5", className)}>
      {/* One size throughout: the rows mixed `text-sm` titles with `text-xs`
          everything-else, which made most of the table look like a footnote
          beside its own first column. The second line is distinguished by
          colour, not by shrinking it. */}
      <div className="flex min-h-5 min-w-0 items-center gap-1.5 text-sm">
        {primary}
      </div>
      {secondary !== undefined && (
        <div className="text-muted-foreground flex h-5 min-w-0 items-center gap-1 text-sm">
          {secondary}
        </div>
      )}
    </div>
  );
}

/** When the session started, when it last moved, and how long that spanned. */
function ActivityCell({ chat }: { chat: ChatOverview }): JSX.Element {
  const lastActivityTimestamp = chat.lastMessageTimestamp ?? chat.createdAt;
  // One date, not two. Both timestamps are nearly always the same minute of
  // the same day, so printing each in full wrapped the column mid-date and
  // read as four facts where there are two: when it started, and how long it
  // ran. The exact pair stays in the title attribute.
  return (
    <Cell
      className="font-mono whitespace-nowrap"
      primary={
        <span
          className="text-muted-foreground"
          title={`Created ${format(chat.createdAt, "MMM d, HH:mm")} · Last activity ${format(
            lastActivityTimestamp,
            "MMM d, HH:mm",
          )}`}
        >
          {format(chat.createdAt, "MMM d, HH:mm")}
        </span>
      }
      secondary={<span className="tabular-nums">{formatDuration(chat)}</span>}
    />
  );
}

// listHarnesses renders a harness list for a lineage tooltip ("Claude Code",
// "Claude Code and Cursor").
function listHarnesses(harnesses: string[]): string {
  const labels = [...new Set(harnesses.map(formatPlatform))];
  if (labels.length <= 1) return labels[0] ?? "";
  return `${labels.slice(0, -1).join(", ")} and ${labels[labels.length - 1]}`;
}

// SessionLineageIcons is the at-a-glance lineage cluster on a session row:
// one icon per state — continued elsewhere (navigable continuation exists),
// derived from an earlier session, and moved with no captured continuation.
// A button is needed for the tooltips, and the table row does not treat a
// click that started on a control as its own, so the cluster forwards to the
// same row selection itself; the row is the tab stop, hence tabIndex={-1}.
function SessionLineageIcons({
  summary,
  onActivate,
}: {
  summary?: LineageSummary;
  onActivate: () => void;
}) {
  if (!summary) return null;
  return (
    <button
      type="button"
      tabIndex={-1}
      onClick={onActivate}
      aria-label="Open session details"
      className="text-muted-foreground inline-flex shrink-0 cursor-pointer items-center gap-1"
    >
      {summary.continuedIn.length > 0 && (
        <SimpleTooltip
          tooltip={`Continued in ${listHarnesses(summary.continuedIn)}`}
        >
          <ArrowUpRight
            className="size-3.5"
            aria-label="Continued in another harness"
          />
        </SimpleTooltip>
      )}
      {summary.derived && (
        <SimpleTooltip tooltip="Derived from an earlier session">
          <GitBranch
            className="size-3.5"
            aria-label="Derived from an earlier session"
          />
        </SimpleTooltip>
      )}
      {summary.danglingIn.length > 0 && (
        <SimpleTooltip
          tooltip={`Moved to ${listHarnesses(summary.danglingIn)} — continuation not yet captured`}
        >
          <CircleDashed
            className="size-3.5"
            aria-label="Moved, continuation not yet captured"
          />
        </SimpleTooltip>
      )}
      {summary.recalledCount > 0 && (
        <SimpleTooltip
          tooltip={
            summary.recalledCount === 1
              ? "Recalled into a later session"
              : `Recalled into ${summary.recalledCount} later sessions`
          }
        >
          <History
            className="size-3.5"
            aria-label="Recalled into a later session"
          />
        </SimpleTooltip>
      )}
    </button>
  );
}

function formatDuration(chat: ChatOverview): string {
  // Use lastMessageTimestamp if available, otherwise fall back to updatedAt
  const endTime = chat.lastMessageTimestamp ?? chat.updatedAt;
  const seconds = Math.round(
    (endTime.getTime() - chat.createdAt.getTime()) / 1000,
  );
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return remainingSeconds > 0
    ? `${minutes}m ${remainingSeconds}s`
    : `${minutes}m`;
}

function SessionPinButton({
  chatId,
  pinned,
}: {
  chatId: string;
  pinned: boolean;
}) {
  const queryClient = useQueryClient();
  const setPinned = useChatSetPinnedMutation();

  const toggle = (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setPinned.mutate(
      {
        request: { setPinnedRequestBody: { id: chatId, pinned: !pinned } },
      },
      {
        onSettled: () => void invalidateAllListChats(queryClient),
        onError: (err) => {
          toast.error(err.message || "Failed to update pin");
        },
      },
    );
  };

  return (
    <button
      type="button"
      aria-label={pinned ? "Unpin session" : "Pin session"}
      title={pinned ? "Unpin session" : "Pin session"}
      disabled={setPinned.isPending}
      onClick={toggle}
      className={cn(
        "hover:bg-muted text-muted-foreground hover:text-foreground p-1 transition-all",
        pinned
          ? "text-foreground opacity-100"
          : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
        setPinned.isPending && "opacity-50",
      )}
    >
      <Pin className={cn("size-4", pinned && "fill-current")} aria-hidden />
    </button>
  );
}

// Subtle copy button - always visible
function CopyButton({
  value,
  label,
  className,
}: {
  value: string;
  label: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation(); // Don't trigger row selection
      void navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    },
    [value],
  );

  return (
    <button
      type="button"
      onClick={handleCopy}
      className={cn(
        "cursor-pointer p-0.5 transition-colors",
        "opacity-50 hover:opacity-100",
        "hover:bg-muted/80",
        copied && "opacity-100",
        className,
      )}
      title={`Copy ${label}`}
      aria-label={`Copy ${label}`}
    >
      <Icon
        name={copied ? "check" : "copy"}
        className={cn(
          "size-3.5",
          copied ? "text-foreground" : "text-muted-foreground",
        )}
      />
    </button>
  );
}

export function ChatLogsTable({
  chats,
  selectedChatId,
  onSelectChat,
  onDeleteChat,
  isLoading,
  error,
  emptyState,
  sort,
  onSortChange,
}: ChatLogsTableProps): JSX.Element {
  const { user } = useSession();
  const { data: membersData } = useMembers();

  // One batched lineage lookup for the visible rows. Optional decoration:
  // errors are swallowed (never blank the list) and the endpoint caps at 100
  // ids, matching the list's maximum page size.
  const client = useGramContext();
  const queryClient = useQueryClient();
  const chatIds = useMemo(
    () => chats.slice(0, 100).map((chat) => chat.id),
    [chats],
  );
  const { data: linksData } = useListChatSessionLinks({ chatIds }, undefined, {
    enabled: chatIds.length > 0,
    throwOnError: false,
    retry: false,
  });
  const { data: ancestors } = useQuery({
    queryKey: ["chatSubsessionAncestors", chatIds, linksData?.links],
    enabled: !!linksData?.links.some((link) => link.kind === "subagent"),
    retry: false,
    throwOnError: false,
    queryFn: async () => {
      const links = [...(linksData?.links ?? [])];
      const parents: ChatOverview[] = [];
      const visited = new Set(chatIds);
      let requested = 0;
      // Fetch authorized overview metadata without opening transcripts. Bound
      // ancestor depth and count, and retain only the exact requested IDs.
      for (let depth = 0; depth < 10 && requested < 100; depth++) {
        const ids = [
          ...new Set(
            links
              .filter(
                (link) =>
                  link.kind === "subagent" &&
                  link.parentCaptured &&
                  link.parentChatId &&
                  !visited.has(link.parentChatId),
              )
              .map((link) => link.parentChatId!),
          ),
        ].slice(0, 100 - requested);
        if (!ids.length) break;
        requested += ids.length;
        for (const id of ids) visited.add(id);
        for (let offset = 0; offset < ids.length; offset += 8) {
          const loaded = await Promise.allSettled(
            ids.slice(offset, offset + 8).map((id) => {
              const query = buildLoadChatOverviewQuery(client, {
                id,
              });
              return queryClient.fetchQuery({ ...query, staleTime: 60_000 });
            }),
          );
          for (const result of loaded)
            if (result.status === "fulfilled" && result.value)
              parents.push(result.value);
        }
        const query = buildListChatSessionLinksQuery(client, { chatIds: ids });
        const next = await queryClient.fetchQuery({
          ...query,
          staleTime: 60_000,
        });
        links.push(...next.links);
      }
      return { parents, links };
    },
  });
  const lineageByChat = useMemo(() => {
    const map = new Map<string, LineageSummary>();
    const links = linksData?.links ?? [];
    for (const id of chatIds) {
      const summary = summarizeLineage(links, id);
      if (summary) map.set(id, summary);
    }
    return map;
  }, [chatIds, linksData]);
  const groupedChats = useMemo(
    () =>
      groupSubsessions(
        [...chats, ...(ancestors?.parents ?? [])],
        ancestors?.links ?? linksData?.links ?? [],
      ),
    [chats, linksData, ancestors],
  );
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);

  const columns: Column<SessionRow>[] = [
    {
      key: "session",
      header: "Session",
      // Capped. Without a width this column was the only flexible track, so
      // every pixel of a wide display landed between the title and the next
      // column instead of being shared out.
      width: "30fr",
      render: ({ session: chat, depth }) => (
        // No indent for depth. Nesting only reads as nesting while the child
        // sits directly under its parent, and any sort breaks that, leaving a
        // row shifted right for no reason the reader can see. The "Subagent"
        // marker beside the title says it instead, wherever the row ends up.
        <div className="flex min-w-0 items-center gap-3">
          <Cell
            primary={
              <>
                <span className="text-foreground line-clamp-3 min-w-0 text-sm font-medium">
                  {chat.title}
                </span>
                {(chat.riskFindingsCount ?? 0) > 0 && (
                  <SessionRiskMarker count={chat.riskFindingsCount!} />
                )}
                {depth > 0 && (
                  <span className="text-muted-foreground inline-flex shrink-0 items-center gap-1">
                    <GitBranch className="size-3" aria-hidden />
                    Subagent
                  </span>
                )}
                <SessionLineageIcons
                  summary={lineageByChat.get(chat.id)}
                  onActivate={() => onSelectChat(chat)}
                />
              </>
            }
            secondary={
              <span className="flex items-center gap-1 font-mono">
                {getTraceId(chat.id)}
                <CopyButton value={chat.id} label="Chat ID" />
              </span>
            }
          />
        </div>
      ),
    },
    {
      key: "principal",
      header: "Acting principal",
      // Takes the slack now: it holds two names on an agent row, and is the
      // column most likely to need the room.
      width: "26fr",
      render: ({ session: chat }) => (
        <ActingPrincipalCell
          chat={chat}
          members={membersData?.members}
          currentUser={user}
        />
      ),
    },
    {
      key: "surface",
      header: "Surface",
      width: "20fr",
      render: ({ session: chat }) => <SurfaceCell chat={chat} />,
    },
    {
      key: "activity",
      // `id` is what the sort descriptor carries, and this page puts that
      // straight into `?sort=`, so it has to be the value the API already
      // knows rather than the column's own key.
      id: "chronological",
      header: "Activity",
      sortable: true,
      // Ordered by the server, over the whole result set. `sortValue` is only
      // here because the type demands it: the rows arrive already in order,
      // and sorting this page in the browser would reorder 25 rows while
      // claiming to have ordered all of them.
      sortValue: ({ session: chat }) =>
        chat.lastMessageTimestamp ?? chat.createdAt,
      width: "170px",
      render: ({ session: chat }) => <ActivityCell chat={chat} />,
    },
    {
      key: "numMessages",
      id: "messageCount",
      header: "Messages",
      sortable: true,
      sortValue: ({ session: chat }) => chat.numMessages,
      // Wide enough for the header itself: at 80px the uppercase label and
      // its sort control truncated to "MES…", which names no column.
      width: "120px",
      render: ({ session: chat }) => (
        <Cell
          primary={<span className="tabular-nums">{chat.numMessages}</span>}
        />
      ),
    },
    {
      key: "totalCost",
      header: "Cost",
      width: "110px",
      render: ({ session: chat }) => (
        <Cell
          primary={
            chat.totalCost !== undefined && chat.totalCost > 0 ? (
              <span className="tabular-nums">${chat.totalCost.toFixed(4)}</span>
            ) : null
          }
          // Work analysis is spend per unit of work delivered, so it reads
          // against the cost rather than in a column of its own that is
          // empty for every organization not running the analysis.
          secondary={<WorkUnitsRowMetrics chat={chat} />}
        />
      ),
    },
    {
      key: "actions",
      header: "",
      width: "90px",
      render: ({ session: chat }) => (
        <div className="flex w-full items-center justify-end gap-1">
          <SessionPinButton chatId={chat.id} pinned={Boolean(chat.pinned)} />
          <button
            type="button"
            onClick={() => setDeleteConfirmId(chat.id)}
            className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive p-1 opacity-0 transition-all group-hover:opacity-100 focus-visible:opacity-100"
            aria-label="Delete chat"
          >
            <Icon name="trash-2" className="size-4" />
          </button>
          <Icon
            name="chevron-right"
            className={cn(
              "size-4 transition-colors",
              selectedChatId === chat.id
                ? "text-foreground/60"
                : "text-muted-foreground/40",
            )}
          />
        </div>
      ),
    },
  ];

  if (isLoading && chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3">
          <div className="border-muted-foreground/30 border-t-muted-foreground size-5 animate-spin rounded-full border-2" />
          <span className="text-muted-foreground text-sm">
            Loading traces...
          </span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3 px-4 text-center">
          <Icon name="triangle-alert" className="text-destructive size-5" />
          <div>
            <p className="text-foreground text-sm font-medium">
              Failed to load traces
            </p>
            <p className="text-muted-foreground mt-1 text-xs">
              {error.message}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3 px-4 text-center">
          <Icon name="inbox" className="text-muted-foreground size-5" />
          <div>
            <p className="text-foreground text-sm font-medium">
              {emptyState?.title ?? "No traces found"}
            </p>
            <p className="text-muted-foreground mt-1 text-xs">
              {emptyState?.description ??
                "Try adjusting your filters or time range"}
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <>
      <Table
        // No border of its own: the list template already draws one around
        // this region, and the two together read as a frame inside a frame.
        className="bg-card border-none"
        columns={columns}
        // Sorted here, not by the table: `Table` renders the sortable headers
        // and reports the choice, and leaves the ordering to the caller.
        //
        // Only the rows already loaded. A subagent also stops sitting under
        // its parent once any other column leads, which is why the row says
        // "Subagent" beside the title instead of relying on its position.
        data={groupedChats}
        rowKey={(row) => row.session.id}
        sort={sort}
        onSortChange={onSortChange}
        onRowClick={(row) => onSelectChat(row.session)}
        renderRow={(row, rowElement) => (
          // The context menu needs one element that forwards refs, which the
          // row element is. Tinting happens here too, because a per-row class
          // is the one thing a column cannot express.
          <TableRowContextMenu
            actions={[
              {
                label: "Delete chat",
                destructive: true,
                onClick: () => setDeleteConfirmId(row.session.id),
              },
            ]}
          >
            {cloneElement(rowElement as ReactElement<{ className?: string }>, {
              className: cn(
                // "group" is what reveals the pin and delete controls on
                // hover; they are invisible until the row is pointed at.
                "group",
                row.depth > 0 && "bg-muted/20",
                selectedChatId === row.session.id && "bg-primary/5",
              ),
            })}
          </TableRowContextMenu>
        )}
      />

      <Dialog
        open={deleteConfirmId !== null}
        onOpenChange={(open) => {
          void (!open && setDeleteConfirmId(null));
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete chat session</Dialog.Title>
            <Dialog.Description>
              Are you sure you want to delete this chat session? This action
              cannot be undone.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Dialog.Close asChild>
              <Button variant="secondary">Cancel</Button>
            </Dialog.Close>
            <Button
              variant="destructive-primary"
              onClick={() => {
                if (deleteConfirmId) {
                  onDeleteChat(deleteConfirmId);
                }
                setDeleteConfirmId(null);
              }}
            >
              Delete
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
