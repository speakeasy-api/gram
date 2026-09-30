import { useRef, useState, type JSX, type ReactNode } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import {
  ArrowRightIcon,
  CalendarIcon,
  CheckIcon,
  CopyIcon,
  ExternalLinkIcon,
} from "lucide-react";
import type { AdminMcpServerHealth } from "@gram/admin-client/models/components/adminmcpserverhealth";
import type { AdminMcpServerHealthRemoteSessionClient } from "@gram/admin-client/models/components/adminmcpserverhealthremotesessionclient";
import type { AdminMcpServerHealthSeriesPoint } from "@gram/admin-client/models/components/adminmcpserverhealthseriespoint";
import type { AdminMcpServerHealthToolCalls } from "@gram/admin-client/models/components/adminmcpserverhealthtoolcalls";
import type { AdminMcpServerHealthUserSessionIssuer } from "@gram/admin-client/models/components/adminmcpserverhealthusersessionissuer";

import { CopyValue } from "@/components/CopyValue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import {
  organizationProjectsQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import { badgeTone } from "@/lib/badgeTone";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import { mcpServerHealthQuery } from "@/lib/gramAdminClient";
import { LEAVES_THE_APP } from "@/lib/impersonation";
import { cn } from "@/lib/utils";

import { SOURCE_LABELS, VISIBILITY_LABELS } from "./mcpServerLabels";
import {
  admissionLabel,
  bucketSquares,
  callsPerSquare,
  CHALLENGE_MODE_LABELS,
  durationLabel,
  fmtBucketDay,
  fmtDate,
  fmtDateTime,
  fmtShare,
  humanize,
  LEGACY_AUTH_LABELS,
  linkedAccounts,
  loginChallengeQuery,
  loginChallengeUrl,
  platformMcpPrompt,
  SCOPE_LABELS,
  toolCallTailQuery,
  toolCallTailUrl,
  toolCallTotals,
  windowRange,
  worstBucket,
} from "./mcpServerHealthModel";
import { HEALTH_WINDOWS, type HealthWindow } from "./mcpServerHealthSearch";

const ROUTE = "/organizations/$idOrSlug/mcp-servers/$serverId";
const COPY_CONFIRM_MS = 1500;

const CARD = "bg-card rounded-lg border";
const CARD_HEAD =
  "flex items-center justify-between gap-3 border-b px-5 py-3.5";
const MUTED = "text-muted-foreground";
const MONO = "font-mono text-xs";
const CARD_TITLE = "text-[0.9375rem] font-semibold";

export function McpServerHealthRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <McpServerHealth org={data} idOrSlug={idOrSlug} />;
}

export function McpServerHealth({
  org,
  idOrSlug,
}: {
  org: AdminOrganization;
  idOrSlug: string;
}): JSX.Element {
  const { serverId } = useParams({ from: ROUTE });
  const { project, window = 14 } = useSearch({ from: ROUTE });
  const navigate = useNavigate({ from: ROUTE });
  const projects = useQuery(organizationProjectsQuery(org.id));
  const health = useQuery({
    ...mcpServerHealthQuery(idOrSlug, {
      organizationId: org.id,
      projectId: project ?? "",
      mcpServerId: serverId,
      windowDays: window,
    }),
    enabled: !!project,
    // A new window keeps the last one on screen until it lands, rather than
    // blanking the page between picks.
    placeholderData: keepPreviousData,
  });

  // The server is named inside a project, so an address without one has
  // nothing to ask for. The list picks a project and links back here.
  if (!project) {
    return (
      <Navigate
        to="/organizations/$idOrSlug/mcp-servers"
        params={{ idOrSlug }}
        replace
      />
    );
  }

  if (!health.data) {
    return (
      <span className={cn(MUTED, "text-sm")}>
        {health.isError
          ? `Unable to load server health: ${errorMessage(health.error)}`
          : "Loading..."}
      </span>
    );
  }

  const projectName =
    projects.data?.projects.find((p) => p.id === project)?.name ?? project;

  return (
    <HealthReport
      health={health.data}
      idOrSlug={idOrSlug}
      project={project}
      projectName={projectName}
      window={window}
      // The report is as of its own fetch, so links and ranges built from it
      // do not drift on every render.
      asOf={new Date(health.dataUpdatedAt)}
      onWindowChange={(next) => {
        void navigate({
          search: (prev) => ({ ...prev, window: next }),
          replace: true,
        });
      }}
    />
  );
}

function HealthReport({
  health,
  idOrSlug,
  project,
  projectName,
  window,
  asOf,
  onWindowChange,
}: {
  health: AdminMcpServerHealth;
  idOrSlug: string;
  project: string;
  projectName: string;
  window: HealthWindow;
  asOf: Date;
  onWindowChange: (window: HealthWindow) => void;
}): JSX.Element {
  const { server, userSessionIssuer: issuer, toolCalls } = health;
  const range = windowRange(window, asOf);
  const clients = issuer?.remoteSessionClients ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h4 className="text-[1.438rem] leading-[1.6] font-light">
              {server.name}
            </h4>
            <Badge variant="outline" className={badgeTone.neutral}>
              {SOURCE_LABELS[server.source] ?? server.source}
            </Badge>
            <Badge variant="outline" className={badgeTone.neutral}>
              {VISIBILITY_LABELS[server.visibility] ?? server.visibility}
            </Badge>
          </div>
          <p
            className={cn(MUTED, "flex flex-wrap items-center gap-x-2 text-sm")}
          >
            <span className="whitespace-nowrap">{projectName} project</span>
            <span aria-hidden="true">·</span>
            <span className="whitespace-nowrap">
              Created {fmtDate(server.createdAt)}
            </span>
            <span aria-hidden="true">·</span>
            <CopyValue label={`${server.name} server id`} value={server.id} />
          </p>
        </div>
        <WindowPicker value={window} onChange={onWindowChange} />
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <IssuerCard health={health} />
        <PeopleCard issuer={issuer} />
        <UpstreamCard issuer={issuer} />
        <ToolCallsCard toolCalls={toolCalls} />
      </div>

      {toolCalls.type === "logging:enabled" ? (
        <ToolCallsChart
          toolCalls={toolCalls}
          range={range}
          prompt={{
            serverName: server.name,
            serverId: server.id,
            projectName,
          }}
        />
      ) : (
        <LoggingOff idOrSlug={idOrSlug} />
      )}

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {issuer && (
          <IssuerPanel
            issuer={issuer}
            idOrSlug={idOrSlug}
            project={project}
            window={window}
          />
        )}
        <LogsCard
          urlSlug={health.correlation.urlSlug}
          issuers={clients.map((c) => c.issuer.issuer)}
          range={range}
          className={cn(!issuer && "lg:col-span-2")}
        />
      </div>

      {clients.length > 0 && <RemoteClients clients={clients} />}
    </div>
  );
}

function WindowPicker({
  value,
  onChange,
}: {
  value: HealthWindow;
  onChange: (window: HealthWindow) => void;
}): JSX.Element {
  return (
    <Select
      value={String(value)}
      onValueChange={(next) => onChange(Number(next) as HealthWindow)}
    >
      <SelectTrigger size="sm" aria-label="Window" className="bg-card">
        <CalendarIcon />
        <span className={MUTED}>Window</span>
        <SelectValue>
          <span className="font-medium">Last {value} days</span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent position="popper" align="end">
        {HEALTH_WINDOWS.map((days) => (
          <SelectItem key={days} value={String(days)}>
            Last {days} days
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function StatCard({
  label,
  value,
  badge,
  detail,
  muted = false,
}: {
  label: string;
  value: ReactNode;
  badge?: ReactNode;
  detail: string;
  muted?: boolean;
}): JSX.Element {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn(CARD, "flex flex-col gap-2 px-5 py-4")}
    >
      <span className={cn(MUTED, "text-xs")}>{label}</span>
      <div className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            "text-[1.375rem] font-light tabular-nums",
            muted && MUTED,
          )}
        >
          {value}
        </span>
        {badge}
      </div>
      <span className={cn(MUTED, "text-[0.8125rem]")}>{detail}</span>
    </div>
  );
}

function IssuerCard({ health }: { health: AdminMcpServerHealth }): JSX.Element {
  const issuer = health.userSessionIssuer;
  if (issuer) {
    const clients = issuer.remoteSessionClients.length;
    return (
      <StatCard
        label="User session issuer"
        value="Configured"
        badge={
          <Badge variant="outline" className={badgeTone.success}>
            {CHALLENGE_MODE_LABELS[issuer.authnChallengeMode]}
          </Badge>
        }
        detail={`${durationLabel(issuer.sessionDurationHours)} sessions · ${clients === 1 ? "1 upstream client" : `${clients} upstream clients`}`}
      />
    );
  }
  const legacy = health.legacyAuth;
  return (
    <StatCard
      label="User session issuer"
      value="None"
      badge={
        legacy && (
          <Badge variant="outline" className={badgeTone.warning}>
            legacy: {LEGACY_AUTH_LABELS[legacy] ?? legacy}
          </Badge>
        )
      }
      detail={
        legacy
          ? "Uses an older auth mode this page does not describe"
          : "No user sign-in configured"
      }
    />
  );
}

function PeopleCard({
  issuer,
}: {
  issuer: AdminMcpServerHealthUserSessionIssuer | undefined;
}): JSX.Element {
  if (!issuer) {
    return (
      <StatCard
        label="People signed in"
        value="—"
        muted
        detail="No issuer, so no sessions to count"
      />
    );
  }
  const { sessions } = issuer;
  return (
    <StatCard
      label="People signed in"
      value={
        <>
          {sessions.distinctSubjectsEver}{" "}
          <span className={cn(MUTED, "text-sm")}>ever</span>
        </>
      }
      detail={`${sessions.distinctSubjectsInWindow} in window · ${sessions.live === 1 ? "1 live session" : `${sessions.live} live sessions`}`}
    />
  );
}

function UpstreamCard({
  issuer,
}: {
  issuer: AdminMcpServerHealthUserSessionIssuer | undefined;
}): JSX.Element {
  const clients = issuer?.remoteSessionClients ?? [];
  if (clients.length === 0) {
    return (
      <StatCard
        label="Upstream accounts linked"
        value="—"
        muted
        detail="No remote session clients"
      />
    );
  }
  const accounts = linkedAccounts(clients);
  return (
    <StatCard
      label="Upstream accounts linked"
      value={accounts.linked}
      badge={
        accounts.invalid > 0 && (
          <Badge variant="outline" className={badgeTone.warning}>
            {accounts.invalid} invalid
          </Badge>
        )
      }
      detail={
        accounts.reauthorizations === 1
          ? "1 reauthorization"
          : `${accounts.reauthorizations} reauthorizations`
      }
    />
  );
}

function ToolCallsCard({
  toolCalls,
}: {
  toolCalls: AdminMcpServerHealthToolCalls;
}): JSX.Element {
  if (toolCalls.type === "logging:disabled" || !toolCalls.outcomes) {
    return (
      <StatCard
        label="Tool calls"
        value="Unknown"
        badge={
          <Badge variant="outline" className={badgeTone.neutral}>
            logging off
          </Badge>
        }
        detail="Not zero: nothing is recorded"
      />
    );
  }
  const totals = toolCallTotals(toolCalls.outcomes);
  return (
    <StatCard
      label="Tool calls"
      value={totals.total}
      badge={
        totals.total > 0 && (
          <Badge
            variant="outline"
            className={
              totals.failed > 0 ? badgeTone.warning : badgeTone.success
            }
          >
            {fmtShare(totals.failedShare)} failed
          </Badge>
        )
      }
      detail={`${totals.failed} failed · ${totals.unauthorized} unauthorized`}
    />
  );
}

function rangeLabel({ from, to }: { from: Date; to: Date }): string {
  return `${fmtBucketDay(from)} – ${fmtDate(to)}`;
}

function ToolCallsChart({
  toolCalls,
  range,
  prompt,
}: {
  toolCalls: AdminMcpServerHealthToolCalls;
  range: { from: Date; to: Date };
  prompt: { serverName: string; serverId: string; projectName: string };
}): JSX.Element {
  const points = toolCalls.daily ?? [];
  const weekly = (toolCalls.bucketSeconds ?? 86_400) >= 7 * 86_400;
  const perSquare = callsPerSquare(points);
  const total = points.reduce((sum, p) => sum + p.total, 0);
  const failed = points.reduce((sum, p) => sum + p.failed, 0);
  const share = total === 0 ? 0 : failed / total;
  const worst = worstBucket(points);
  // Past fifteen columns every label will not fit, so every other one shows.
  const labelEvery = points.length > 15 ? 2 : 1;
  const unit = weekly ? "week" : "day";

  const summary = worst
    ? `${failed} of ${total} calls failed; the worst ${unit} was ${fmtBucketDay(worst.bucketStart)} with ${worst.failed} of ${worst.total}.`
    : `${total} calls, none failed.`;

  return (
    <section className={CARD} aria-labelledby="tool-calls-chart">
      <div className={CARD_HEAD}>
        <h2 id="tool-calls-chart" className={CARD_TITLE}>
          Tool calls per {unit}
        </h2>
        <span className={cn(MUTED, "text-xs")}>
          {rangeLabel(range)}
          {toolCalls.watermark &&
            ` · data as of ${fmtDateTime(toolCalls.watermark)}`}
        </span>
      </div>
      <div className="flex flex-col gap-3 px-5 pt-4 pb-3.5">
        <div
          className={cn(
            MUTED,
            "flex flex-wrap items-center gap-x-4 gap-y-1 text-[0.8125rem]",
          )}
        >
          <span className="inline-flex items-center gap-1.5">
            <Square ok />
            OK{" "}
            <span className="text-foreground tabular-nums">
              {total - failed}
            </span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Square />
            Failed{" "}
            <span className="text-foreground tabular-nums">
              {failed}
            </span> ·{" "}
            {fmtShare(share)}
          </span>
          <span>
            Each square is {perSquare === 1 ? "1 call" : `${perSquare} calls`}
          </span>
        </div>
        <div
          role="img"
          aria-label={`Tool calls per ${unit}, ${rangeLabel(range)}. ${summary}`}
          className="bg-muted/30 flex items-end justify-between gap-1 overflow-x-auto rounded-md px-4 pt-6 pb-3"
        >
          {points.map((point, index) => (
            <BucketColumn
              key={point.bucketStart.toISOString()}
              point={point}
              perSquare={perSquare}
              label={index % labelEvery === 0}
              spike={point === worst}
            />
          ))}
        </div>
      </div>
      <p className={cn(MUTED, "border-t px-5 pt-2.5 pb-3.5 text-xs")}>
        The chart counts calls that reach the server directly. The Tool calls
        total above also counts calls reported by client hooks, so it can be
        higher. Failed means status 400 or above. A single failed call still
        fills a red square. Tool errors returned inside a 200 response count as
        OK.
      </p>
      <PromptBlock
        prompt={platformMcpPrompt({
          ...prompt,
          range: rangeLabel(range),
          worst:
            worst &&
            `${weekly ? "the week of " : ""}${fmtBucketDay(worst.bucketStart)}`,
        })}
      />
    </section>
  );
}

function Square({ ok = false }: { ok?: boolean }): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-block size-[9px] rounded-[2px]",
        ok ? "bg-[#8cc084]" : "bg-[#b8332b]",
      )}
    />
  );
}

function BucketColumn({
  point,
  perSquare,
  label,
  spike,
}: {
  point: AdminMcpServerHealthSeriesPoint;
  perSquare: number;
  label: boolean;
  spike: boolean;
}): JSX.Element {
  const squares = bucketSquares(point, perSquare);
  const day = fmtBucketDay(point.bucketStart);
  const share = point.total === 0 ? 0 : point.failed / point.total;
  return (
    <div
      title={`${day}: ${point.total - point.failed} OK, ${point.failed} failed (${fmtShare(share)})`}
      className="relative flex shrink-0 flex-col items-center gap-1.5"
    >
      {spike && (
        <span
          className={cn(
            MUTED,
            "absolute -top-[18px] text-[0.6875rem] whitespace-nowrap",
          )}
        >
          {point.failed} of {point.total}
        </span>
      )}
      {/* wrap-reverse fills from the bottom, so the red squares come first. */}
      <div className="flex w-5 flex-wrap-reverse gap-0.5">
        {Array.from({ length: squares.failed }, (_, k) => (
          <Square key={`f${k}`} />
        ))}
        {Array.from({ length: squares.ok }, (_, k) => (
          <Square key={`o${k}`} ok />
        ))}
      </div>
      <span
        className={cn(
          MUTED,
          "text-[0.6875rem] whitespace-nowrap tabular-nums",
          !label && "invisible",
        )}
      >
        {day}
      </span>
    </div>
  );
}

function PromptBlock({ prompt }: { prompt: string }): JSX.Element {
  // The prompt copied, not a flag, for the reason CopyValue gives: a new
  // window swaps the prompt under a mounted confirmation.
  const [copied, setCopied] = useState<string>();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useOnUnmount(() => clearTimeout(timer.current));

  return (
    <div className="bg-muted/30 flex flex-col gap-2.5 border-t px-5 pt-4 pb-4.5">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium">
          Investigate with the Platform MCP
        </span>
        <Button
          variant="outline"
          size="xs"
          onClick={() => {
            if (!navigator.clipboard?.writeText) return;
            void navigator.clipboard.writeText(prompt).then(
              () => {
                setCopied(prompt);
                clearTimeout(timer.current);
                timer.current = setTimeout(
                  () => setCopied(undefined),
                  COPY_CONFIRM_MS,
                );
              },
              () => undefined,
            );
          }}
        >
          {copied === prompt ? <CheckIcon /> : <CopyIcon />}
          {copied === prompt ? "Copied" : "Copy prompt"}
        </Button>
      </div>
      <p
        className={cn(
          MONO,
          "bg-card rounded-md border px-3.5 py-3 leading-relaxed whitespace-pre-wrap",
        )}
      >
        {prompt}
      </p>
    </div>
  );
}

function LoggingOff({ idOrSlug }: { idOrSlug: string }): JSX.Element {
  return (
    <section className={CARD} aria-labelledby="tool-call-outcomes">
      <div className={CARD_HEAD}>
        <h2 id="tool-call-outcomes" className={CARD_TITLE}>
          Tool call outcomes
        </h2>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-6 px-5 py-7">
        <div className="flex max-w-3xl flex-col gap-1.5">
          <span className="text-[0.9375rem] font-medium">
            Logging is off for this organization
          </span>
          <span className={cn(MUTED, "text-sm leading-normal")}>
            Tool calls are not recorded while the logs feature is off, so this
            server's outcomes are unknown. Treat it as unverified, not healthy.
            Turning logging on only records calls from that point.
          </span>
        </div>
        <Button asChild size="sm">
          <Link to="/organizations/$idOrSlug/features" params={{ idOrSlug }}>
            Review features
          </Link>
        </Button>
      </div>
    </section>
  );
}

function KeyValues({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <dl
      className={cn(
        "grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm",
        className,
      )}
    >
      {children}
    </dl>
  );
}

function KeyValue({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="contents">
      <dt className={MUTED}>{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

function None({ children = "None" }: { children?: string }): JSX.Element {
  return <span className={MUTED}>{children}</span>;
}

function IssuerPanel({
  issuer,
  idOrSlug,
  project,
  window,
}: {
  issuer: AdminMcpServerHealthUserSessionIssuer;
  idOrSlug: string;
  project: string;
  window: HealthWindow;
}): JSX.Element {
  const trusted = issuer.trustedRemoteSession;
  const others = issuer.otherServersUsingIssuer;
  return (
    <section className={CARD} aria-labelledby="user-session-issuer">
      <div className={CARD_HEAD}>
        <h2 id="user-session-issuer" className={CARD_TITLE}>
          User session issuer
        </h2>
        <span className={cn(MONO, MUTED)}>{issuer.slug}</span>
      </div>
      <KeyValues className="px-5 py-4">
        <KeyValue label="Classification">
          {issuer.classification === "project_default_idp"
            ? "Project default"
            : "Custom"}
        </KeyValue>
        <KeyValue label="Challenge mode">
          {CHALLENGE_MODE_LABELS[issuer.authnChallengeMode]}
        </KeyValue>
        <KeyValue label="Session duration">
          {durationLabel(issuer.sessionDurationHours)}
        </KeyValue>
        <KeyValue label="Scope">
          {SCOPE_LABELS[issuer.attachmentScope]}
        </KeyValue>
        <KeyValue label="CIMD admission">
          {admissionLabel(issuer.clientIdMetadataAdmissionMode)}
        </KeyValue>
        <KeyValue label="Authentication host">
          {issuer.useAuthenticationHost ? "Used" : "Not used"}
        </KeyValue>
        <KeyValue label="Trusted remote session">
          {trusted ? (
            <span className={MONO}>
              issuer {trusted.issuerId} · client {trusted.clientId}
            </span>
          ) : (
            <None />
          )}
        </KeyValue>
        <KeyValue label="Other servers on issuer">
          {others.length > 0 ? (
            <span className="flex flex-wrap gap-x-3">
              {others.map((other) => (
                <Link
                  key={other.id}
                  to={ROUTE}
                  params={{ idOrSlug, serverId: other.id }}
                  search={{ project, window }}
                  className="underline-offset-4 hover:underline"
                >
                  {other.name}
                </Link>
              ))}
            </span>
          ) : (
            <None />
          )}
        </KeyValue>
        <KeyValue label="First sign-in">
          {fmtDate(issuer.sessions.firstIssuedAt)}
        </KeyValue>
        <KeyValue label="Last token issued">
          {issuer.sessions.lastIssuedAt ? (
            <>
              {fmtDateTime(issuer.sessions.lastIssuedAt)}{" "}
              <span className={cn(MUTED, "text-xs")}>(includes refreshes)</span>
            </>
          ) : (
            <None>Never</None>
          )}
        </KeyValue>
      </KeyValues>
    </section>
  );
}

function LogLink({
  href,
  title,
  description,
  query,
}: {
  href: string;
  title: string;
  description: string;
  query: string;
}): JSX.Element {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="hover:bg-muted/50 flex items-center gap-4 border-t px-5 py-3.5 first-of-type:border-t-0"
    >
      <span className="flex min-w-0 grow flex-col gap-1">
        <span className="text-sm font-medium">{title}</span>
        <span className={cn(MUTED, "text-[0.8125rem]")}>{description}</span>
        <span className={cn(MONO, MUTED, "truncate")}>Datadog · {query}</span>
      </span>
      <ExternalLinkIcon
        aria-hidden="true"
        className={cn(MUTED, "size-4 shrink-0")}
      />
      <span className="sr-only">{LEAVES_THE_APP}</span>
    </a>
  );
}

function LogsCard({
  urlSlug,
  issuers,
  range,
  className,
}: {
  // Absent when the server has no slug: then there is no endpoint to tail,
  // and the login logs are found by issuer alone.
  urlSlug: string | undefined;
  issuers: string[];
  range: { from: Date; to: Date };
  className?: string;
}): JSX.Element {
  const loginQuery = loginChallengeQuery(urlSlug, issuers);
  return (
    <section className={cn(CARD, className)} aria-labelledby="logs">
      <div className={CARD_HEAD}>
        <h2 id="logs" className={CARD_TITLE}>
          Logs
        </h2>
        <span className={cn(MUTED, "text-xs")}>
          Opens filtered to this server
        </span>
      </div>
      <div>
        {urlSlug && (
          <LogLink
            href={toolCallTailUrl(urlSlug)}
            title="Tool call tail"
            description="Datadog live tail of every request to this server's MCP endpoint. Works with logging off."
            query={toolCallTailQuery(urlSlug)}
          />
        )}
        {loginQuery && (
          <LogLink
            href={loginChallengeUrl(loginQuery, range)}
            title="Login challenge logs"
            description="OAuth flow, issuer gate and token exchange logs for this server's sign-ins"
            query={loginQuery}
          />
        )}
        {!urlSlug && !loginQuery && (
          <p className={cn(MUTED, "px-5 py-3.5 text-sm")}>
            This server has no URL slug or upstream issuer to filter logs by.
          </p>
        )}
      </div>
    </section>
  );
}

const REGISTRATION_LABELS: Record<string, string> = {
  cimd: "CIMD",
  dcr: "DCR",
  static: "Static",
};

const PKCE_TONE: Record<string, string> = {
  supported: badgeTone.success,
  unsupported: badgeTone.warning,
  none: badgeTone.warning,
  uncaptured: badgeTone.neutral,
};

function ErrorAt({ at }: { at: Date | undefined }): JSX.Element {
  if (!at) return <None />;
  return (
    <Badge variant="outline" className={badgeTone.warning}>
      {fmtDateTime(at)}
    </Badge>
  );
}

function ColumnTitle({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span
      className={cn(MUTED, "text-xs font-semibold tracking-wide uppercase")}
    >
      {children}
    </span>
  );
}

const CLIENT_KV = "grid-cols-[minmax(0,8rem)_minmax(0,1fr)] text-[0.8125rem]";

function RemoteClient({
  client,
}: {
  client: AdminMcpServerHealthRemoteSessionClient;
}): JSX.Element {
  const { issuer, sessions } = client;
  const statuses = Object.entries(sessions.validationStatusCounts);
  return (
    <li className="grid border-t first:border-t-0 lg:grid-cols-3">
      <div className="flex flex-col gap-2.5 px-5 py-4 lg:border-r">
        <div className="flex items-center gap-2">
          <ColumnTitle>Client</ColumnTitle>
          <CopyValue label="remote session client id" value={client.id} />
        </div>
        <KeyValues className={CLIENT_KV}>
          <KeyValue label="Registration">
            {REGISTRATION_LABELS[client.registration]}
          </KeyValue>
          <KeyValue label="Auth method">
            {client.tokenEndpointAuthMethod ? (
              <span className={MONO}>{client.tokenEndpointAuthMethod}</span>
            ) : (
              <None />
            )}
          </KeyValue>
          <KeyValue label="Scopes">
            {client.scope.length > 0 ? (
              <span className={MONO}>{client.scope.join(" ")}</span>
            ) : (
              <None />
            )}
          </KeyValue>
          <KeyValue label="Grant types">
            {client.grantTypes.length > 0 ? (
              <span className={MONO}>{client.grantTypes.join(" ")}</span>
            ) : (
              <None />
            )}
          </KeyValue>
          <KeyValue label="Scope">
            {SCOPE_LABELS[client.attachmentScope]}
          </KeyValue>
          <KeyValue label="Upstream rejected">
            {client.upstreamRejectedAt ? (
              <ErrorAt at={client.upstreamRejectedAt} />
            ) : (
              <None>Never</None>
            )}
          </KeyValue>
        </KeyValues>
      </div>
      <div className="flex flex-col gap-2.5 border-t px-5 py-4 lg:border-t-0 lg:border-r">
        <div className="flex flex-wrap items-center gap-2">
          <ArrowRightIcon
            aria-hidden="true"
            className={cn(MUTED, "size-3.5")}
          />
          <ColumnTitle>Issuer</ColumnTitle>
          <span className="text-sm font-medium">
            {issuer.name ?? issuer.slug}
          </span>
          <Badge variant="outline" className={badgeTone.neutral}>
            {SCOPE_LABELS[issuer.attachmentScope]}
          </Badge>
          <Badge variant="outline" className={badgeTone.neutral}>
            {issuer.networking}
          </Badge>
        </div>
        <KeyValues className={CLIENT_KV}>
          <KeyValue label="Scope">
            {issuer.attachmentScope === "global" ? (
              <>
                Platform{" "}
                <span className={cn(MUTED, "text-xs")}>
                  (shared by every organization)
                </span>
              </>
            ) : (
              SCOPE_LABELS[issuer.attachmentScope]
            )}
          </KeyValue>
          <KeyValue label="Issuer URL">
            <span className={MONO}>{issuer.issuer}</span>
          </KeyValue>
          <KeyValue label="OIDC">{issuer.oidc ? "Yes" : "No"}</KeyValue>
          <KeyValue label="Passthrough">
            {issuer.passthrough ? "Yes" : "No"}
          </KeyValue>
          <KeyValue label="PKCE">
            <Badge variant="outline" className={PKCE_TONE[issuer.pkce]}>
              {issuer.pkce}
            </Badge>
          </KeyValue>
          <KeyValue label="CIMD">
            {issuer.cimdSupported ? "Supported" : "Not supported"}
          </KeyValue>
          <KeyValue label="Scope override">
            {issuer.scopeOverride && issuer.scopeOverride.length > 0 ? (
              <span className={MONO}>{issuer.scopeOverride.join(" ")}</span>
            ) : (
              <None />
            )}
          </KeyValue>
          <KeyValue label="Metadata fetched">
            {fmtDateTime(issuer.metadataFetchedAt)}
          </KeyValue>
          <KeyValue label="Metadata error">
            <ErrorAt at={issuer.metadataLastErrorAt} />
          </KeyValue>
          <KeyValue label="JWKS error">
            <ErrorAt at={issuer.jwksLastErrorAt} />
          </KeyValue>
        </KeyValues>
      </div>
      <div className="flex flex-col gap-2.5 border-t px-5 py-4 lg:border-t-0">
        <ColumnTitle>Linked accounts</ColumnTitle>
        <KeyValues className={CLIENT_KV}>
          <KeyValue label="Linked people">{sessions.linkedSubjects}</KeyValue>
          <KeyValue label="Reauthorizations">
            {sessions.reauthorizations}
          </KeyValue>
          <KeyValue label="First linked">
            {fmtDate(sessions.firstLinkedAt)}
          </KeyValue>
          <KeyValue label="Validation">
            {statuses.length > 0 ? (
              <span className="flex flex-wrap gap-1.5">
                {statuses.map(([status, count]) => (
                  <Badge
                    key={status}
                    variant="outline"
                    className={
                      status === "valid" ? badgeTone.success : badgeTone.warning
                    }
                  >
                    {count} {humanize(status).toLowerCase()}
                  </Badge>
                ))}
              </span>
            ) : (
              <None>Not validated yet</None>
            )}
          </KeyValue>
        </KeyValues>
      </div>
    </li>
  );
}

function RemoteClients({
  clients,
}: {
  clients: AdminMcpServerHealthRemoteSessionClient[];
}): JSX.Element {
  return (
    <section className={CARD} aria-labelledby="remote-session-clients">
      <div className={CARD_HEAD}>
        <h2 id="remote-session-clients" className={CARD_TITLE}>
          Remote session clients
        </h2>
        <span className={cn(MUTED, "text-xs")}>
          Each client and the upstream issuer it signs in to
        </span>
      </div>
      <ul>
        {clients.map((client) => (
          <RemoteClient key={client.id} client={client} />
        ))}
      </ul>
    </section>
  );
}
