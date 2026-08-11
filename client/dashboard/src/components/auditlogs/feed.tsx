import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { getActionMeta } from "@/components/auditlogs/action-meta";
import {
  formatDateHeader,
  type FacetOption,
  type TimestampMode,
} from "@/lib/audit-log-feed";
import { cn } from "@/lib/utils";
import { Icon } from "@/components/ui/Icon";
import React from "react";

export function DateGroupHeader({
  date,
  mode,
}: {
  date: Date;
  mode: TimestampMode;
}): React.JSX.Element {
  return (
    <div className="flex items-center gap-3 px-4 py-2">
      <span className="text-eyebrow shrink-0">
        {formatDateHeader(date, mode)}
      </span>
      <div className="bg-border h-px flex-1" />
    </div>
  );
}

/**
 * The bordered square icon tile that leads an audit row — same idiom as the
 * project-home Activity Timeline, so clicking through from the timeline to
 * the audit log lands on rows that read identically.
 */
export function ActionIconTile({
  action,
  className,
}: {
  action: string;
  className?: string;
}): React.JSX.Element {
  const meta = getActionMeta(action);
  return (
    <div
      className={cn(
        "border-border bg-card relative flex size-8 shrink-0 items-center justify-center border",
        className,
      )}
    >
      <meta.icon className="text-muted-foreground size-4" />
      {meta.dot && (
        <span
          className={cn(
            "absolute top-1 right-1 size-1.5 rounded-full",
            meta.dot,
          )}
        />
      )}
    </div>
  );
}

export function FacetSelect({
  label,
  value,
  onValueChange,
  placeholder,
  allLabel,
  options,
}: {
  label: string;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
  allLabel: string;
  options: FacetOption[];
}): React.JSX.Element {
  return (
    <div className="flex flex-col gap-1.5">
      <Text small muted>
        {label}
      </Text>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger size="sm" className="bg-background min-w-[220px]">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{allLabel}</SelectItem>
          {options.map((option) => (
            <SelectItem
              key={option.value}
              value={option.value}
              description={
                option.count == null
                  ? undefined
                  : `${option.count.toLocaleString()} audit log${option.count === 1 ? "" : "s"}`
              }
            >
              {option.displayName}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

/**
 * Footer bar shared by audit log feeds: entry count on the left, cursor
 * pagination on the right. Render it inside the feed's bordered container.
 */
export function AuditFeedFooter({
  count,
  noun = "audit log",
  hasNextPage,
  isFetching,
  isFetchingNextPage,
  onLoadMore,
  endLabel = "End of audit log history",
}: {
  count: number;
  noun?: string;
  hasNextPage: boolean;
  isFetching: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
  endLabel?: string;
}): React.JSX.Element | null {
  if (count === 0 && !isFetchingNextPage) return null;

  return (
    <div className="bg-card flex items-center justify-between border-t px-4 py-3">
      <Text muted small>
        {count.toLocaleString()} {noun}
        {count === 1 ? "" : "s"}
      </Text>

      {hasNextPage ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={onLoadMore}
          disabled={isFetchingNextPage}
        >
          {isFetchingNextPage ? (
            <>
              <Icon name="loader-circle" className="size-4 animate-spin" />
              Loading...
            </>
          ) : (
            "Load more"
          )}
        </Button>
      ) : (
        <Text muted small>
          {isFetching ? "Refreshing..." : endLabel}
        </Text>
      )}
    </div>
  );
}
