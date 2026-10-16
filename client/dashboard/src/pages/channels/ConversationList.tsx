import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useState } from "react";
import type { Conversation } from "./model";

export type ListFilter = "all" | "following";

const encoder = new TextEncoder();

/** Cuts text to whole characters that fit the server's byte limit. */
function fitBytes(value: string, maxBytes: number): string {
  if (encoder.encode(value).length <= maxBytes) return value;
  let used = 0;
  let fitted = "";
  for (const character of value) {
    used += encoder.encode(character).length;
    if (used > maxBytes) break;
    fitted += character;
  }
  return fitted;
}

/**
 * The conversation rail: everything the organization is talking about, newest
 * first, so the page opens on activity rather than on a setup form.
 */
export function ConversationList({
  conversations,
  selectedId,
  onSelect,
  onCreate,
  canParticipate,
  maxTitleBytes,
  busy,
  filter,
  onFilterChange,
  more,
  onLoadMore,
  className,
}: {
  conversations: Conversation[];
  selectedId: string;
  onSelect: (id: string) => void;
  /** Resolves true once the thread exists. */
  onCreate: (title: string) => Promise<boolean>;
  canParticipate: boolean;
  /** The server's thread title limit, so the field never invents its own. */
  maxTitleBytes: number;
  busy: boolean;
  filter: ListFilter;
  onFilterChange: (filter: ListFilter) => void;
  /** Whether the organization has conversations beyond the ones listed. */
  more: { hasMore: boolean; loading: boolean; error?: string };
  onLoadMore: () => void;
  className?: string;
}): JSX.Element {
  const [title, setTitle] = useState("");
  const [composing, setComposing] = useState(false);
  // "Following" is what the reader asked to hear about: threads they follow and
  // conversations addressed to them. The open conversation always stays listed
  // so filtering never orphans the selection.
  const shown =
    filter === "following"
      ? conversations.filter(
          (conversation) =>
            conversation.followed ||
            conversation.involvesViewer ||
            conversation.id === selectedId,
        )
      : conversations;
  const threads = shown.filter((conversation) => !conversation.private);
  const directs = shown.filter((conversation) => conversation.private);

  // The title stays until the thread exists, so a failed create can be
  // retried without typing it again.
  const create = async () => {
    const next = title.trim();
    if (!next || busy) return;
    if (!(await onCreate(next))) return;
    setTitle("");
    setComposing(false);
  };

  return (
    <div className={cn("flex min-h-0 min-w-0 flex-col", className)}>
      <div className="flex items-center justify-between gap-2 px-4 pt-4 pb-3">
        <SegmentedControl
          className="h-8"
          ariaLabel="Which conversations to list"
          value={filter}
          onChange={onFilterChange}
          options={[
            { value: "all", label: "All" },
            {
              value: "following",
              label: "Following",
              tooltip: "Threads you follow and conversations addressed to you",
            },
          ]}
        />
        <Button
          size="sm"
          variant="tertiary"
          icon="plus"
          disabled={!canParticipate}
          tooltip={
            canParticipate
              ? "Start a thread everyone in your organization can read"
              : "Channels is off for this organization"
          }
          onClick={() => setComposing(true)}
        >
          <Button.Text>New thread</Button.Text>
        </Button>
      </div>

      {composing ? (
        <div className="px-4 pb-3">
          <Input
            autoFocus
            aria-label="New thread title"
            placeholder="What is this about?"
            value={title}
            onChange={(value) => setTitle(fitBytes(value, maxTitleBytes))}
            onEnter={() => void create()}
            onBlur={() => {
              if (!title.trim()) setComposing(false);
            }}
          />
          <div className="mt-2 flex gap-2">
            <Button
              size="sm"
              disabled={!title.trim() || busy}
              onClick={() => void create()}
            >
              <Button.Text>Create thread</Button.Text>
            </Button>
            <Button
              size="sm"
              variant="tertiary"
              onClick={() => {
                setTitle("");
                setComposing(false);
              }}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </div>
        </div>
      ) : null}

      <nav
        aria-label="Conversations"
        className="min-h-0 min-w-0 flex-1 overflow-y-auto pb-4"
      >
        <Group
          title="Threads"
          conversations={threads}
          selectedId={selectedId}
          onSelect={onSelect}
          empty={
            filter === "following"
              ? "No threads followed yet."
              : "No threads yet."
          }
        />
        <Group
          title="Direct messages"
          conversations={directs}
          selectedId={selectedId}
          onSelect={onSelect}
          empty="Agents you message directly show up here."
        />
        {/* An organization can hold more conversations than one read returns.
            Saying so, and offering the rest, is the difference between a long
            list and a list that quietly ends. */}
        {more.hasMore || more.error ? (
          <div className="flex flex-col items-start gap-1 px-4 pt-2">
            {more.error ? (
              <p className="text-muted-foreground text-xs">
                More conversations could not be loaded. {more.error}
              </p>
            ) : null}
            <Button
              size="xs"
              variant="tertiary"
              icon="chevron-down"
              disabled={more.loading}
              onClick={onLoadMore}
            >
              <Button.Text>
                {more.loading
                  ? "Loading conversations…"
                  : more.error
                    ? "Try again"
                    : "Load more conversations"}
              </Button.Text>
            </Button>
          </div>
        ) : null}
      </nav>
    </div>
  );
}

function Group({
  title,
  conversations,
  selectedId,
  onSelect,
  empty,
}: {
  title: string;
  conversations: Conversation[];
  selectedId: string;
  onSelect: (id: string) => void;
  empty: string;
}): JSX.Element {
  return (
    <section className="mb-2">
      <h2 className="text-eyebrow px-4 py-2">{title}</h2>
      {conversations.length === 0 ? (
        <p className="text-muted-foreground px-4 pb-2 text-sm">{empty}</p>
      ) : (
        <ul>
          {conversations.map((conversation) => (
            <li key={conversation.id}>
              <Row
                conversation={conversation}
                selected={conversation.id === selectedId}
                onSelect={onSelect}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Row({
  conversation,
  selected,
  onSelect,
}: {
  conversation: Conversation;
  selected: boolean;
  onSelect: (id: string) => void;
}): JSX.Element {
  const when = conversation.lastMessage
    ? formatRelativeTime(new Date(conversation.lastMessage.createdAt))
    : null;
  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      onClick={() => onSelect(conversation.id)}
      className={cn(
        "group/row hover:bg-muted/60 flex w-full items-start gap-2.5 px-4 py-2 text-left transition-colors",
        selected && "bg-muted",
      )}
    >
      <span className="mt-1 flex w-5 shrink-0 items-center gap-1">
        {conversation.unread > 0 ? (
          <>
            <span className="bg-foreground size-1.5 rounded-full" aria-hidden />
            <span className="sr-only">{conversation.unread} unread</span>
          </>
        ) : (
          <span className="size-1.5" aria-hidden />
        )}
        <Icon
          name={conversation.private ? "lock" : "hash"}
          className="text-muted-foreground size-3.5"
          aria-hidden
        />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-baseline gap-2">
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-sm",
              conversation.unread > 0 && "font-medium",
            )}
          >
            {conversation.parentId ? "↳ " : ""}
            {conversation.title}
          </span>
          {conversation.muted ? (
            <Icon
              name="bell-off"
              className="text-muted-foreground size-3 shrink-0 self-center"
              aria-label="Muted"
            />
          ) : null}
          {when ? (
            <span className="text-muted-foreground shrink-0 text-xs">
              {when}
            </span>
          ) : null}
        </span>
        {conversation.lastMessage ? (
          <span
            className={cn(
              "text-muted-foreground mt-0.5 block truncate text-xs",
              conversation.unread > 0 && "text-foreground",
            )}
          >
            {conversation.lastMessage.text}
          </span>
        ) : null}
      </span>
    </button>
  );
}
