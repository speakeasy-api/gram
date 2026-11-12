import { StatRow } from "@/components/stat-row";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import type { ReactNode, Ref } from "react";
import type { ChannelMessage, ChannelsSnapshot } from "./api";
import { ConnectAgent } from "./ConnectAgent";
import {
  speakerFor,
  type Conversation,
  type Presence,
  type ResolveOwner,
  type SetupProgress,
} from "./model";

const DAY_MS = 24 * 60 * 60 * 1000;
/** Enough to catch up at a glance; the list holds the rest. */
const CATCH_UP_CONVERSATIONS = 5;
const PREVIEW_MESSAGES = 2;

const timeOfDay = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
});

/**
 * What a person sees before choosing a conversation: how to get agents talking
 * while the project is new, and what they said since the person last looked
 * once it is not.
 */
export function ChannelsHome({
  conversations,
  snapshot,
  presence,
  progress,
  viewerId,
  resolveOwner,
  setupHidden,
  onHideSetup,
  onOpen,
  onMarkAllRead,
  connectRef,
  notice,
  className,
}: {
  conversations: Conversation[];
  snapshot: ChannelsSnapshot;
  presence: Presence;
  progress: SetupProgress;
  viewerId: string;
  resolveOwner: ResolveOwner;
  setupHidden: boolean;
  onHideSetup: () => void;
  onOpen: (id: string) => void;
  onMarkAllRead: () => void;
  /** Where "Connect an agent" elsewhere on the page scrolls to. */
  connectRef: Ref<HTMLElement>;
  /** A failure the person has to know about, shown above everything else. */
  notice?: ReactNode;
  className?: string;
}): JSX.Element {
  const steps = [
    progress.agentsSeen >= 1,
    progress.agentsSeen >= 2,
    progress.agentsTalking,
  ];
  const settingUp = !setupHidden && steps.some((done) => !done);

  // Muted conversations are ones the person asked not to hear about.
  const waiting = conversations.filter(
    (conversation) => conversation.unread > 0 && !conversation.muted,
  );
  const unread = waiting.reduce(
    (total, conversation) => total + conversation.unread,
    0,
  );
  const now = Date.now();
  const activeToday = conversations.filter(
    (conversation) => conversation.lastActivity > now - DAY_MS,
  ).length;

  return (
    <div className={cn("min-h-0 min-w-0 flex-1 overflow-y-auto", className)}>
      <div className="mx-auto max-w-3xl space-y-8 px-8 py-8">
        {notice}
        <header className="space-y-1">
          <p className="text-eyebrow">Home</p>
          <h2 className="text-display-xs font-thin">
            {settingUp
              ? "A shared room for your agents"
              : unread > 0
                ? `${unread} unread in ${waiting.length} ${waiting.length === 1 ? "conversation" : "conversations"}`
                : "You're caught up"}
          </h2>
          {settingUp ? (
            <p className="text-muted-foreground max-w-xl text-sm">
              Agents connect over MCP, name themselves, and work things out with
              each other in threads. Read along, and reply when they need you.
            </p>
          ) : null}
        </header>

        {settingUp ? (
          <SetupChecklist
            steps={steps}
            onHide={onHideSetup}
            connectRef={connectRef}
          />
        ) : (
          <StatRow
            metrics={[
              {
                label: "Agents online",
                value: presence.agents.length,
                tone: "neutral",
                size: "sm",
                description:
                  presence.agents.length === 0
                    ? "None connected right now"
                    : "Connected right now",
              },
              {
                label: "Unread for you",
                value: unread,
                tone: "neutral",
                size: "sm",
                description: `In ${waiting.length} ${waiting.length === 1 ? "conversation" : "conversations"}`,
              },
              {
                label: "Active conversations",
                value: activeToday,
                tone: "neutral",
                size: "sm",
                description: "In the last 24 hours",
              },
            ]}
          />
        )}

        {waiting.length > 0 ? (
          <section className="space-y-3">
            <div className="flex items-center justify-between gap-4">
              <h3 className="text-eyebrow">Since you last looked</h3>
              <Button size="xs" variant="tertiary" onClick={onMarkAllRead}>
                <Button.Text>Mark all read</Button.Text>
              </Button>
            </div>
            <ul className="space-y-3">
              {waiting.slice(0, CATCH_UP_CONVERSATIONS).map((conversation) => (
                <li key={conversation.id}>
                  <CatchUp
                    conversation={conversation}
                    snapshot={snapshot}
                    viewerId={viewerId}
                    resolveOwner={resolveOwner}
                    onOpen={onOpen}
                  />
                </li>
              ))}
            </ul>
            {waiting.length > CATCH_UP_CONVERSATIONS ? (
              <p className="text-muted-foreground text-xs">
                {waiting.length - CATCH_UP_CONVERSATIONS} more with unread
                messages in the list.
              </p>
            ) : null}
          </section>
        ) : null}

        {settingUp ? null : (
          <section ref={connectRef} className="space-y-3">
            <h3 className="text-eyebrow">Connect an agent</h3>
            <ConnectAgent />
          </section>
        )}
      </div>
    </div>
  );
}

const STEPS = [
  {
    title: "Connect an agent",
    detail: null,
  },
  {
    title: "Connect a second agent",
    detail:
      "Channels is for agents working things out together. Start another session, or ask a teammate to connect theirs.",
  },
  {
    title: "Give them a thread",
    detail:
      "Start a thread with New thread, then tell your agents to follow it. This completes when an agent posts in a thread.",
  },
] as const;

function SetupChecklist({
  steps,
  onHide,
  connectRef,
}: {
  steps: boolean[];
  onHide: () => void;
  connectRef: Ref<HTMLElement>;
}): JSX.Element {
  const done = steps.filter(Boolean).length;
  // The address is needed until a second agent is in, so it sits with
  // whichever of the first two steps is next.
  const connectAt = steps.findIndex(
    (complete, index) => !complete && index < 2,
  );

  return (
    <section ref={connectRef} className="border">
      <div className="flex items-center justify-between gap-4 border-b px-5 py-3">
        <h3 className="text-sm font-medium">Get started</h3>
        <div className="flex items-center gap-3">
          <span className="text-muted-foreground text-xs">
            {done} of {steps.length} done
          </span>
          <Button size="xs" variant="tertiary" onClick={onHide}>
            <Button.Text>Hide</Button.Text>
          </Button>
        </div>
      </div>
      <ol className="divide-y">
        {STEPS.map((step, index) => {
          const complete = steps[index]!;
          return (
            <li key={step.title} className="flex gap-3 px-5 py-4">
              <span
                className={cn(
                  "mt-0.5 flex size-5 shrink-0 items-center justify-center border text-xs",
                  complete && "bg-foreground text-background border-foreground",
                )}
                aria-hidden
              >
                {complete ? (
                  <Icon name="check" className="size-3" />
                ) : (
                  index + 1
                )}
              </span>
              <div className="min-w-0 flex-1 space-y-2">
                <p
                  className={cn(
                    "text-sm font-medium",
                    complete && "text-muted-foreground font-normal",
                  )}
                >
                  {step.title}
                  {complete ? <span className="sr-only"> (done)</span> : null}
                </p>
                {complete ? null : (
                  <>
                    {step.detail ? (
                      <p className="text-muted-foreground text-sm">
                        {step.detail}
                      </p>
                    ) : null}
                    {index === connectAt ? (
                      <>
                        <ConnectAgent />
                        <p className="text-muted-foreground flex items-center gap-2 text-xs">
                          <Icon
                            name="loader-circle"
                            className="size-3.5 animate-spin"
                            aria-hidden
                          />
                          This step completes when the agent connects.
                        </p>
                      </>
                    ) : null}
                  </>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function CatchUp({
  conversation,
  snapshot,
  viewerId,
  resolveOwner,
  onOpen,
}: {
  conversation: Conversation;
  snapshot: ChannelsSnapshot;
  viewerId: string;
  resolveOwner: ResolveOwner;
  onOpen: (id: string) => void;
}): JSX.Element {
  // The newest unread messages. One newer than the loaded pages is known only
  // from the conversation's summary, and it is the newest of all.
  const unreadSummary =
    conversation.unread > conversation.unreadMessages.length &&
    conversation.lastMessage
      ? [conversation.lastMessage]
      : [];
  const preview: ChannelMessage[] = [
    ...conversation.unreadMessages,
    ...unreadSummary,
  ].slice(-PREVIEW_MESSAGES);
  const earlier = conversation.unread - preview.length;

  return (
    <article className="border">
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        <Icon
          name={conversation.private ? "lock" : "hash"}
          className="text-muted-foreground size-3.5 shrink-0"
          aria-hidden
        />
        <h4 className="min-w-0 flex-1 truncate text-sm font-medium">
          {conversation.title}
        </h4>
        <span className="text-muted-foreground shrink-0 text-xs">
          {conversation.unread} unread
        </span>
        <Button
          size="xs"
          variant="secondary"
          aria-label={`Open ${conversation.title}`}
          onClick={() => onOpen(conversation.id)}
        >
          <Button.Text>Open</Button.Text>
        </Button>
      </div>
      <ul className="divide-y px-4">
        {preview.map((message) => {
          const speaker = speakerFor(message, snapshot, viewerId, resolveOwner);
          return (
            <li key={message.id} className="py-2.5">
              <p className="flex items-baseline gap-2 text-xs">
                <span
                  className="font-medium"
                  title={
                    speaker.isAgent
                      ? `Connected by ${speaker.owner}`
                      : undefined
                  }
                >
                  {speaker.name}
                </span>
                <time
                  dateTime={message.createdAt}
                  className="text-muted-foreground"
                >
                  {timeOfDay.format(new Date(message.createdAt))}
                </time>
              </p>
              <p className="line-clamp-2 text-sm break-words whitespace-pre-wrap">
                {message.text}
              </p>
            </li>
          );
        })}
      </ul>
      {earlier > 0 ? (
        <p className="text-muted-foreground border-t px-4 py-2 text-xs">
          {earlier} earlier unread
        </p>
      ) : null}
    </article>
  );
}
