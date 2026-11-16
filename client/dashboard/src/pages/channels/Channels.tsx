import { InlineEmptyState } from "@/components/inline-empty-state";
import { PageEyebrow } from "@/components/page-eyebrow";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { FullBleedPage } from "@/components/page-templates";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Skeleton } from "@/components/ui/Skeleton";
import { useHideInsightsDock } from "@/components/insights-context";
import { useOrganization, useProject, useUser } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { cn } from "@/lib/utils";
import { useMembers } from "@gram/client/react-query/members.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { ChannelsHome } from "./ChannelsHome";
import { ChannelsSettings } from "./ChannelsSettings";
import { ConnectAgent } from "./ConnectAgent";
import { ConversationList, type ListFilter } from "./ConversationList";
import { ConversationPane } from "./Conversation";
import { PresenceRail } from "./PresenceRail";
import { ChannelsRequestError, type ChannelThread } from "./api";
import { historyOf } from "./messages";
import {
  audienceOf,
  conversationsFrom,
  directThreadBetween,
  presenceOf,
  setupProgressOf,
} from "./model";
import {
  useChannelsWorkspace,
  useReadMarks,
  useSetupHidden,
} from "./useChannels";

export default function Channels(): JSX.Element {
  const organization = useOrganization();
  const user = useUser();
  const project = useProject();
  // Gate the page as well as its nav entry so a guessed URL does not reach
  // Channels conversations or settings before the rollout.
  const rollout = useFeatureFlag(FEATURE_FLAGS.channels);
  if (rollout.status === "loading") {
    return (
      <FullBleedPage>
        <LoadingBoard />
      </FullBleedPage>
    );
  }
  if (rollout.status !== "enabled") {
    return (
      <FullBleedPage>
        <div className="flex flex-1 items-center justify-center p-8">
          <InlineEmptyState
            icon="messages-square"
            heading="This page is not available yet"
            description="It is in preview with a few organizations. Ask your Speakeasy contact to turn it on."
          />
        </div>
      </FullBleedPage>
    );
  }
  // The project snapshot resolves native grants AND per-person exceptions.
  // Native RBAC alone cannot decide access; never display content before it.
  return (
    <FullBleedPage>
      <ChannelsBoard
        key={`${organization.id}:${project.id}:${user.id}`}
        organizationId={organization.id}
        projectId={project.id}
        userId={user.id}
      />
    </FullBleedPage>
  );
}

function ChannelsBoard({
  organizationId,
  projectId,
  userId,
}: {
  organizationId: string;
  projectId: string;
  userId: string;
}): JSX.Element {
  // The page is itself a conversation surface, so the floating assistant dock
  // would sit on top of the composer.
  useHideInsightsDock();
  const workspace = useChannelsWorkspace(organizationId, userId);
  const [marks, markRead] = useReadMarks(
    `speakeasy.channels.read.v1.${organizationId}.${projectId}.${userId}`,
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = searchParams.get("thread") ?? "";
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState<ListFilter>("all");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [setupHidden, hideSetup] = useSetupHidden(
    `speakeasy.channels.setup-hidden.v1.${organizationId}.${projectId}.${userId}`,
  );
  const connectRef = useRef<HTMLElement>(null);
  const [presenceOpen, setPresenceOpen] = useState(false);
  // A retried send must reuse its key so the server recognizes the retry.
  const pendingPost = useRef<
    { threadId: string; text: string; key: string } | undefined
  >(undefined);
  // The last action attempted, so a failed one can be retried as it was.
  const lastAttempt = useRef<(() => Promise<unknown>) | undefined>(undefined);
  const attempt = (action: () => Promise<unknown>) => {
    lastAttempt.current = action;
    void action();
  };

  const members = useMembers().data?.members;
  const resolveOwner = useCallback(
    (subjectId: string) => {
      const member = members?.find((candidate) => candidate.id === subjectId);
      return member?.name.trim() || member?.email;
    },
    [members],
  );

  const { data, store } = workspace;
  const conversations = useMemo(
    () =>
      data
        ? conversationsFrom(data, {
            viewerId: userId,
            resolve: resolveOwner,
            sessionInstanceId: workspace.sessionInstanceId,
            readMarks: marks,
            store,
          })
        : [],
    [data, store, userId, resolveOwner, workspace.sessionInstanceId, marks],
  );
  const selected = conversations.find(
    (conversation) => conversation.id === selectedId,
  );
  const history = historyOf(store, selectedId);
  const presence = useMemo(
    () =>
      data
        ? presenceOf(data, { viewerId: userId, resolve: resolveOwner })
        : { agents: [], readers: [] },
    [data, userId, resolveOwner],
  );
  const progress = useMemo(
    () =>
      data ? setupProgressOf(data) : { agentsSeen: 0, agentsTalking: false },
    [data],
  );
  // The snapshot only shows live and recent agents, so a project whose agents
  // have gone quiet would look unset again. Finishing setup puts it away for
  // good, the same as hiding it.
  const setupDone = progress.agentsSeen >= 2 && progress.agentsTalking;
  useEffect(() => {
    if (setupDone) hideSetup();
  }, [setupDone, hideSetup]);
  const homeUnread = conversations.reduce(
    (total, conversation) =>
      conversation.muted ? total : total + conversation.unread,
    0,
  );

  const selectThread = useCallback(
    (id: string) =>
      setSearchParams(
        (params) => {
          if (id) params.set("thread", id);
          else params.delete("thread");
          return params;
        },
        { replace: true },
      ),
    [setSearchParams],
  );

  // The snapshot carries only a bounded tail, so an opened conversation reads
  // its own newest page. Doing it once per conversation keeps a long list from
  // turning into one request per row.
  const { openConversation } = workspace;
  useEffect(() => {
    if (selectedId && data?.enabled) openConversation(selectedId);
  }, [selectedId, data?.enabled, openConversation]);

  // Opening a conversation is what marks it read, so the unread marks in the
  // list always mean "arrived since you last looked".
  const lastSeen = selected?.lastMessage?.id;
  useEffect(() => {
    if (selectedId && lastSeen) markRead(selectedId, lastSeen);
  }, [selectedId, lastSeen, markRead]);

  const draft = drafts[selectedId] ?? "";
  const setDraft = useCallback(
    (value: string) =>
      setDrafts((current) => ({ ...current, [selectedId]: value })),
    [selectedId],
  );

  const send = async () => {
    const text = draft.trim();
    if (!selected || !text || workspace.isActing || !workspace.canParticipate)
      return;
    if (
      pendingPost.current?.threadId !== selectedId ||
      pendingPost.current.text !== text
    ) {
      pendingPost.current = {
        threadId: selectedId,
        text,
        key: crypto.randomUUID(),
      };
    }
    const sent = await workspace.run({
      operation: "channels_post",
      withSession: true,
      args: {
        threadId: selectedId,
        text,
        idempotencyKey: pendingPost.current.key,
      },
    });
    // A failed send keeps the draft so nothing typed is ever lost.
    if (!sent) return;
    pendingPost.current = undefined;
    // A retry can land after the draft was edited; only the text that was
    // sent is cleared.
    setDrafts((current) =>
      current[selectedId]?.trim() === text
        ? { ...current, [selectedId]: "" }
        : current,
    );
  };

  const createThread = async (title: string): Promise<boolean> => {
    const created = await workspace.run({
      operation: "channels_create_thread",
      withSession: true,
      args: { title },
    });
    if (!created) return false;
    selectThread((created as ChannelThread).id);
    return true;
  };

  const startDirect = async (instanceId: string) => {
    const existing =
      data && workspace.sessionInstanceId
        ? directThreadBetween(data, workspace.sessionInstanceId, instanceId)
        : undefined;
    if (existing) {
      selectThread(existing.id);
      return;
    }
    const created = await workspace.run({
      operation: "channels_dm",
      withSession: true,
      args: { recipientInstanceId: instanceId },
    });
    if (created) selectThread((created as ChannelThread).id);
  };

  // Connecting lives on Home, so every "Connect an agent" leads there.
  const [connectOpen, setConnectOpen] = useState(false);
  const showConnect = () => {
    // A narrow window shows the list in place of Home, so the steps open in a
    // panel there instead.
    if (!window.matchMedia("(min-width: 1024px)").matches) {
      setConnectOpen(true);
      return;
    }
    selectThread("");
    requestAnimationFrame(() =>
      connectRef.current?.scrollIntoView({ block: "start" }),
    );
  };

  const markAllRead = () => {
    for (const conversation of conversations) {
      if (conversation.unread > 0 && conversation.lastMessage)
        markRead(conversation.id, conversation.lastMessage.id);
    }
  };

  const status =
    workspace.error instanceof ChannelsRequestError
      ? workspace.error.status
      : undefined;
  const throttled = status === 429;
  // A rejected request is not a failed one: the project is reachable and
  // retrying the same thing will be rejected again.
  const rejected = status === 400;
  // Being signed out and being denied are both access failures, but only one
  // of them is something an administrator can do anything about.
  const signedOut = status === 401;
  const notice = workspace.error ? (
    <Alert variant="error">
      <AlertTitle>
        {signedOut
          ? "You're signed out"
          : workspace.accessDenied
            ? "You can no longer read Channels"
            : throttled
              ? "Too many messages at once"
              : rejected
                ? "Channels wouldn't accept that"
                : "Couldn't reach Channels"}
      </AlertTitle>
      <AlertDescription>
        {signedOut
          ? "Sign in again to read Channels."
          : workspace.accessDenied
            ? "Ask a project admin to restore your access."
            : throttled
              ? "Your project's send limit was reached. Send again in a moment; the message is kept and will not be duplicated."
              : rejected
                ? "Check what you entered and try again. Nothing you typed was lost."
                : "Retrying every few seconds. Anything you have typed is kept."}{" "}
        <span className="text-muted-foreground block break-words">
          {workspace.error.message}
        </span>
        {workspace.accessDenied || rejected ? null : (
          <Button
            size="xs"
            variant="secondary"
            icon="refresh-cw"
            className="mt-2"
            onClick={() => {
              if (workspace.actionFailed && lastAttempt.current)
                attempt(lastAttempt.current);
              else workspace.refresh();
            }}
          >
            <Button.Text>Try again now</Button.Text>
          </Button>
        )}
      </AlertDescription>
    </Alert>
  ) : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex items-center justify-between gap-4 border-b px-6 py-3">
        <div>
          <PageEyebrow />
          <div className="flex items-center gap-2">
            <h1 className="text-display-sm font-thin">Channels</h1>
            <ReleaseStageBadge stage="beta" />
          </div>
          <p className="text-muted-foreground text-sm">
            Where your agents talk to each other. Read along, and step in when
            they need you.
          </p>
        </div>
        <div className="flex items-center gap-1">
          {data?.enabled ? (
            <Button
              size="sm"
              variant="tertiary"
              icon="users"
              className="xl:hidden"
              tooltip="Agents connected to Channels right now"
              onClick={() => setPresenceOpen(true)}
            >
              <Button.Text>{`${presence.agents.length} ${presence.agents.length === 1 ? "agent" : "agents"} online`}</Button.Text>
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="tertiary"
            icon="settings"
            aria-label="Channels settings"
            tooltip="Channels settings"
            disabled={!data}
            onClick={() => setSettingsOpen(true)}
          />
        </div>
      </header>

      {workspace.isLoading ? <LoadingBoard /> : null}

      {!workspace.isLoading && !data ? (
        <div className="flex flex-1 items-center justify-center p-8">
          <div className="w-full max-w-xl space-y-4">
            {notice}
            <InlineEmptyState
              icon="lock"
              heading="Channels isn't available to you"
              description="Channels needs project access that your role doesn't include yet."
            />
          </div>
        </div>
      ) : null}

      {data && !data.enabled ? (
        <div className="flex flex-1 items-center justify-center p-8">
          <div className="w-full max-w-xl space-y-4">
            {notice}
            <InlineEmptyState
              icon="messages-square"
              heading="Bring people and agents into one conversation"
              description="Turn on Channels to start collaborating in this project. It is currently off."
              action={
                workspace.canAdmin ? (
                  <Button
                    disabled={workspace.isActing}
                    onClick={() =>
                      attempt(() =>
                        workspace.run({
                          operation: "channels_admin",
                          args: { enabled: true },
                        }),
                      )
                    }
                  >
                    <Button.Text>Turn on Channels</Button.Text>
                  </Button>
                ) : (
                  <p className="text-muted-foreground text-sm">
                    Ask a project admin to turn it on.
                  </p>
                )
              }
            />
          </div>
        </div>
      ) : null}

      {data && data.enabled && !data.viewer.canConnect ? (
        <div className="flex flex-1 items-center justify-center p-8">
          <div className="w-full max-w-xl space-y-4">
            {notice}
            <InlineEmptyState
              icon="lock"
              heading="You can't take part in Channels"
              description="Your role or an exception in Channels settings stops you from reading and posting here. Give yourself access in Channels settings to join in."
              action={
                <Button
                  variant="secondary"
                  onClick={() => setSettingsOpen(true)}
                >
                  <Button.Text>Open Channels settings</Button.Text>
                </Button>
              }
            />
          </div>
        </div>
      ) : null}

      {data && data.enabled && data.viewer.canConnect ? (
        <div className="grid min-h-0 flex-1 lg:grid-cols-[22rem_minmax(0,1fr)] xl:grid-cols-[22rem_minmax(0,1fr)_16rem]">
          <ConversationList
            className={cn(
              "min-w-0 lg:flex lg:border-r",
              // Only a conversation that resolved replaces the list, so a
              // link to one that is not listed never strands a narrow window.
              selected && "hidden",
            )}
            conversations={conversations}
            selectedId={selectedId}
            onSelect={selectThread}
            homeSelected={!selected}
            homeUnread={homeUnread}
            onCreate={(title) => {
              const create = () => createThread(title);
              lastAttempt.current = create;
              return create();
            }}
            canParticipate={workspace.canParticipate}
            maxTitleBytes={workspace.limits.titleBytes}
            busy={workspace.isActing}
            filter={filter}
            onFilterChange={setFilter}
            more={workspace.moreConversations}
            onLoadMore={workspace.loadMoreConversations}
          />
          {!selected ? (
            <ChannelsHome
              className="hidden lg:block"
              conversations={conversations}
              snapshot={data}
              presence={presence}
              progress={progress}
              viewerId={userId}
              resolveOwner={resolveOwner}
              setupHidden={setupHidden}
              onHideSetup={hideSetup}
              onOpen={selectThread}
              onMarkAllRead={markAllRead}
              connectRef={connectRef}
              notice={notice}
              partial={workspace.moreConversations.hasMore}
            />
          ) : (
            <ConversationPane
              className="min-w-0 lg:flex"
              conversation={selected}
              history={history}
              snapshot={data}
              viewerId={userId}
              resolveOwner={resolveOwner}
              audience={
                selected
                  ? audienceOf(selected, data, userId, resolveOwner)
                  : undefined
              }
              parentTitle={
                selected?.parentId
                  ? conversations.find(
                      (conversation) => conversation.id === selected.parentId,
                    )?.title
                  : undefined
              }
              onOpenParent={
                selected?.parentId
                  ? () => selectThread(selected.parentId!)
                  : undefined
              }
              draft={draft}
              onDraftChange={setDraft}
              onSend={() => attempt(send)}
              sending={workspace.isActing}
              maxBytes={workspace.limits.messageBytes}
              canParticipate={workspace.canParticipate}
              reconnecting={workspace.reconnecting}
              lastReadAt={workspace.lastReadAt}
              onToggleFollow={() =>
                attempt(() =>
                  workspace.run({
                    operation: "channels_follow",
                    args: { threadId: selectedId, follow: !selected?.followed },
                  }),
                )
              }
              onToggleMute={() =>
                attempt(() =>
                  workspace.run({
                    operation: "channels_follow",
                    args: { threadId: selectedId, muted: !selected?.muted },
                  }),
                )
              }
              onLoadOlder={() => workspace.loadOlder(selectedId)}
              onConnectAgent={showConnect}
              onBack={() => selectThread("")}
              notice={notice}
            />
          )}
          <PresenceRail
            className="hidden min-w-0 border-l xl:flex"
            presence={presence}
            canParticipate={workspace.canParticipate}
            busy={workspace.isActing}
            onMessage={(instanceId) => attempt(() => startDirect(instanceId))}
            onConnectAgent={showConnect}
          />
        </div>
      ) : null}

      <Sheet open={connectOpen} onOpenChange={setConnectOpen}>
        <SheetContent side="right" className="w-[24rem] sm:max-w-[24rem]">
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle>Connect an agent</SheetTitle>
          </SheetHeader>
          <div className="px-6 py-4">
            <ConnectAgent />
          </div>
        </SheetContent>
      </Sheet>

      <Sheet open={presenceOpen} onOpenChange={setPresenceOpen}>
        <SheetContent side="right" className="w-[20rem] sm:max-w-[20rem]">
          <SheetHeader className="px-4 pt-6 pb-0">
            <SheetTitle>Online now</SheetTitle>
          </SheetHeader>
          <PresenceRail
            className="flex-1"
            titled={false}
            presence={presence}
            canParticipate={workspace.canParticipate}
            busy={workspace.isActing}
            onMessage={(instanceId) => {
              setPresenceOpen(false);
              attempt(() => startDirect(instanceId));
            }}
            onConnectAgent={() => {
              setPresenceOpen(false);
              showConnect();
            }}
          />
        </SheetContent>
      </Sheet>

      {data ? (
        <ChannelsSettings
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          snapshot={data}
          canAdmin={workspace.canAdmin}
          busy={workspace.isActing}
          members={(members ?? []).map((member) => ({
            id: member.id,
            name: member.name.trim() || member.email,
          }))}
          resolveOwner={resolveOwner}
          sessionLabel={
            workspace.sessionInstanceId
              ? data.instances[workspace.sessionInstanceId]?.label
              : undefined
          }
          sessionInstanceId={workspace.sessionInstanceId}
          onRun={(operation, args) =>
            attempt(() => workspace.run({ operation, args }))
          }
        />
      ) : null}
    </div>
  );
}

/** Holds the loaded layout's geometry so nothing jumps when data arrives. */
function LoadingBoard(): JSX.Element {
  return (
    <div
      role="status"
      aria-label="Loading conversations"
      className="grid min-h-0 flex-1 lg:grid-cols-[22rem_minmax(0,1fr)] xl:grid-cols-[22rem_minmax(0,1fr)_16rem]"
    >
      <div className="space-y-3 p-4 lg:border-r">
        {[0, 1, 2, 3, 4].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
      <div className="hidden flex-col gap-3 p-6 lg:flex">
        {[0, 1, 2].map((row) => (
          <Skeleton key={row} className="h-16 w-full max-w-lg" />
        ))}
      </div>
      <div className="hidden space-y-3 border-l p-4 xl:block">
        {[0, 1].map((row) => (
          <Skeleton key={row} className="h-8 w-full" />
        ))}
      </div>
    </div>
  );
}
