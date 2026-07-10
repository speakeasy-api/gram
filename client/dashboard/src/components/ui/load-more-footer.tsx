import { LoaderCircle } from "lucide-react";
import * as React from "react";

import { Button } from "@/components/ui/moonshine";
import { cn } from "@/lib/utils";

function formatShownCount(
  shown: number,
  total: number | undefined,
  noun: string,
): string {
  const shownLabel = shown.toLocaleString();
  if (total === undefined) {
    return `Showing ${shownLabel} ${noun}`;
  }
  return `Showing ${shownLabel} of ${total.toLocaleString()} ${noun}`;
}

export interface LoadMoreFooterProps {
  /** Number of rows currently rendered. */
  shown: number;
  /** Total row count, if known. */
  total?: number;
  /** Plural noun used in the count text, e.g. "tools", "chats". */
  noun?: string;
  hasMore: boolean;
  isLoading?: boolean;
  onLoadMore: () => void;
  className?: string;
  /** Overrides the "End of list" label shown once every row is loaded. */
  endLabel?: string;
  /**
   * Shows a "Refreshing…" label in place of `endLabel` while a background
   * refetch is in flight — only relevant once `hasMore` is false.
   */
  isRefreshing?: boolean;
}

/**
 * List footer bar: mono count on the left, a "Load more" button (or an
 * "end of list" label once exhausted) on the right.
 */
export function LoadMoreFooter({
  shown,
  total,
  noun = "items",
  hasMore,
  isLoading = false,
  onLoadMore,
  className,
  endLabel = "End of list",
  isRefreshing = false,
}: LoadMoreFooterProps): React.JSX.Element {
  return (
    <div
      className={cn(
        "bg-muted/20 flex items-center justify-between border-t px-4 py-3",
        className,
      )}
    >
      <span className="font-mono text-xs uppercase tracking-[0.08em] text-muted">
        {formatShownCount(shown, total, noun)}
      </span>
      {hasMore ? (
        <LoadMoreTrigger isLoading={isLoading} onLoadMore={onLoadMore} />
      ) : (
        <span className="font-mono text-xs uppercase tracking-[0.08em] text-muted">
          {isRefreshing ? "Refreshing…" : endLabel}
        </span>
      )}
    </div>
  );
}

function LoadMoreTrigger({
  isLoading,
  onLoadMore,
  label = "Load more",
}: {
  isLoading: boolean;
  onLoadMore: () => void;
  label?: string;
}): React.JSX.Element {
  return (
    <Button
      variant="tertiary"
      size="sm"
      onClick={onLoadMore}
      disabled={isLoading}
    >
      {isLoading && (
        <Button.LeftIcon>
          <LoaderCircle className="animate-spin" />
        </Button.LeftIcon>
      )}
      <Button.Text>{isLoading ? "Loading" : label}</Button.Text>
    </Button>
  );
}

export interface LoadMoreButtonProps {
  hasMore: boolean;
  isLoading?: boolean;
  onLoadMore: () => void;
  label?: string;
  className?: string;
}

/** Bare centered load-more button, without the surrounding count row. */
export function LoadMoreButton({
  hasMore,
  isLoading = false,
  onLoadMore,
  label,
  className,
}: LoadMoreButtonProps): React.JSX.Element | null {
  if (!hasMore) return null;

  return (
    <div className={cn("flex justify-center py-4", className)}>
      <LoadMoreTrigger
        isLoading={isLoading}
        onLoadMore={onLoadMore}
        label={label}
      />
    </div>
  );
}
