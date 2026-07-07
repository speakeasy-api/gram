import type { FC } from "react";
import {
  ThreadListItemPrimitive,
  ThreadListPrimitive,
  useAssistantState,
} from "@assistant-ui/react";
import { MessageSquareTextIcon, PlusIcon } from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useRadius } from "@/hooks/useRadius";
import { cn, initialsOf } from "@/lib/utils";
import { useDensity } from "@/hooks/useDensity";
import { useThreadMeta } from "@/contexts/ThreadMetaContext";

/**
 * Formats a chat's creation date for the list row: "Jun 14" within the current
 * year, "Jun 14, 2025" otherwise. Returns null for missing/invalid dates so
 * the row simply omits the date rather than rendering "Invalid Date".
 */
function formatThreadCreatedAt(iso: string | undefined): string | null {
  if (!iso) return null;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  const sameYear = date.getFullYear() === new Date().getFullYear();
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  });
}

interface ThreadListProps {
  className?: string;
}

export const ThreadList: FC<ThreadListProps> = ({ className }) => {
  const d = useDensity();
  return (
    <ThreadListPrimitive.Root
      className={cn(
        "aui-root aui-thread-list-root flex flex-col items-stretch bg-background text-foreground",
        d("gap-sm"),
        className,
      )}
    >
      <div
        className={cn(
          "aui-thread-list-new-section border-b border-border pb-2",
          d("py-sm"),
          d("px-sm"),
        )}
      >
        <ThreadListNew />
      </div>
      <div
        className={cn(
          "aui-thread-list-items-section flex flex-col gap-1",
          d("py-xs"),
          d("px-sm"),
        )}
      >
        <ThreadListItems />
      </div>
    </ThreadListPrimitive.Root>
  );
};

const ThreadListNew: FC = () => {
  const d = useDensity();
  return (
    <ThreadListPrimitive.New asChild>
      <Button
        className={cn(
          "aui-thread-list-new flex w-full cursor-pointer items-center justify-start gap-1 rounded-lg px-2.5 py-2 text-start text-foreground hover:bg-muted data-[active=true]:bg-muted/80",
          d("p-sm"),
          d("py-xs"),
        )}
        variant="ghost"
      >
        <PlusIcon />
        New Thread
      </Button>
    </ThreadListPrimitive.New>
  );
};

const ThreadListItems: FC = () => {
  const isLoading = useAssistantState(({ threads }) => threads.isLoading);

  if (isLoading) {
    return <ThreadListSkeleton />;
  }

  return <ThreadListPrimitive.Items components={{ ThreadListItem }} />;
};

const ThreadListSkeleton: FC = () => {
  return (
    <>
      {Array.from({ length: 5 }, (_, i) => (
        <div
          key={i}
          role="status"
          aria-label="Loading threads"
          aria-live="polite"
          className="aui-thread-list-skeleton-wrapper flex items-center gap-2 rounded-md px-3 py-2"
        >
          <Skeleton className="aui-thread-list-skeleton h-[22px] grow" />
        </div>
      ))}
    </>
  );
};

const ThreadListItem: FC = () => {
  const r = useRadius();
  const d = useDensity();
  return (
    <ThreadListItemPrimitive.Root
      className={cn(
        "aui-thread-list-item group flex items-center gap-2 rounded-lg transition-all hover:bg-muted focus-visible:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none data-[active=true]:bg-muted",
        r("md"),
      )}
    >
      <ThreadListItemPrimitive.Trigger
        className={cn(
          // px-sm (not px-lg) so the row icon's left edge lines up with the
          // "New Thread" + icon above: that button's padding resolves to the
          // p-sm value, which equals px-sm at every density tier.
          "aui-thread-list-item-trigger flex min-w-0 grow cursor-pointer items-center gap-2.5 text-start",
          d("px-sm"),
          d("py-sm"),
        )}
      >
        <ThreadListItemIcon />
        <span className="flex min-w-0 flex-col">
          <ThreadListItemTitle />
          <ThreadListItemDate />
        </span>
      </ThreadListItemPrimitive.Trigger>
      {/* Archive button hidden until feature is implemented */}
      {/* <ThreadListItemArchive /> */}
    </ThreadListItemPrimitive.Root>
  );
};

/**
 * Row icon: the chat creator's avatar when `history.resolveCreator` resolves
 * one, otherwise the default message icon.
 */
const ThreadListItemIcon: FC = () => {
  const id = useAssistantState(
    ({ threadListItem }) =>
      threadListItem.remoteId ?? threadListItem.externalId,
  );
  const owner = useThreadMeta(id ?? undefined)?.owner;

  if (owner) {
    const display = owner.name || owner.email;
    return (
      <Avatar className="aui-thread-list-item-icon size-7 shrink-0">
        {owner.photoUrl ? (
          <AvatarImage src={owner.photoUrl} alt={display} />
        ) : null}
        <AvatarFallback className="border border-border bg-card text-[10px] font-medium text-muted-foreground">
          {initialsOf(display)}
        </AvatarFallback>
      </Avatar>
    );
  }

  return (
    <span className="aui-thread-list-item-icon flex size-7 shrink-0 items-center justify-center rounded-md border border-border bg-card text-muted-foreground">
      <MessageSquareTextIcon className="size-3.5" />
    </span>
  );
};

const ThreadListItemTitle: FC = () => {
  return (
    <span className="aui-thread-list-item-title block truncate text-sm text-foreground">
      <ThreadListItemPrimitive.Title fallback="New Chat" />
    </span>
  );
};

const ThreadListItemDate: FC = () => {
  // Both remoteId and externalId equal the chat id in the Gram adapter; the
  // side-channel map is keyed by that id. New local threads have neither yet,
  // so they simply render no date.
  const id = useAssistantState(
    ({ threadListItem }) =>
      threadListItem.remoteId ?? threadListItem.externalId,
  );
  const meta = useThreadMeta(id ?? undefined);
  const label = formatThreadCreatedAt(meta?.createdAt);
  if (!label) return null;
  return (
    <span className="aui-thread-list-item-date text-xs text-muted-foreground">
      {label}
    </span>
  );
};
