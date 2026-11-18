import { MetricCard } from "@/components/ui/MetricCard";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { authorizationServerModeLabel } from "@/pages/user-session-issuers/issuerDisplay";
import { splitIntoUnit } from "@/pages/mcp/x/tabs/settings/sections/authentication/sessionSettings";
import { useOrgRoutes, useRoutes } from "@/routes";
import { scopeLabel } from "@/components/killswitch/killswitch-view-model";
import { useSession } from "@/contexts/Auth";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { clientAccessLabel } from "../providers";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { ArrowRight, Pencil } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import {
  connectionHealthNow,
  useActiveKillswitches,
  useConnectionAttempts,
  useDeniedClients,
  useSessionAverage,
  useSuspectedProblems,
} from "./api";
import { AttemptDetailSheet } from "./AttemptDetailSheet";
import { AttemptsTable } from "./AttemptsTable";
import { OwnAttemptsNotice } from "./ConnectionLogsView";
import {
  ATTEMPT_PARAM,
  logsSearch,
  presetWindow,
  PROBLEM_PARAM,
  type TimeRangePreset,
} from "./filters";
import {
  appKeyLabel,
  formatDayMonth,
  formatLast,
  formatSessionLength,
  plural,
} from "./format";
import { ProblemDetailSheet } from "./ProblemDetailSheet";
import { SummaryPanel, type SummaryMetric } from "./SummaryPanel";
import { SuspectedProblems } from "./SuspectedProblems";
import {
  SIGN_IN_STEPS,
  type ActiveKillswitch,
  type AttemptFilters,
  type DeniedClient,
} from "./types";

const RANGES: { value: TimeRangePreset; label: string; days: number }[] = [
  { value: "1d", label: "Last 24 hours", days: 1 },
  { value: "7d", label: "Last 7 days", days: 7 },
  { value: "30d", label: "Last 30 days", days: 30 },
];
const DEFAULT_RANGE: TimeRangePreset = "7d";
const RANGE_PARAM = "range";
const CHART_PARAM = "chart";

/** The client-admission refusals the API lists as denied clients. */
const DENIED_CLIENT_REASONS = [
  "cimd_admission_denied",
  "ai_tool_blocked",
  "client_not_cimd",
] as const;

const SIGN_IN_ACTIVITY_FILTERS: AttemptFilters = {
  steps: [...SIGN_IN_STEPS],
};

/** MCP Sessions › Overview: how sign-in through this provider is going. */
export function OverviewTab({
  provider,
}: {
  provider: UserSessionIssuer;
}): JSX.Element {
  const routes = useRoutes();
  const [searchParams, setSearchParams] = useSearchParams();
  const [now] = useState(connectionHealthNow);
  const rangeParam = searchParams.get(RANGE_PARAM);
  const range =
    RANGES.find((r) => r.value === rangeParam) ??
    RANGES.find((r) => r.value === DEFAULT_RANGE)!;
  const metric: SummaryMetric =
    searchParams.get(CHART_PARAM) === "sessions" ? "sessions" : "sign-ins";
  const window = useMemo(
    () => presetWindow(range.value, now),
    [range.value, now],
  );
  const providerId = provider.id;
  const scope = useMemo(() => ({ providerId }), [providerId]);

  const setParam = (key: string, value: string | null) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      // Opening a sheet is a step Back undoes; closing one is not.
      { replace: !value || (key !== ATTEMPT_PARAM && key !== PROBLEM_PARAM) },
    );
  };

  const problems = useSuspectedProblems(scope);
  const killswitches = useActiveKillswitches();
  const denied = useDeniedClients(scope, window);
  const activity = useConnectionAttempts(
    scope,
    presetWindow("30d", now),
    SIGN_IN_ACTIVITY_FILTERS,
    { limit: 5 },
  );

  const logsHref = (search: string) =>
    `${routes.mcpSessions.provider.connectionLogs.href(providerId)}${search}`;
  const deniedClients = denied.data?.clients ?? [];
  const activeKillswitches = killswitches.killswitches;
  const activityPage = activity.data?.pages[0];
  const activityRows = activityPage?.attempts ?? [];

  return (
    <div className="space-y-10 pb-8">
      {activityPage?.loggingEnabled === false ? null : problems.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        <SuspectedProblems
          problems={problems.data?.problems ?? []}
          now={now}
          onOpen={(problem) => setParam(PROBLEM_PARAM, problem.id)}
        />
      )}

      {activityPage?.viewerScope === "own" ? <OwnAttemptsNotice /> : null}

      <SummaryPanel
        providerId={providerId}
        window={window}
        days={range.days}
        metric={metric}
        onMetricChange={(next) =>
          setParam(CHART_PARAM, next === "sessions" ? "sessions" : null)
        }
        rangeControl={
          <Select
            value={range.value}
            onValueChange={(value) =>
              setParam(RANGE_PARAM, value === DEFAULT_RANGE ? null : value)
            }
          >
            <SelectTrigger className="w-40" aria-label="Summary time range">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGES.map((r) => (
                <SelectItem key={r.value} value={r.value}>
                  {r.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
        logsHref={logsHref(logsSearch({ range: range.value }, now))}
        sessionsHref={routes.mcpSessions.provider.connections.href(providerId)}
      />

      {activeKillswitches.length > 0 || deniedClients.length > 0 ? (
        <div className="grid gap-6 lg:grid-cols-2 [&>*:only-child]:lg:col-span-2">
          {activeKillswitches.length > 0 ? (
            <KillswitchesInForce killswitches={activeKillswitches} />
          ) : null}
          {deniedClients.length > 0 ? (
            <DeniedClients
              clients={deniedClients}
              now={now}
              rangeLabel={range.label.toLowerCase()}
              href={logsHref(
                logsSearch(
                  {
                    range: range.value,
                    outcomes: ["blocked"],
                    reasons: [...DENIED_CLIENT_REASONS],
                  },
                  now,
                ),
              )}
            />
          ) : null}
        </div>
      ) : null}

      <OverviewSection
        title="Sign-in activity"
        description="Latest first. Sign-ins and renewals through this provider."
        action={
          <ArrowLink
            to={logsHref(
              logsSearch({ steps: [...SIGN_IN_STEPS], range: "30d" }, now),
            )}
          >
            Open in Connection Logs
          </ArrowLink>
        }
      >
        {activity.isPending ? (
          <Skeleton className="h-64 w-full" />
        ) : activityPage && !activityPage.loggingEnabled ? (
          <p className="text-muted-foreground border px-4 py-6 text-sm">
            Sign-in activity needs logging, which is off for this organization.
          </p>
        ) : activity.isError ? (
          <p className="text-muted-foreground border px-4 py-6 text-sm">
            Couldn&apos;t load sign-in activity.
          </p>
        ) : activityRows.length === 0 ? (
          <p className="text-muted-foreground border px-4 py-6 text-sm">
            No sign-ins or renewals in the last 30 days.
          </p>
        ) : (
          <AttemptsTable
            rows={activityRows}
            now={now}
            onOpen={(row) => setParam(ATTEMPT_PARAM, row.id)}
          />
        )}
      </OverviewSection>

      <Configuration
        provider={provider}
        settingsHref={routes.mcpSessions.provider.settings.href(providerId)}
      />

      <AttemptDetailSheet
        reference={searchParams.get(ATTEMPT_PARAM)}
        providerId={providerId}
        providerName={provider.slug}
        onClose={() => setParam(ATTEMPT_PARAM, null)}
      />
      <ProblemDetailSheet
        id={searchParams.get(PROBLEM_PARAM)}
        providerId={providerId}
        now={now}
        onClose={() => setParam(PROBLEM_PARAM, null)}
      />
    </div>
  );
}

function OverviewSection({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description: string;
  action?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="space-y-3">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h2 className="text-eyebrow">{title}</h2>
          <p className="text-muted-foreground mt-1 text-sm">{description}</p>
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function ArrowLink({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Link
      to={to}
      className="inline-flex shrink-0 items-center gap-1 text-sm hover:underline"
    >
      {children}
      <ArrowRight className="size-3.5" aria-hidden />
    </Link>
  );
}

function KillswitchesInForce({
  killswitches,
}: {
  killswitches: ActiveKillswitch[];
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const session = useSession();
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const servers = useKillswitchMCPServers(
    { sessionHeaderGramSession: session.session },
    { gramSession: session.session },
    { throwOnError: false },
  );
  const columns = useMemo<Column<ActiveKillswitch>[]>(() => {
    const names = new Map(
      (members.data?.members ?? []).map((m) => [m.id, m.name || m.email]),
    );
    const serverNames = new Map(
      (servers.data?.servers ?? []).map((s) => [s.id, s.name]),
    );
    return [
      {
        key: "who",
        header: "Who",
        width: "1.4fr",
        render: (k) => names.get(k.userId) ?? "Former member",
      },
      {
        key: "servers",
        header: "Servers",
        width: "1fr",
        render: (k) => scopeLabel(k.scope, serverNames),
      },
      {
        key: "until",
        header: "Until",
        width: "1fr",
        render: (k) => (k.until ? formatDayMonth(k.until) : "No end date"),
      },
    ];
  }, [members.data, servers.data]);
  return (
    <OverviewSection
      title={`Killswitches in force · ${killswitches.length}`}
      description="Across this organization, not only this provider. They block tool calls right now."
      action={<ArrowLink to={orgRoutes.killswitch.href()}>View all</ArrowLink>}
    >
      <Table columns={columns} data={killswitches} rowKey={(k) => k.id} />
    </OverviewSection>
  );
}

function DeniedClients({
  clients,
  now,
  rangeLabel,
  href,
}: {
  clients: DeniedClient[];
  now: Date;
  rangeLabel: string;
  href: string;
}): JSX.Element {
  const columns = useMemo<Column<DeniedClient>[]>(
    () => [
      {
        key: "client",
        header: "Client",
        width: "1.2fr",
        render: (c) => (
          <div>
            <div>
              {c.app.name}
              {!c.app.family && appKeyLabel(c.app) ? (
                <span className="text-muted-foreground">
                  {" "}
                  · {appKeyLabel(c.app)}
                </span>
              ) : null}
            </div>
            <div className="text-muted-foreground text-xs">
              {plural(c.attempts, "time")}
              {c.networks > 0 ? `, from ${plural(c.networks, "network")}` : ""}
            </div>
          </div>
        ),
      },
      {
        key: "reason",
        header: "Reason",
        width: "1.4fr",
        render: (c) => (
          <div>
            <div>{c.message}</div>
            {c.technicalMessage ? (
              <div className="text-muted-foreground text-xs">
                {c.technicalMessage}
              </div>
            ) : null}
          </div>
        ),
      },
      {
        key: "last",
        header: "Last",
        width: "120px",
        render: (c) => formatLast(c.lastAt, now),
      },
    ],
    [now],
  );
  return (
    <OverviewSection
      title={`Denied clients · ${clients.length}`}
      description={`Refused in the ${rangeLabel}.`}
      action={<ArrowLink to={href}>Open in Connection Logs</ArrowLink>}
    >
      <Table
        columns={columns}
        data={clients}
        rowKey={(c) => `${c.app.family}|${c.app.appKey ?? ""}|${c.reason}`}
      />
    </OverviewSection>
  );
}

function Configuration({
  provider,
  settingsHref,
}: {
  provider: UserSessionIssuer;
  settingsHref: string;
}): JSX.Element {
  const duration = splitIntoUnit(provider.sessionDurationHours);
  const average = useSessionAverage(provider.id).data;
  const facts: [string, string, string?][] = [
    [
      "Session duration",
      `${duration.number} ${duration.unit}${duration.number === 1 ? "" : "s"}`,
      average === undefined
        ? undefined
        : `Sessions averaged ${formatSessionLength(average)} in the last 30 days.`,
    ],
    [
      "Client access",
      clientAccessLabel(provider.clientIdMetadataAdmissionMode),
    ],
    ["Mode", authorizationServerModeLabel(provider)],
    [
      "Enterprise Managed Auth",
      provider.trustedRemoteSessionClientId ? "On" : "Off",
    ],
  ];
  return (
    <OverviewSection
      title="Configuration"
      description="How people sign in through this provider."
      action={
        <Link
          to={settingsHref}
          className="inline-flex items-center gap-1 text-sm hover:underline"
        >
          <Pencil className="size-3.5" aria-hidden />
          Edit in Settings
        </Link>
      }
    >
      <MetricCard.Group>
        {facts.map(([label, value, description]) => (
          <MetricCard
            key={label}
            label={label}
            value={value}
            description={description}
            tone="neutral"
            size="xs"
          />
        ))}
      </MetricCard.Group>
    </OverviewSection>
  );
}
