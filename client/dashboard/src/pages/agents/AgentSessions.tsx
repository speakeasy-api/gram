import { useEffect, useState } from "react";
import { Link } from "react-router";
import { SettingsSection } from "@/components/page-templates";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip, TooltipProvider } from "@/components/ui/Tooltip";
import { HumanizeDateTime, dateTimeFormatters } from "@/lib/dates";
import { IdentityLink } from "@/components/identity-link";

/**
 * Presentation model for one way in: a session from the agent-scoped sessions
 * endpoint, or one of the agent's API keys that has been used.
 */
export type AgentSessionRow = {
  id: string;
  /**
   * How it signed in. "agent" is an MCP sign-in that chose this agent;
   * "workload" a platform that federates as it; "api-key" one of its keys,
   * which opens no session. Absent reads as "agent". A string, not a union:
   * the SDK passes through kinds a newer server adds.
   */
  subjectKind?: string;
  /** The workload's subject as its platform issued it. */
  workloadSubject?: string;
  /** For a key, the visible start of it, so two keys can be told apart. */
  keyPrefix?: string;
  clientName?: string;
  /**
   * The person who approved this session, or who issued the key behind a key
   * row. A session a federated platform opened has none. Not who the agent is
   * upstream: each server decides that with its own sign-in.
   */
  authorizerUserId?: string;
  issuerSlug?: string;
  refreshExpiresAt?: Date;
  createdAt: Date;
  expiresAt: Date;
  lastUsedAt?: Date;
  revokedAt?: Date;
};

export type AgentSessionsSectionProps = {
  /**
   * "bare" drops the section chrome for a caller that supplies its own.
   * "panel" also drops the table's frame, for a caller whose panel border
   * already encloses it.
   */
  variant?: "section" | "bare" | "panel";
  sessions: AgentSessionRow[];
  isLoading: boolean;
  isError: boolean;
  /**
   * Resolves an authorizer's user id to their name. Omitted leaves the id,
   * which is still true and still links.
   */
  authorizerName?: (userId: string) => string | undefined;
  /** Use the agent's server-calculated credential permission, not an RBAC scope. */
  canRevoke: boolean;
  canRead?: boolean;
  onRetry: () => void;
  onRevoke: (session: AgentSessionRow) => Promise<void>;
  hasMore?: boolean;
  isLoadingMore?: boolean;
  loadMoreError?: boolean;
  onLoadMore?: () => void;
  /**
   * Where a key or workload row is managed. Neither is revoked here: a key is
   * revoked with the agent's credentials, and a workload session is
   * short-lived and ends when its platform stops signing in.
   */
  credentialsHref?: string;
};

/** Whether a row is a session this table can revoke. */
function isRevocableSession(session: AgentSessionRow): boolean {
  return (session.subjectKind ?? "agent") === "agent";
}

/** The day, with the exact moment on hover — the shape credential tables on
 * the agent page share. */
function SessionDate({ date }: { date: Date }): JSX.Element {
  return (
    <time
      className="tabular-nums"
      title={dateTimeFormatters.full.format(date)}
      dateTime={date.toISOString()}
    >
      {dateTimeFormatters.day.format(date)}
    </time>
  );
}

export function AgentSessionsSection({
  variant = "section",
  sessions,
  isLoading,
  isError,
  canRevoke,
  canRead = true,
  onRetry,
  onRevoke,
  hasMore = false,
  isLoadingMore = false,
  loadMoreError = false,
  onLoadMore,
  authorizerName,
  credentialsHref,
}: AgentSessionsSectionProps): JSX.Element {
  const [selected, setSelected] = useState<AgentSessionRow | null>(null);
  const [pending, setPending] = useState(false);
  const [revokeError, setRevokeError] = useState(false);
  // Defaulted, not fetched here: this component is handed its data, and a
  // query inside it would make every caller supply a client to render rows
  // it already holds.
  const nameFor = (id: string) => authorizerName?.(id) || id;

  const [clock, setClock] = useState(Date.now);
  const now = Math.max(clock, Date.now());
  useEffect(() => {
    if (!canRead || isLoading || isError || sessions.length === 0) return;
    let timer: number | undefined;
    const schedule = () => {
      window.clearTimeout(timer);
      if (document.hidden) return;
      const currentTime = Math.max(clock, Date.now());
      const nextExpiry = sessions.reduce((next, session) => {
        const expiry = session.refreshExpiresAt?.getTime();
        return !session.revokedAt &&
          expiry !== undefined &&
          Number.isFinite(expiry) &&
          expiry > currentTime
          ? Math.min(next, expiry)
          : next;
      }, Infinity);
      if (Number.isFinite(nextExpiry)) {
        // Browsers overflow delays larger than a signed 32-bit integer.
        timer = window.setTimeout(
          () => setClock(Date.now()),
          Math.min(nextExpiry - currentTime, 2_147_483_647),
        );
      }
    };
    const onVisibilityChange = () => {
      setClock(Date.now());
      schedule();
    };
    schedule();
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [sessions, canRead, isLoading, isError, clock]);

  useEffect(() => {
    if (!canRead) {
      setSelected(null);
      setRevokeError(false);
    }
  }, [canRead]);

  async function confirmRevoke() {
    if (!selected || !canRead || !canRevoke || pending) return;
    setPending(true);
    setRevokeError(false);
    try {
      await onRevoke(selected);
      setSelected(null);
    } catch {
      setRevokeError(true);
    } finally {
      setPending(false);
    }
  }

  const columns: Column<AgentSessionRow>[] = [
    {
      key: "clientName",
      header: "Client",
      width: "1.5fr",
      render: (session) => {
        if (session.subjectKind === "api-key") {
          return (
            <span className="min-w-0 truncate" title={session.clientName}>
              {session.clientName || "Unnamed key"}
              {session.keyPrefix && (
                <span className="text-muted-foreground font-mono text-xs">
                  {" "}
                  {session.keyPrefix}
                </span>
              )}
            </span>
          );
        }
        if (session.subjectKind === "workload") {
          return (
            <span
              className="min-w-0 truncate font-mono text-xs"
              title={session.workloadSubject}
            >
              {session.workloadSubject || "Unknown workload"}
            </span>
          );
        }
        // An MCP client names itself when it registers, so the name is a
        // claim, not a fact.
        return (
          <span title="The name the client gave itself. Not verified.">
            {session.clientName || "Unknown client"}
          </span>
        );
      },
    },
    {
      key: "authorizerUserId",
      // The person who approved the session, so a reader never revokes
      // someone's delegation without being told whose it was. Not who the
      // agent is upstream: each server decides that with its own sign-in.
      header: "Authorized by",
      width: "1fr",
      render: (session) =>
        session.authorizerUserId ? (
          <IdentityLink identifier={{ userId: session.authorizerUserId }}>
            {nameFor(session.authorizerUserId)}
          </IdentityLink>
        ) : (
          <TooltipProvider>
            <SimpleTooltip tooltip="No person is on record for this connection: the agent connected with its own key or its platform's identity. On each server it then signs in however that server is set up, with a shared credential or a connection assigned to the agent.">
              <span
                tabIndex={0}
                className="text-muted-foreground cursor-default underline decoration-dotted underline-offset-4"
              >
                No one
              </span>
            </SimpleTooltip>
          </TooltipProvider>
        ),
    },
    {
      key: "issuerSlug",
      header: "Signed in with",
      // Issuer slugs are long and hyphenated, so they truncate in a share of
      // the row rather than wrapping it taller than every other one.
      width: "1fr",
      render: (session) =>
        session.subjectKind === "api-key" ? (
          "API key"
        ) : session.subjectKind === "workload" ? (
          "Workload identity"
        ) : (
          <span className="min-w-0 truncate" title={session.issuerSlug}>
            {session.issuerSlug || "Unknown issuer"}
          </span>
        ),
    },
    {
      key: "createdAt",
      header: "Created",
      width: "auto",
      render: (session) => <SessionDate date={session.createdAt} />,
    },
    {
      key: "lastUsedAt",
      header: "Last used",
      width: "auto",
      // The one column where elapsed time is the answer: "2 days ago" says
      // whether this session is still in use, which a date does not.
      //
      // Absent is not never: a session older than the tracking reports no
      // last use, and "Never" would state something about it that is not
      // known.
      render: (session) =>
        session.lastUsedAt ? (
          <HumanizeDateTime date={session.lastUsedAt} />
        ) : (
          "Unknown"
        ),
    },
    {
      key: "refreshExpiresAt",
      header: "Expires",
      width: "auto",
      render: (session) =>
        session.refreshExpiresAt ? (
          <SessionDate date={session.refreshExpiresAt} />
        ) : session.subjectKind === "api-key" ? (
          "Never"
        ) : (
          "Unknown"
        ),
    },
    {
      key: "status",
      header: "Status",
      width: "auto",
      render: (session) => (
        <Badge
          size="sm"
          variant={
            session.revokedAt
              ? "destructive"
              : sessionIsExpired(session, now)
                ? "neutral"
                : "success"
          }
        >
          {session.revokedAt
            ? "Revoked"
            : sessionIsExpired(session, now)
              ? "Expired"
              : "Active"}
        </Badge>
      ),
    },
    {
      key: "actions",
      header: "",
      // Content-sized, so the button is never pushed past the panel edge
      // when fixed columns outgrow a narrow detail page.
      width: "auto",
      render: (session) => {
        if (!isRevocableSession(session)) {
          return credentialsHref ? (
            <Button asChild size="sm" variant="secondary">
              <Link to={credentialsHref}>
                {session.subjectKind === "api-key"
                  ? "Manage key"
                  : "Manage access"}
              </Link>
            </Button>
          ) : null;
        }
        return !session.revokedAt && canRevoke ? (
          <Button
            size="sm"
            variant="destructive-secondary"
            onClick={() => {
              setRevokeError(false);
              setSelected(session);
            }}
          >
            Revoke session
          </Button>
        ) : null;
      },
    },
  ];

  // The panel supplies its own frame, so inner tables and states drop theirs.
  const frameless = variant === "panel" ? "border-none" : undefined;
  const body = (
    <>
      {!canRead ? (
        <Text>You do not have permission to view this agent's sessions.</Text>
      ) : isLoading ? (
        <div role="status" aria-label="Loading agent sessions">
          <SkeletonTable className={frameless} />
        </div>
      ) : isError ? (
        <div role="alert" className="space-y-3">
          <Text>Unable to load this agent's connections.</Text>
          <Button variant="secondary" onClick={onRetry}>
            Try again
          </Button>
        </div>
      ) : sessions.length === 0 ? (
        <InlineEmptyState
          icon="key-round"
          heading="No sessions yet"
          description="Connections appear here once this agent's API key is used, a platform signs in as it, or someone picks it when authorizing an MCP connection."
          className={frameless}
        />
      ) : (
        <Table
          columns={columns}
          data={sessions}
          rowKey={(session) => session.id}
          className={frameless}
        />
      )}
      {canRead && loadMoreError && (
        <Text role="alert">Unable to load more sessions. Try again.</Text>
      )}
      {canRead && hasMore && onLoadMore && (
        <Button
          variant="secondary"
          disabled={isLoadingMore}
          onClick={onLoadMore}
        >
          {isLoadingMore ? "Loading more…" : "Load more sessions"}
        </Button>
      )}
      {canRead && !canRevoke && (
        <Text small muted>
          You do not have permission to revoke this agent's sessions.
        </Text>
      )}
    </>
  );

  return (
    <>
      {variant !== "section" ? (
        // No border and no padding: the table draws its own, and the caller
        // supplying "bare" has already drawn the panel around it. Three
        // nested rectangles read as a mistake, which it was.
        <div className="space-y-4">{body}</div>
      ) : (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Sessions</SettingsSection.Title>
            <SettingsSection.Description>
              The clients acting as this agent right now. Revoking one stops it
              without touching the agent's keys.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <SettingsSection.Panel>
            <SettingsSection.Body>{body}</SettingsSection.Body>
          </SettingsSection.Panel>
        </SettingsSection>
      )}
      <Dialog
        open={canRead && selected !== null}
        onOpenChange={(open) => {
          if (!open && !pending) setSelected(null);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke session?</Dialog.Title>
            <Dialog.Description>
              This permanently revokes the selected session. The client must
              authorize a new session to reconnect.
            </Dialog.Description>
          </Dialog.Header>
          {canRead && selected && (
            <Text small>
              Client name (unverified):{" "}
              {selected.clientName || "Unknown client"}. Created{" "}
              <HumanizeDateTime date={selected.createdAt} />.
              {/* Whose delegation is ending. Revoking one without saying so
                  is the one place this omission actually costs something. */}
              {selected.authorizerUserId
                ? ` Authorized by ${nameFor(selected.authorizerUserId)}.`
                : ""}
            </Text>
          )}
          {revokeError && (
            <Text role="alert">Unable to revoke the session. Try again.</Text>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => setSelected(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={pending || !canRevoke}
              onClick={() => void confirmRevoke()}
            >
              {pending ? "Revoking…" : "Confirm revoke"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function sessionIsExpired(session: AgentSessionRow, now: number): boolean {
  return (
    session.refreshExpiresAt !== undefined &&
    session.refreshExpiresAt.getTime() <= now
  );
}
