import { InlineEmptyState } from "@/components/inline-empty-state";
import { PageEyebrow } from "@/components/page-eyebrow";
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
import { useOrganization, useUser } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { useMembers } from "@gram/client/react-query/members.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { ChannelsSettings } from "./ChannelsSettings";
import { ConversationList, type ListFilter } from "./ConversationList";
import { ConversationPane } from "./Conversation";
import { PresenceRail } from "./PresenceRail";
import { ChannelsRequestError, type ChannelThread } from "./api";
import { historyOf } from "./messages";
import {
  audienceOf,
  conversationsFrom,
  directThreadBetween,
  presenceGroups,
} from "./model";
import { useChannelsWorkspace, useReadMarks } from "./useChannels";

export default function Channels(): JSX.Element {
  const organization = useOrganization();
  const user = useUser();
  return (
    <FullBleedPage>
      <ChannelsBoard
        key={`${organization.id}:${user.id}`}
        organizationId={organization.id}
        userId={user.id}
      />
    </FullBleedPage>
  );
}

function ChannelsBoard({
  organizationId,
  userId,
}: {
  organizationId: string;
  userId: string;
}): JSX.Element {
  // The page is itself a conversation surface, so the floating assistant dock
  // would sit on top of the composer.
  useHideInsightsDock();
  const workspace = useChannelsWorkspace(organizationId, userId);
  const [marks, markRead] = useReadMarks(
    `speakeasy.channels.read.v1.${organizationId}.${userId}`,
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = searchParams.get("thread") ?? "";
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState<ListFilter>("all");
  const [settingsOpen, setSettingsOpen] = useState(false);
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
  const groups = useMemo(
    () =>
      data
        ? presenceGroups(data, { viewerId: userId, resolve: resolveOwner })
        : [],
    [data, userId, resolveOwner],
  );
  const online = groups.reduce(
    (total, group) => total + group.entries.length,
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

  // A wide window shows the list and a conversation side by side, so the page
  // opens on the newest activity instead of an empty panel. A narrow window
  // shows one at a time, where that would hide the list behind a conversation
  // nobody asked for.
  const newest = conversations[0]?.id;
  useEffect(() => {
    if (selectedId || !newest) return;
    if (!window.matchMedia("(min-width: 1024px)").matches) return;
    selectThread(newest);
  }, [selectedId, newest, selectThread]);

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

  const status =
    workspace.error instanceof ChannelsRequestError
      ? workspace.error.status
      : undefined;
  const throttled = status === 429;
  // A rejected request is not a failed one: the organization is reachable and
  // retrying the same thing will be rejected again.
  const rejected = status === 400;
  // Being signed out and being denied are both access failures, but only one
  // of them is something an administrator can do anything about.
  const signedOut = status === 401;
  const notice = workspace.error ? (
    <Alert variant="error" alignTop>
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
            ? "Ask an organization admin to restore your access."
            : throttled
              ? "Your organization's send limit was reached. Send again in a moment; the message is kept and will not be duplicated."
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
          <h1 className="text-display-sm font-thin">Channels</h1>
        </div>
        <div className="flex items-center gap-1">
          {data?.enabled ? (
            <Button
              size="sm"
              variant="tertiary"
              icon="users"
              className="xl:hidden"
              tooltip="People and agents connected to Channels right now"
              onClick={() => setPresenceOpen(true)}
            >
              <Button.Text>{`${online} online`}</Button.Text>
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
              description="Channels needs organization access that your role doesn't include yet."
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
              heading="Channels is off for this organization"
              description="Turn it on to let people and agents in your organization talk to each other."
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
                    Ask an organization admin to turn it on.
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
              description="An exception in Channels settings stops you from reading and posting here. Remove it to join in."
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
          <ConversationPane
            className={cn("min-w-0 lg:flex", !selected && "hidden")}
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
            onConnectAgent={() => setSettingsOpen(true)}
            onBack={() => selectThread("")}
            notice={notice}
          />
          <PresenceRail
            className="hidden min-w-0 border-l xl:flex"
            groups={groups}
            sessionInstanceId={workspace.sessionInstanceId}
            canParticipate={workspace.canParticipate}
            busy={workspace.isActing}
            onMessage={(instanceId) => attempt(() => startDirect(instanceId))}
            onConnectAgent={() => setSettingsOpen(true)}
          />
        </div>
      ) : null}

      <Sheet open={presenceOpen} onOpenChange={setPresenceOpen}>
        <SheetContent side="right" className="w-[20rem] sm:max-w-[20rem]">
          <SheetHeader className="px-4 pt-6 pb-0">
            <SheetTitle>Online now</SheetTitle>
          </SheetHeader>
          <PresenceRail
            className="flex-1"
            titled={false}
            groups={groups}
            sessionInstanceId={workspace.sessionInstanceId}
            canParticipate={workspace.canParticipate}
            busy={workspace.isActing}
            onMessage={(instanceId) => {
              setPresenceOpen(false);
              attempt(() => startDirect(instanceId));
            }}
            onConnectAgent={() => {
              setPresenceOpen(false);
              setSettingsOpen(true);
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
