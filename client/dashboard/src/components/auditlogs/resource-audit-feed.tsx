import {
  ActionIconTile,
  AuditFeedFooter,
  DateGroupHeader,
} from "@/components/auditlogs/feed";
import { subjectHref } from "@/components/auditlogs/subject-href";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Link as TextLink } from "@/components/ui/Link";
import { Text } from "@/components/ui/Text";
import { useSlugs } from "@/contexts/Sdk";
import {
  formatTimeOnly,
  groupLogsByDate,
  type TimestampMode,
} from "@/lib/audit-log-feed";
import {
  formatSubjectLabel,
  getActorLabel,
  renderVerb,
} from "@/lib/audit-log-format";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { useAuditLogsInfinite } from "@gram/client/react-query/auditLogs.js";
import React, { useMemo } from "react";
import { Link } from "react-router";

const TIMESTAMP_MODE: TimestampMode = "local";

// ResourceAuditFeed is the audit trail of one resource, embedded in its detail
// page: every entry whose subject is one of `subjectIds`. A resource's own
// events name it as the subject; events on its children name the child, which
// is why the caller passes the children's ids too rather than a single id.
//
// The list is exact and paginated server-side, so "load more" and the end of
// the feed mean what they say. Pass `enabled: false` until the child ids are
// known, otherwise the first page would be fetched twice.
export function ResourceAuditFeed({
  subjectIds,
  enabled = true,
  noun = "event",
  emptyMessage = "No activity recorded yet.",
}: {
  subjectIds: string[];
  enabled?: boolean;
  noun?: string;
  emptyMessage?: string;
}): JSX.Element {
  const { orgSlug } = useSlugs();
  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    isFetchNextPageError,
    isPending,
    refetch,
  } = useAuditLogsInfinite({ subjectIds }, undefined, {
    enabled,
    throwOnError: false,
  });

  const logs = useMemo(
    () => data?.pages.flatMap((page) => page.result.logs) ?? [],
    [data],
  );
  const dateGroups = useMemo(
    () => groupLogsByDate(logs, TIMESTAMP_MODE),
    [logs],
  );

  if (!enabled || isPending) {
    return (
      <div className="text-muted-foreground flex items-center gap-2 py-6">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Text small muted>
          Loading activity…
        </Text>
      </div>
    );
  }

  // A failure with nothing loaded yet replaces the feed. A failure while
  // loading a later page keeps every page already loaded (they are still in
  // `data`) and reports the failure inline instead, so a flaky "load more"
  // never wipes out the rows the reader was looking at.
  if (error && logs.length === 0) {
    return (
      <div className="flex flex-col items-start gap-2 py-6">
        <Text muted>Failed to load activity.</Text>
        <Button size="sm" variant="secondary" onClick={() => void refetch()}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  }

  if (logs.length === 0) {
    return (
      <Text muted className="py-6">
        {emptyMessage}
      </Text>
    );
  }

  return (
    <div className="bg-card overflow-hidden border">
      <div className="divide-border divide-y">
        {dateGroups.map((group) => (
          <React.Fragment key={group.key}>
            <DateGroupHeader date={group.date} mode={TIMESTAMP_MODE} />
            {group.logs.map((log) => (
              <ResourceAuditRow key={log.id} log={log} orgSlug={orgSlug} />
            ))}
          </React.Fragment>
        ))}
      </div>
      {error && (
        <div className="flex items-center justify-between gap-4 border-t px-4 py-3">
          {/* A failed "load more" and a failed refresh of the rows already on
              screen are different failures with different retries. */}
          <Text small muted>
            {isFetchNextPageError
              ? "The next page of activity could not be loaded."
              : "The activity could not be refreshed."}
          </Text>
          <Button
            size="sm"
            variant="secondary"
            onClick={() =>
              void (isFetchNextPageError ? fetchNextPage() : refetch())
            }
            disabled={isFetching}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      )}
      <AuditFeedFooter
        count={logs.length}
        noun={noun}
        hasNextPage={hasNextPage ?? false}
        isFetching={isFetching}
        isFetchingNextPage={isFetchingNextPage}
        onLoadMore={() => {
          void fetchNextPage();
        }}
        endLabel="End of activity"
      />
    </div>
  );
}

function ResourceAuditRow({
  log,
  orgSlug,
}: {
  log: AuditLog;
  orgSlug: string | undefined;
}): JSX.Element {
  const raw = log.subjectDisplayName || log.subjectSlug || log.subjectId;
  const label = formatSubjectLabel(raw, log.subjectType);
  const href = orgSlug ? subjectHref(log, orgSlug) : null;

  return (
    <div className="bg-card flex items-center gap-3 px-4 py-2.5">
      <ActionIconTile action={log.action} />
      <div className="min-w-0 flex-1 text-sm leading-5">
        <strong className="text-foreground font-semibold">
          {getActorLabel(log)}
        </strong>{" "}
        <span className="text-muted-foreground">{renderVerb(log)}</span>{" "}
        {href ? (
          <TextLink
            asChild
            size="xs"
            underline={false}
            className="font-mono text-xs"
          >
            <Link to={href} title={raw}>
              {label}
            </Link>
          </TextLink>
        ) : (
          <span className="text-muted-foreground font-mono text-xs" title={raw}>
            {label}
          </span>
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs tabular-nums">
        {formatTimeOnly(log.createdAt, TIMESTAMP_MODE)}
      </span>
    </div>
  );
}
