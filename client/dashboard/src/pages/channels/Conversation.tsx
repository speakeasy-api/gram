import { InlineEmptyState } from "@/components/inline-empty-state";
import { getIdentityTint, useIsDarkTheme } from "@/components/gradient-colors";
import { Avatar, AvatarFallback } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { formatRelativeTime } from "@/lib/dates";
import { cn, initialsOf } from "@/lib/utils";
import {
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type UIEvent,
} from "react";
import type { ChannelMessage, ChannelsSnapshot } from "./api";
import type { ThreadHistory } from "./messages";
import { speakerFor, type Conversation, type ResolveOwner } from "./model";

/** Consecutive messages from one speaker read as one turn. */
const GROUPING_WINDOW_MS = 5 * 60_000;
/** Keep following live activity only when the reader is already at the end. */
const FOLLOW_THRESHOLD_PX = 120;
/** Reaching the top of what is loaded asks for the page above it. */
const NEAR_TOP_PX = 120;

const encoder = new TextEncoder();
const timeOfDay = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
});
const dayFormat = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
});

function dayLabel(iso: string): string {
  const date = new Date(iso);
  const today = new Date();
  const yesterday = new Date(today.getTime() - 86_400_000);
  if (date.toDateString() === today.toDateString()) return "Today";
  if (date.toDateString() === yesterday.toDateString()) return "Yesterday";
  return dayFormat.format(date);
}

/**
 * One conversation: who can read it, what was said, and a composer that turns
 * a person into a participant without asking them to set anything up first.
 *
 * Only part of a conversation is held at a time. Reading upwards asks for the
 * page above, which must arrive without moving what the reader is looking at;
 * new messages at the bottom follow the reader only while they are already
 * there, and announce themselves when they are not.
 */
export function ConversationPane({
  conversation,
  history,
  snapshot,
  viewerId,
  resolveOwner,
  audience,
  parentTitle,
  onOpenParent,
  draft,
  onDraftChange,
  onSend,
  sending,
  maxBytes,
  canParticipate,
  reconnecting,
  lastReadAt,
  onToggleFollow,
  onToggleMute,
  onLoadOlder,
  onConnectAgent,
  onBack,
  notice,
  className,
}: {
  conversation?: Conversation;
  /** How far back this conversation is loaded, and whether more exists. */
  history: ThreadHistory;
  snapshot: ChannelsSnapshot;
  viewerId: string;
  resolveOwner: ResolveOwner;
  audience?: string;
  parentTitle?: string;
  onOpenParent?: () => void;
  draft: string;
  onDraftChange: (value: string) => void;
  onSend: () => void;
  sending: boolean;
  /** The server's message size limit, in bytes. */
  maxBytes: number;
  canParticipate: boolean;
  reconnecting: boolean;
  lastReadAt?: number;
  onToggleFollow: () => void;
  onToggleMute: () => void;
  onLoadOlder: () => void;
  onConnectAgent: () => void;
  onBack: () => void;
  notice?: ReactNode;
  className?: string;
}): JSX.Element {
  const messages = conversation?.messages ?? [];
  const firstId = messages[0]?.id;
  const lastId = messages[messages.length - 1]?.id;

  const scroller = useRef<HTMLOListElement>(null);
  const following = useRef(true);
  /**
   * How far from the end of the conversation the reader was when they asked
   * for older messages. Taken at that moment and not updated afterwards: the
   * list's own height and scroll position both move between the request and
   * the page arriving, so a later reading of them is not the reader's intent.
   */
  const askedFromEnd = useRef<number | undefined>(undefined);
  const openedConversation = useRef<string | undefined>(undefined);
  const oldestHeld = useRef<number | undefined>(undefined);
  const newestHeld = useRef(0);
  /** The newest message at the moment the reader scrolled away from the end. */
  const [leftAt, setLeftAt] = useState<number>();

  // Measure intent on scroll, before new content changes the scroll height.
  // Measuring after insertion mistakes a long new message for scrolling up.
  useLayoutEffect(() => {
    const opened = openedConversation.current !== conversation?.id;
    openedConversation.current = conversation?.id;
    const grewUpwards =
      !opened &&
      oldestHeld.current !== undefined &&
      firstId !== undefined &&
      firstId < oldestHeld.current;
    oldestHeld.current = firstId;
    newestHeld.current = lastId ?? 0;

    const element = scroller.current;
    if (!element) return;
    if (opened) {
      // A conversation opens at its latest message: the reader has expressed
      // no intent in it yet.
      following.current = true;
      askedFromEnd.current = undefined;
      setLeftAt(undefined);
      element.scrollTop = element.scrollHeight;
      return;
    }
    if (grewUpwards) {
      const asked = askedFromEnd.current;
      askedFromEnd.current = undefined;
      // A conversation's first page arrives as a prepend too, and a reader who
      // has not scrolled away yet belongs at the newest message.
      element.scrollTop =
        asked === undefined
          ? following.current
            ? element.scrollHeight
            : element.scrollTop
          : element.scrollHeight - asked;
      return;
    }
    if (following.current && element.scrollHeight > element.clientHeight) {
      element.scrollTop = element.scrollHeight;
    }
  }, [firstId, lastId, conversation?.id]);

  const loadOlder = () => {
    const element = scroller.current;
    if (element)
      askedFromEnd.current = element.scrollHeight - element.scrollTop;
    onLoadOlder();
  };

  const onScroll = (event: UIEvent<HTMLOListElement>) => {
    const element = event.currentTarget;
    const atEnd =
      element.scrollHeight - element.scrollTop - element.clientHeight <
      FOLLOW_THRESHOLD_PX;
    following.current = atEnd;
    setLeftAt((current) =>
      atEnd ? undefined : (current ?? newestHeld.current),
    );
    // Reading upwards past what is held asks for the page above it — but only
    // once there is a known page above. A conversation that has not been asked
    // about yet opens scrolled to the top of an empty list, and letting that
    // count as reading upwards would race its opening request.
    if (element.scrollTop < NEAR_TOP_PX && history.hasMore === true)
      loadOlder();
  };

  // Sending is an explicit return to the end of the conversation: a reader
  // who writes should not have to be told their own message is waiting below.
  const send = () => {
    following.current = true;
    askedFromEnd.current = undefined;
    setLeftAt(undefined);
    onSend();
  };

  const jumpToLatest = () => {
    const element = scroller.current;
    following.current = true;
    askedFromEnd.current = undefined;
    setLeftAt(undefined);
    if (element) element.scrollTop = element.scrollHeight;
  };

  let unseen = 0;
  if (leftAt !== undefined) {
    for (
      let index = messages.length - 1;
      index >= 0 && messages[index]!.id > leftAt;
      index--
    )
      unseen++;
  }

  return (
    <section className={cn("flex min-h-0 min-w-0 flex-col", className)}>
      {conversation ? (
        <header className="flex items-start gap-3 border-b px-6 py-4">
          <Button
            size="sm"
            variant="tertiary"
            icon="chevron-left"
            aria-label="Back to conversations"
            className="lg:hidden"
            onClick={onBack}
          />
          <div className="min-w-0 flex-1">
            <h2 className="flex items-center gap-2 text-base font-medium">
              <Icon
                name={conversation.private ? "lock" : "hash"}
                className="text-muted-foreground size-4 shrink-0"
                aria-hidden
              />
              <span className="truncate">{conversation.title}</span>
            </h2>
            <p className="text-muted-foreground mt-0.5 truncate text-xs">
              {audience}
              {parentTitle && onOpenParent ? (
                <>
                  {" · "}
                  <button
                    type="button"
                    onClick={onOpenParent}
                    className="hover:text-foreground underline underline-offset-2"
                  >
                    Part of {parentTitle}
                  </button>
                </>
              ) : null}
            </p>
            {reconnecting ? (
              <p
                role="status"
                className="text-muted-foreground mt-1 flex items-center gap-1.5 text-xs"
              >
                <Icon name="refresh-cw" className="size-3" aria-hidden />
                Reconnecting
                {lastReadAt
                  ? ` · last updated ${formatRelativeTime(new Date(lastReadAt))}`
                  : null}
              </p>
            ) : null}
          </div>
          <div className="flex shrink-0 gap-1">
            <Button
              size="sm"
              variant={conversation.followed ? "secondary" : "tertiary"}
              icon={conversation.followed ? "bell-ring" : "bell"}
              tooltip={
                conversation.followed
                  ? "Your agents are told about new messages here, as long as they also subscribe to channels://messages. Press to stop."
                  : "Tell your agents about new messages here. They must also subscribe to channels://messages to receive them."
              }
              disabled={!canParticipate}
              onClick={onToggleFollow}
            >
              <Button.Text>
                {conversation.followed ? "Following" : "Follow"}
              </Button.Text>
            </Button>
            <Button
              size="sm"
              variant={conversation.muted ? "secondary" : "tertiary"}
              icon={conversation.muted ? "bell-off" : "volume-2"}
              tooltip={
                conversation.muted
                  ? "No notifications from here. You can still read it."
                  : "Stop notifications from this conversation."
              }
              disabled={!canParticipate}
              onClick={onToggleMute}
            >
              <Button.Text>{conversation.muted ? "Muted" : "Mute"}</Button.Text>
            </Button>
          </div>
        </header>
      ) : null}

      {notice ? <div className="px-6 pt-4">{notice}</div> : null}

      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <ol
          ref={scroller}
          onScroll={onScroll}
          aria-label="Thread messages"
          // Scroll anchoring is handled here, from the reader's intent. Left
          // to the browser as well, the two corrections compound and the
          // message being read still moves when a page is added above it.
          className="min-h-0 flex-1 overflow-y-auto px-6 py-4 [overflow-anchor:none]"
        >
          {conversation && hasOlderRow(history) ? (
            <li>
              <OlderMessages history={history} onLoad={loadOlder} />
            </li>
          ) : null}
          {!conversation ? (
            <li>
              <InlineEmptyState
                icon="messages-square"
                heading="Pick a conversation"
                description="Threads are open to everyone in your organization. Direct messages go to one agent."
              />
            </li>
          ) : null}
          {conversation && messages.length === 0 && !history.loading ? (
            <li>
              <InlineEmptyState
                icon="message-square-dashed"
                heading="Nothing said yet"
                description={
                  canParticipate
                    ? "Write the first message. An agent reads it once it is connected, follows this conversation, and subscribes to new messages."
                    : "Channels is off, so nobody can post here."
                }
                action={
                  canParticipate ? (
                    <Button
                      size="sm"
                      variant="secondary"
                      icon="plug"
                      onClick={onConnectAgent}
                    >
                      <Button.Text>How agents connect</Button.Text>
                    </Button>
                  ) : null
                }
              />
            </li>
          ) : null}
          {messages.map((message, index) => (
            <MessageRow
              key={message.id}
              message={message}
              previous={messages[index - 1]}
              snapshot={snapshot}
              viewerId={viewerId}
              resolveOwner={resolveOwner}
            />
          ))}
        </ol>

        {conversation && leftAt !== undefined ? (
          // Announced politely, so a reader who is not watching the list is
          // still told that the conversation moved on without them.
          <div
            aria-live="polite"
            className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center"
          >
            <Button
              size="sm"
              variant="secondary"
              icon="arrow-down"
              className="pointer-events-auto"
              onClick={jumpToLatest}
            >
              <Button.Text>
                {unseen > 0
                  ? `${unseen} new message${unseen === 1 ? "" : "s"}`
                  : "Jump to latest"}
              </Button.Text>
            </Button>
          </div>
        ) : null}
      </div>

      <Composer
        draft={draft}
        onDraftChange={onDraftChange}
        onSend={send}
        sending={sending}
        maxBytes={maxBytes}
        disabled={!conversation || !canParticipate}
        placeholder={
          !conversation
            ? "Pick a conversation to write in"
            : !canParticipate
              ? "Channels is off for this organization"
              : conversation.private
                ? `Message ${conversation.title}`
                : `Write in ${conversation.title}`
        }
      />
    </section>
  );
}

/** Whether the top of the list has anything to report about older messages. */
function hasOlderRow(history: ThreadHistory): boolean {
  return (
    history.loading ||
    history.error !== undefined ||
    history.removedByRetention ||
    history.hasMore !== false
  );
}

/**
 * The top of what is loaded. A conversation can be longer than the pages the
 * viewer holds, and older messages can also be gone for good, which is a
 * different answer from "not loaded yet".
 */
function OlderMessages({
  history,
  onLoad,
}: {
  history: ThreadHistory;
  onLoad: () => void;
}): JSX.Element | null {
  if (history.loading) {
    return (
      <p
        role="status"
        className="text-muted-foreground py-2 text-center text-xs"
      >
        Loading older messages…
      </p>
    );
  }
  if (history.error) {
    return (
      <div className="flex flex-col items-center gap-1 py-2">
        <p className="text-muted-foreground text-center text-xs">
          Older messages could not be loaded. {history.error}
        </p>
        <Button size="xs" variant="tertiary" onClick={onLoad}>
          <Button.Text>Try again</Button.Text>
        </Button>
      </div>
    );
  }
  if (history.removedByRetention) {
    return (
      <p className="text-muted-foreground py-2 text-center text-xs">
        Older messages are past this organization's retention window and are no
        longer kept.
      </p>
    );
  }
  if (history.hasMore === false) return null;
  return (
    <div className="flex justify-center py-2">
      <Button size="xs" variant="tertiary" icon="history" onClick={onLoad}>
        <Button.Text>Load older messages</Button.Text>
      </Button>
    </div>
  );
}

function MessageRow({
  message,
  previous,
  snapshot,
  viewerId,
  resolveOwner,
}: {
  message: ChannelMessage;
  previous?: ChannelMessage;
  snapshot: ChannelsSnapshot;
  viewerId: string;
  resolveOwner: ResolveOwner;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  const speaker = speakerFor(message, snapshot, viewerId, resolveOwner);
  const newDay =
    !previous || dayLabel(previous.createdAt) !== dayLabel(message.createdAt);
  const continues =
    !newDay &&
    previous?.instanceId === message.instanceId &&
    Date.parse(message.createdAt) - Date.parse(previous.createdAt) <
      GROUPING_WINDOW_MS;

  return (
    <li>
      {newDay ? (
        <div className="flex items-center gap-3 py-4">
          <span className="text-eyebrow">{dayLabel(message.createdAt)}</span>
          <span className="bg-border h-px flex-1" />
        </div>
      ) : null}
      <article className={cn("flex gap-3", continues ? "mt-0.5" : "mt-3")}>
        <div className="w-8 shrink-0">
          {continues ? null : speaker.isAgent ? (
            <span className="text-muted-foreground flex size-8 items-center justify-center border">
              <Icon name="bot" className="size-4" aria-hidden />
            </span>
          ) : (
            <Avatar className="size-8">
              <AvatarFallback
                className="text-xs font-medium"
                style={getIdentityTint(speaker.account || speaker.name, isDark)}
              >
                {initialsOf(speaker.account || speaker.name)}
              </AvatarFallback>
            </Avatar>
          )}
        </div>
        <div className="min-w-0 flex-1">
          {continues ? null : (
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-sm font-medium">{speaker.name}</span>
              <span className="text-muted-foreground text-xs">
                {speaker.isYou
                  ? speaker.isAgent
                    ? "your agent"
                    : "you"
                  : speaker.isAgent
                    ? `agent of ${speaker.owner}`
                    : speaker.owner}
              </span>
              <time
                dateTime={message.createdAt}
                className="text-muted-foreground text-xs"
              >
                {timeOfDay.format(new Date(message.createdAt))}
              </time>
            </div>
          )}
          <p className="text-sm break-words whitespace-pre-wrap">
            {message.text}
          </p>
        </div>
      </article>
    </li>
  );
}

function Composer({
  draft,
  onDraftChange,
  onSend,
  sending,
  maxBytes,
  disabled,
  placeholder,
}: {
  draft: string;
  onDraftChange: (value: string) => void;
  onSend: () => void;
  sending: boolean;
  maxBytes: number;
  disabled: boolean;
  placeholder: string;
}): JSX.Element {
  // Enter sends; Shift+Enter writes a new line. An IME composition keystroke
  // must never send a half-typed word.
  const composing = useRef(false);
  // The server's limit is in bytes, so a message of accented or emoji text
  // reaches it sooner than its character count suggests.
  const remaining = maxBytes - encoder.encode(draft).length;
  const tooLong = remaining < 0;

  return (
    <form
      className="border-t px-6 py-4"
      onSubmit={(event) => {
        event.preventDefault();
        onSend();
      }}
    >
      <div className="flex w-full items-end gap-2">
        {/* Input renders its own positioning wrapper, so the row stretches this
            one rather than the field's border box. */}
        <div className="min-w-0 flex-1">
          <Input
            multiline
            lines={2}
            className="min-h-0"
            aria-label="Message"
            placeholder={placeholder}
            value={draft}
            onChange={onDraftChange}
            maxLength={maxBytes}
            disabled={disabled}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
            }}
            onKeyDown={(event) => {
              if (
                event.key === "Enter" &&
                !event.shiftKey &&
                !composing.current &&
                !event.nativeEvent.isComposing
              ) {
                event.preventDefault();
                onSend();
              }
            }}
          />
        </div>
        <Button
          type="submit"
          icon="arrow-up"
          aria-label="Send message"
          disabled={disabled || !draft.trim() || sending || tooLong}
          tooltip="Send (Enter)"
        />
      </div>
      <p
        className={cn(
          "mt-2 text-xs",
          tooLong ? "text-destructive" : "text-muted-foreground",
        )}
      >
        {tooLong
          ? `Too long to send. Remove about ${-remaining} characters.`
          : remaining < 500
            ? `${remaining} characters left.`
            : "Plain text only. Enter sends, Shift+Enter adds a line."}
      </p>
    </form>
  );
}
