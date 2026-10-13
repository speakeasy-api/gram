import { useRef, useState, type JSX, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
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
import type { AdminMcpServerToolCallBucket } from "@gram/admin-client/models/components/adminmcpservertoolcallbucket";
import type { AdminMcpServerToolCalls } from "@gram/admin-client/models/components/adminmcpservertoolcalls";
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
import type { AdminOrganization } from "@/lib/gramAdminApi";
import {
  mcpServerHealthQuery,
  mcpServerToolCallsQuery,
} from "@/lib/gramAdminClient";
import { LEAVES_THE_APP } from "@/lib/impersonation";
import { cn } from "@/lib/utils";

import { SOURCE_LABELS, VISIBILITY_LABELS } from "./mcpServerLabels";
import {
  adminMcpPrompt,
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
// A dot grid behind the chart, drawn in the muted foreground so it holds in
// both schemes.
const PLOT =
  "bg-card bg-[radial-gradient(color-mix(in_oklab,var(--muted-foreground)_35%,transparent)_1px,transparent_1.2px)] [background-size:12px_12px]";

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
  const request = {
    organizationId: org.id,
    projectId: project ?? "",
    mcpServerId: serverId,
    windowDays: window,
  };
  // Two reads, so telemetry that fails or runs slow never holds back the
  // configuration. Both take the window: the configuration counts the people
  // who signed in inside it. A new window keeps the last answers on screen
  // until it lands, rather than blanking the page between picks.
  // Only another window of this same server stands in while a read lands.
  // Moving to a different server, say through "Other servers on issuer",
  // must never show the last server's report under the new one's name.
  const sameServer = (previousKey: readonly unknown[] | undefined): boolean =>
    previousKey?.[1] === idOrSlug &&
    previousKey[2] === project &&
    previousKey[3] === serverId;
  const health = useQuery({
    ...mcpServerHealthQuery(idOrSlug, request),
    enabled: !!project,
    placeholderData: (previous, previousQuery) =>
      sameServer(previousQuery?.queryKey) ? previous : undefined,
  });
  const toolCalls = useQuery({
    ...mcpServerToolCallsQuery(idOrSlug, request),
    enabled: !!project,
    placeholderData: (previous, previousQuery) =>
      sameServer(previousQuery?.queryKey) ? previous : undefined,
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

  // Fixed copy, never the error's own text: a server error can carry detail
  // this page must not repeat.
  if (!health.data) {
    return health.isError ? (
      <LoadFailed what="Server health" onRetry={() => void health.refetch()} />
    ) : (
      <span className={cn(MUTED, "text-sm")}>Loading...</span>
    );
  }

  const projectName =
    projects.data?.projects.find((p) => p.id === project)?.name ?? project;

  let toolCallsState: ToolCallsState = { status: "loading" };
  if (toolCalls.data) {
    toolCallsState = { status: "ready", toolCalls: toolCalls.data };
  } else if (toolCalls.isError) {
    toolCallsState = {
      status: "error",
      // Telemetry alone: the configuration on screen is fine.
      retry: () => void toolCalls.refetch(),
    };
  }

  return (
    <HealthReport
      health={health.data}
      toolCalls={toolCallsState}
      idOrSlug={idOrSlug}
      project={project}
      projectName={projectName}
      window={window}
      // The report is as of its own fetch, so links and ranges built from it
      // do not drift on every render. While another window's answer stands in
      // for this one, the read has no time of its own yet (it reports 0), and
      // "now" is what the new window is measured back from.
      asOf={new Date(health.dataUpdatedAt || Date.now())}
      onWindowChange={(next) => {
        void navigate({
          search: (prev) => ({ ...prev, window: next }),
          replace: true,
        });
      }}
    />
  );
}

type ToolCallsState =
  | { status: "loading" }
  | { status: "error"; retry: () => void }
  | { status: "ready"; toolCalls: AdminMcpServerToolCalls };

function HealthReport({
  health,
  toolCalls,
  idOrSlug,
  project,
  projectName,
  window,
  asOf,
  onWindowChange,
}: {
  health: AdminMcpServerHealth;
  toolCalls: ToolCallsState;
  idOrSlug: string;
  project: string;
  projectName: string;
  window: HealthWindow;
  asOf: Date;
  onWindowChange: (window: HealthWindow) => void;
}): JSX.Element {
  const { server, userSessionIssuer: issuer } = health;
  const loggingOff =
    toolCalls.status === "ready" &&
    toolCalls.toolCalls.type === "logging:disabled";
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

      {loggingOff ? (
        <LoggingOff idOrSlug={idOrSlug} />
      ) : (
        <ToolCallsChart
          state={toolCalls}
          window={window}
          range={range}
          prompt={{
            serverName: server.name,
            serverId: server.id,
            projectName,
          }}
        />
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
