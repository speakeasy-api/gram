import { Alert } from "@/components/ui/Alert";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { connectionActivityLabel } from "@/lib/connection-state";
import { getInitials } from "@/lib/initials";
import type { McpServerUsageImpact } from "@gram/client/models/components/mcpserverusageimpact.js";
import type { McpServerUsageSubject } from "@gram/client/models/components/mcpserverusagesubject.js";
import { useMcpServerUsageImpact } from "@gram/client/react-query/mcpServerUsageImpact.js";
import type { UseQueryResult } from "@tanstack/react-query";
import type { ServerUsageConfirmationState } from "./useServerUsageConfirmation";

/**
 * Shows the active connections a breaking change cuts off, and the typed
 * confirmation when the state requires one.
 */
export function ServerUsageConfirmation({
  state,
  disabled = false,
}: {
  state: ServerUsageConfirmationState;
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="grid gap-4">
      <ServerUsageImpactPanel impact={state.impact} />
      {state.requireTyping && (
        <div className="grid gap-2">
          <Text small>
            To confirm, type the server name:{" "}
            <strong className="break-all">{state.confirmValue}</strong>
          </Text>
          <Input
            value={state.typed}
            onChange={state.setTyped}
            placeholder={state.confirmValue}
            disabled={disabled}
            aria-label="Type the server name to confirm"
            autoComplete="off"
          />
        </div>
      )}
    </div>
  );
}

function ServerUsageImpactPanel({
  impact,
  heading,
}: {
  impact: UseQueryResult<McpServerUsageImpact, unknown>;
  /** Names the server, for dialogs that list more than one. */
  heading?: string;
}): JSX.Element | null {
  let body: JSX.Element;
  if (impact.isPending && impact.fetchStatus === "idle") {
    return null;
  } else if (impact.isFetching && !impact.data) {
    body = (
      <Text small muted>
        Checking who uses this server…
      </Text>
    );
  } else if (impact.isError || !impact.data) {
    body = (
      <Alert variant="warning" dismissible={false}>
        Couldn't check who uses this server. Continue only if you're sure nobody
        depends on it.
      </Alert>
    );
  } else {
    body = <UsageSummary data={impact.data} />;
  }
  return (
    <div className="grid gap-2" data-testid="server-usage-impact">
      {heading && (
        <Text small className="font-medium">
          {heading}
        </Text>
      )}
      {body}
    </div>
  );
}

/**
 * The usage panel for one server, loading its own preview. For dialogs that
 * list several servers and already ask for a typed confirmation.
 */
export function ServerUsageImpactById({
  mcpServerId,
  heading,
}: {
  mcpServerId: string;
  heading?: string;
}): JSX.Element | null {
  const impact = useMcpServerUsageImpact({ id: mcpServerId }, undefined, {
    throwOnError: false,
    retry: false,
    staleTime: 0,
  });
  return <ServerUsageImpactPanel impact={impact} heading={heading} />;
}

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

function UsageSummary({ data }: { data: McpServerUsageImpact }): JSX.Element {
  if (!data.signInRequired) {
    return (
      <Alert variant="warning" dismissible={false}>
        This server doesn't require sign-in, so Speakeasy can't tell who is
        connected. Clients may be using it right now.
      </Alert>
    );
  }
  if (data.activeConnectionCount === 0) {
    return (
      <Text small muted>
        No active connections. Nobody holds a current sign-in to this server.
      </Text>
    );
  }

  const hidden = data.activeSubjectCount - data.subjects.length;
  return (
    <div className="grid gap-3">
      <Alert variant="warning" dismissible={false}>
        {plural(
          data.activeConnectionCount,
          "active connection",
          "active connections",
        )}{" "}
        from {plural(data.activeSubjectCount, "caller", "callers")} will be
        affected
        {data.lastUsedAt
          ? `, most recently ${connectionActivityLabel(data.lastUsedAt)}`
          : ""}
        .
      </Alert>
      {data.sharedSignIn && data.sharedConnectionCount > 0 && (
        <Text small muted>
          This server shares its sign-in with other servers. Callers marked{" "}
          <strong>Shared sign-in</strong> signed in once for all of them, so
          they may be using another server rather than this one.
        </Text>
      )}
      <div className="max-h-64 overflow-y-auto border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr className="text-left">
              <th className="px-3 py-2 font-medium">Caller</th>
              <th className="px-3 py-2 text-right font-medium">Connections</th>
              <th className="px-3 py-2 font-medium">Last used</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {data.subjects.map((subject) => (
              <SubjectRow key={subject.subjectUrn} subject={subject} />
            ))}
          </tbody>
        </table>
      </div>
      {data.subjectsTruncated && hidden > 0 && (
        <Text small muted>
          and {plural(hidden, "more caller", "more callers")}.
        </Text>
      )}
    </div>
  );
}

const SUBJECT_TYPE_LABEL: Record<string, string> = {
  user: "User",
  apikey: "API key",
  agent: "Agent",
  anonymous: "Anonymous",
  workload: "Workload",
};

function usageSubjectLabel(subject: McpServerUsageSubject): string {
  if (subject.displayName) return subject.displayName;
  if (subject.email) return subject.email;
  const kind = SUBJECT_TYPE_LABEL[subject.subjectType] ?? subject.subjectType;
  const id = subject.subjectUrn.slice(subject.subjectUrn.indexOf(":") + 1);
  return `${kind} ${id}`;
}

function SubjectRow({
  subject,
}: {
  subject: McpServerUsageSubject;
}): JSX.Element {
  const label = usageSubjectLabel(subject);
  const secondary =
    subject.email && subject.email !== label
      ? subject.email
      : subject.subjectType !== "user"
        ? (SUBJECT_TYPE_LABEL[subject.subjectType] ?? subject.subjectType)
        : undefined;
  const lastUsed = subject.lastUsedAt
    ? connectionActivityLabel(subject.lastUsedAt).replace(/^used /, "")
    : "unknown";
  return (
    <tr>
      <td className="px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Avatar className="size-6 shrink-0">
            {subject.photoUrl && <AvatarImage src={subject.photoUrl} alt="" />}
            <AvatarFallback className="text-[10px]">
              {getInitials(label)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <Text small className="truncate">
                {label}
              </Text>
              {subject.sharedSignIn && (
                <span title="Signed in once for every server sharing this sign-in, so this connection may belong to another server.">
                  <Badge variant="neutral" size="sm">
                    Shared sign-in
                  </Badge>
                </span>
              )}
            </div>
            {(secondary || subject.clientNames.length > 0) && (
              <Text small muted className="truncate" as="div">
                {[secondary, subject.clientNames.join(", ")]
                  .filter(Boolean)
                  .join(" · ")}
              </Text>
            )}
          </div>
        </div>
      </td>
      <td className="px-3 py-2 text-right tabular-nums">
        {subject.connectionCount}
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        <Text small muted={!subject.lastUsedAt}>
          {lastUsed}
        </Text>
      </td>
    </tr>
  );
}
