import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import type { OrgMemoryDigest } from "@gram/client/models/components/orgmemorydigest.js";
import type { OrgMemoryMemo } from "@gram/client/models/components/orgmemorymemo.js";
import type { OrgMemorySession } from "@gram/client/models/components/orgmemorysession.js";
import { useOrgMemoryDigest } from "@gram/client/react-query/orgMemoryDigest.js";
import { useOrgMemoryDigests } from "@gram/client/react-query/orgMemoryDigests.js";
import { useOrgMemoryRecent } from "@gram/client/react-query/orgMemoryRecent.js";
import { pluralize } from "@/lib/format";
import { keepPreviousData } from "@tanstack/react-query";
import { format, formatDistanceToNow } from "date-fns";
import { useState } from "react";
import {
  childLevel,
  describeDigestSources,
  type DigestLevel,
  firstLine,
  formatPeriod,
} from "./memoryFormat";

const PAGE_SIZE = 12;
const MAX_DIGESTS = 60;
/** The timeline's entry for sessions and memos no day digest has folded yet. */
const RECENT = "recent";

const LEVEL_NOUN: Record<DigestLevel, string> = {
  day: "Day digest",
  week: "Week digest",
  month: "Month digest",
};

/**
 * The digest timeline: periods at one level on the left, newest first, and
 * the selected period on the right with everything it cites. A week or month
 * lists the digests it folded, and choosing one steps down a level; a day
 * lists its sessions and memos, which open the session sheet.
 */
export function OrgMemoryTimeline({
  level,
  selectedId,
  onSelect,
  onOpenSession,
}: {
  level: DigestLevel;
  selectedId: string | null;
  onSelect: (digestId: string, level?: DigestLevel) => void;
  // Undefined when the reader lacks chat:read: sessions show but don't open.
  onOpenSession: ((chatId: string) => void) | undefined;
}): JSX.Element {
  const [limit, setLimit] = useState(PAGE_SIZE);
  const digests = useOrgMemoryDigests({ level, limit }, undefined, {
    throwOnError: false,
    // "Show older" grows the page; keep the current one on screen meanwhile.
    placeholderData: keepPreviousData,
  });
  const recent = useOrgMemoryRecent({ limit: 20 }, undefined, {
    throwOnError: false,
  });

  const rows = digests.data?.digests ?? [];
  const recentCount =
    (recent.data?.sessions.length ?? 0) + (recent.data?.memos.length ?? 0);
  const hasRecent = recentCount > 0;
  // A "recent" selection whose items have all rolled up falls back to the
  // newest digest rather than an empty view.
  const selected = selectedId === RECENT && !hasRecent ? null : selectedId;
  const activeId =
    selected ?? (hasRecent ? RECENT : (rows[0]?.id ?? null)) ?? null;

  if (digests.isPending || recent.isPending) {
    return (
      <div className="grid gap-6 lg:grid-cols-[300px_minmax(0,1fr)]">
        <Skeleton className="h-80" />
        <Skeleton className="h-80" />
      </div>
    );
  }

  // Either failure would otherwise read as an empty memory.
  const loadError = digests.error ?? recent.error;
  if (loadError) {
    return (
      <InlineEmptyState
        icon="circle-alert"
        heading="Couldn't load Organizational Memory"
        description={
          loadError.message ||
          "Reading memory needs memory:read on this project."
        }
      />
    );
  }

  if (rows.length === 0 && !hasRecent) {
    return (
      <InlineEmptyState
        icon="brain"
        heading="Nothing remembered yet"
        description="Team sessions join memory about 30 minutes after they go quiet, and day, week and month digests roll up each night. Agents can also save memos with the remember tool."
      />
    );
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[300px_minmax(0,1fr)]">
      <nav
        aria-label="Memory periods"
        className="flex min-w-0 flex-col gap-1 self-start"
      >
        {hasRecent && (
          <PeriodButton
            active={activeId === RECENT}
            onClick={() => onSelect(RECENT)}
            title="Not rolled up yet"
            meta={pluralize(recentCount, "item")}
            preview="Sessions and memos waiting for tonight's day digest"
            dashed
          />
        )}
        {rows.map((digest) => (
          <PeriodButton
            key={digest.id}
            active={activeId === digest.id}
            onClick={() => onSelect(digest.id)}
            title={formatPeriod(
              digest.level,
              digest.periodStart,
              digest.periodEnd,
            )}
            meta={describeDigestSources(digest)}
            preview={firstLine(digest.content)}
          />
        ))}
        {rows.length === 0 && (
          <p className="text-muted-foreground px-3 py-2 text-sm">
            No {level} digests yet.
          </p>
        )}
        {digests.data?.nextBefore && limit < MAX_DIGESTS && (
          <Button
            variant="tertiary"
            size="sm"
            className="mt-1 self-start"
            onClick={() =>
              setLimit((n) => Math.min(n + PAGE_SIZE, MAX_DIGESTS))
            }
          >
            <Button.Text>Show older</Button.Text>
          </Button>
        )}
      </nav>

      <div className="min-w-0">
        {activeId === RECENT && recent.data ? (
          <RecentDetail
            sessions={recent.data.sessions}
            memos={recent.data.memos}
            onOpenSession={onOpenSession}
          />
        ) : activeId ? (
          <DigestDetail
            digestId={activeId}
            onSelect={onSelect}
            onOpenSession={onOpenSession}
          />
        ) : null}
      </div>
    </div>
  );
}

function PeriodButton({
  active,
  onClick,
  title,
  meta,
  preview,
  dashed,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  meta: string;
  preview: string;
  dashed?: boolean;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? "true" : undefined}
      className={cn(
        "hover:bg-muted/50 focus-visible:ring-ring flex min-w-0 flex-col gap-0.5 border px-3 py-2 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none",
        active ? "bg-muted border-border" : "border-transparent",
        dashed && !active && "border-border border-dashed",
      )}
    >
      <span className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
          {meta}
        </span>
      </span>
      <span className="text-muted-foreground truncate text-xs">{preview}</span>
    </button>
  );
}

function DigestDetail({
  digestId,
  onSelect,
  onOpenSession,
}: {
  digestId: string;
  onSelect: (digestId: string, level?: DigestLevel) => void;
  onOpenSession: ((chatId: string) => void) | undefined;
}): JSX.Element {
  const detail = useOrgMemoryDigest({ id: digestId }, undefined, {
    throwOnError: false,
  });

  if (detail.isPending) return <Skeleton className="h-80" />;
  if (detail.isError) {
    return (
      <InlineEmptyState
        icon="circle-alert"
        heading="Couldn't load this digest"
        description="It may have been rebuilt after a session left memory. Pick another period."
      />
    );
  }

  const { digest, children, sessions, memos } = detail.data;
  const level = digest.level as DigestLevel;
  const below = childLevel(level);

  return (
    <article className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="information">
            <Badge.Text>{LEVEL_NOUN[level] ?? "Digest"}</Badge.Text>
          </Badge>
          <span className="text-muted-foreground font-mono text-xs">
            {describeDigestSources(digest)} · written{" "}
            {formatDistanceToNow(digest.generatedAt, { addSuffix: true })}
          </span>
        </div>
        <h2 className="text-display-xs font-thin">
          {formatPeriod(digest.level, digest.periodStart, digest.periodEnd)}
        </h2>
      </header>

      <p className="text-foreground/90 max-w-[70ch] text-sm leading-relaxed whitespace-pre-wrap">
        {digest.content}
      </p>

      {children.length > 0 && below && (
        <Section
          title={`${below === "day" ? "Days" : "Weeks"} in this ${level}`}
        >
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {children.map((child) => (
              <ChildDigestCard
                key={child.id}
                digest={child}
                onClick={() => onSelect(child.id, below)}
              />
            ))}
          </div>
        </Section>
      )}

      {(sessions.length > 0 || memos.length > 0) && (
        <SourceLists
          sessions={sessions}
          memos={memos}
          onOpenSession={onOpenSession}
        />
      )}
    </article>
  );
}

function RecentDetail({
  sessions,
  memos,
  onOpenSession,
}: {
  sessions: OrgMemorySession[];
  memos: OrgMemoryMemo[];
  onOpenSession: ((chatId: string) => void) | undefined;
}): JSX.Element {
  return (
    <article className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <Badge variant="neutral">
          <Badge.Text>Not rolled up yet</Badge.Text>
        </Badge>
        <h2 className="text-display-xs font-thin">This week so far</h2>
        <p className="text-muted-foreground max-w-[70ch] text-sm">
          Agents already find these through search and recall. Tonight's rollup
          folds each closed day into a day digest.
        </p>
      </header>
      <SourceLists
        sessions={sessions}
        memos={memos}
        onOpenSession={onOpenSession}
      />
    </article>
  );
}

function SourceLists({
  sessions,
  memos,
  onOpenSession,
}: {
  sessions: OrgMemorySession[];
  memos: OrgMemoryMemo[];
  onOpenSession: ((chatId: string) => void) | undefined;
}): JSX.Element {
  return (
    <>
      {memos.length > 0 && (
        <Section title="Memos">
          <ul className="divide-border divide-y border">
            {memos.map((memo) => (
              <li key={memo.id} className="flex flex-col gap-1.5 px-4 py-3">
                <p className="text-sm leading-relaxed">{memo.content}</p>
                <p className="text-muted-foreground flex flex-wrap items-center gap-x-2 text-xs">
                  <span>
                    Saved by {memoAuthor(memo)} ·{" "}
                    {format(memo.createdAt, "MMM d, HH:mm")}
                  </span>
                  {memo.chatId && onOpenSession && (
                    <button
                      type="button"
                      className="text-foreground hover:underline"
                      onClick={() => onOpenSession(memo.chatId!)}
                    >
                      Open session
                    </button>
                  )}
                </p>
              </li>
            ))}
          </ul>
        </Section>
      )}
      {sessions.length > 0 && (
        <Section title="Sessions">
          <ul className="divide-border divide-y border">
            {sessions.map((session) => (
              <li key={session.id}>
                {onOpenSession ? (
                  <button
                    type="button"
                    onClick={() => onOpenSession(session.id)}
                    className="hover:bg-muted/40 focus-visible:ring-ring flex w-full min-w-0 flex-col gap-1 px-4 py-3 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
                  >
                    <SessionRow session={session} />
                  </button>
                ) : (
                  <div className="flex min-w-0 flex-col gap-1 px-4 py-3">
                    <SessionRow session={session} />
                  </div>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
    </>
  );
}

function ChildDigestCard({
  digest,
  onClick,
}: {
  digest: OrgMemoryDigest;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-muted/40 focus-visible:ring-ring flex min-w-0 flex-col gap-1 border px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
    >
      <span className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-medium">
          {formatPeriod(digest.level, digest.periodStart, digest.periodEnd)}
        </span>
        <span className="text-muted-foreground font-mono text-[11px]">
          {describeDigestSources(digest)}
        </span>
      </span>
      <span className="text-muted-foreground line-clamp-2 text-xs">
        {firstLine(digest.content)}
      </span>
    </button>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
        {title}
      </h3>
      {children}
    </section>
  );
}

function memoAuthor(memo: OrgMemoryMemo): string {
  if (memo.authorName) return memo.authorName;
  if (memo.authorKind === "agent") return "an agent";
  if (memo.authorKind === "user") return "a teammate";
  return "Speakeasy";
}

// Titles are empty for readers without chat:read; the time still places it.
function SessionRow({ session }: { session: OrgMemorySession }): JSX.Element {
  return (
    <>
      <span className="flex items-baseline justify-between gap-3">
        <span className="truncate text-sm font-medium">
          {session.title || "Session"}
        </span>
        <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
          {format(session.summarizedAt, "MMM d, HH:mm")}
        </span>
      </span>
      <span className="text-muted-foreground line-clamp-3 text-sm">
        {session.summary}
      </span>
    </>
  );
}
