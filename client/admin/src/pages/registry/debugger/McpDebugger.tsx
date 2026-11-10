import { useMemo, useState, type JSX, type ReactNode } from "react";
import {
  CheckIcon,
  CircleDashedIcon,
  ClockIcon,
  CopyIcon,
  ExternalLinkIcon,
  FlagIcon,
  Loader2Icon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react";
import { toast } from "sonner";
import type { AdminMcpDebugReport } from "@gram/admin-client/models/components/adminmcpdebugreport";
import type { AdminMcpDebugStep } from "@gram/admin-client/models/components/adminmcpdebugstep";
import type { AdminMcpDebugTool } from "@gram/admin-client/models/components/adminmcpdebugtool";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { badgeTone } from "@/lib/badgeTone";
import { tone } from "@/lib/tone";
import { cn } from "@/lib/utils";

type StepStatus = "passed" | "warning" | "failed" | "skipped" | "running";

/** Step titles shown while the trace runs; the server answers with its own. */
const PENDING_TITLES = [
  "Reach server",
  "Parse authentication challenge",
  "Fetch protected resource metadata",
  "Fetch authorization server metadata",
  "Detect authentication method",
  "Initialize MCP",
  "List tools, prompts and resources",
];

const OVERVIEW = "__overview__";
const SLOW_STEP_MS = 1000;

const HEADLINE: Record<string, string> = {
  mcp_available: "MCP server ready",
  authentication_required: "Sign-in required",
  invalid_mcp_response: "Not an MCP server",
  unreachable: "Server unreachable",
};

const AUTH_METHOD: Record<string, string> = {
  none: "No sign-in needed: any client can connect.",
  client_id_metadata_document:
    "Clients sign in with OAuth and identify themselves with a client metadata document.",
  dynamic_client_registration:
    "Clients sign in with OAuth and register themselves automatically.",
  pre_registered:
    "Clients sign in with OAuth, but need a client ID and secret from the provider first.",
  non_oauth:
    "The server expects a credential such as an API key, sent as a header.",
};

function formatDuration(ms: number | undefined): string | undefined {
  if (ms === undefined) return undefined;
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isReadOnly(tool: AdminMcpDebugTool): boolean {
  return isRecord(tool.annotations) && tool.annotations.readOnlyHint === true;
}

function statusOf(step: AdminMcpDebugStep): StepStatus {
  switch (step.status) {
    case "passed":
    case "warning":
    case "failed":
    case "skipped":
      return step.status;
    default:
      // A status this build does not know yet reads as a warning rather than
      // silently passing.
      return "warning";
  }
}

/** How to give a local stack a public HTTPS address, so CIMD sign-in works. */
const PUBLIC_ADDRESS_RUNBOOK_URL =
  "https://github.com/speakeasy-api/gram/blob/main/docs/runbooks/oauth-debugging.md";

/**
 * The step that warns a provider can't fetch Speakeasy's client metadata
 * document, because this deployment's address isn't reachable from the
 * internet. The runner records that address only in this case.
 */
function needsPublicAddress(step: AdminMcpDebugStep | undefined): boolean {
  return (
    !!step &&
    step.status === "warning" &&
    step.details?.["speakeasy_client_metadata_origin"] !== undefined
  );
}

function PublicAddressRunbookLink(): JSX.Element {
  return (
    <a
      href={PUBLIC_ADDRESS_RUNBOOK_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 text-sm underline-offset-2 hover:underline"
    >
      Set up a public address to test CIMD sign-in locally
      <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
    </a>
  );
}

export interface McpDebuggerProps {
  url: string;
  onUrlChange: (url: string) => void;
  onRun: () => void;
  canRun: boolean;
  isRunning: boolean;
  report: AdminMcpDebugReport | undefined;
  /** Shown in place of a report when the request itself failed. */
  error: string | undefined;
  /** Rendered between the URL bar and the trace, e.g. a remote picker. */
  urlHint?: ReactNode;
  /**
   * The next step once a trace reaches the server, e.g. a link to sign in and
   * run its tools. Shown in the report header.
   */
  verifyAction?: ReactNode;
}

/**
 * Step-by-step trace of how a client reaches an MCP server, then what the
 * server offers. Steps on the left; the selected step or the report on the
 * right. Read-only: staff see a server's tools, never call them.
 */
export function McpDebugger(props: McpDebuggerProps): JSX.Element {
  const { report, isRunning, error } = props;
  const [selected, setSelected] = useState(OVERVIEW);
  const hasRun = isRunning || !!report || !!error;
  const steps = report?.steps ?? [];
  const selectedStep = report
    ? steps.find((step) => step.id === selected)
    : undefined;

  return (
    <div className="bg-card flex min-h-[560px] flex-col rounded-lg border">
      <Toolbar {...props} hasRun={hasRun} />
      {props.urlHint}
      {!hasRun ? (
        <div className="text-muted-foreground flex flex-1 items-center justify-center p-6 text-center text-sm">
          Enter an MCP server URL and run the trace. It runs from Speakeasy's
          network, sends no credentials, and saves nothing.
        </div>
      ) : (
        <div className="grid flex-1 grid-cols-1 md:grid-cols-[minmax(240px,300px)_1fr]">
          <nav
            aria-label="Debug steps"
            className="flex flex-col gap-0.5 border-b p-3 md:border-r md:border-b-0"
          >
            {isRunning || !report
              ? PENDING_TITLES.map((title, index) => (
                  <TimelineRow
                    key={title}
                    label={title}
                    status={
                      isRunning
                        ? index === 0
                          ? "running"
                          : "skipped"
                        : "skipped"
                    }
                    selected={false}
                    onClick={() => undefined}
                  />
                ))
              : steps.map((step) => (
                  <TimelineRow
                    key={step.id}
                    label={step.title}
                    status={statusOf(step)}
                    durationMs={step.durationMs}
                    selected={selected === step.id}
                    onClick={() => setSelected(step.id)}
                  />
                ))}
            <div className="mt-2 border-t pt-2">
              <button
                type="button"
                onClick={() => setSelected(OVERVIEW)}
                aria-current={!selectedStep ? "step" : undefined}
                className={cn(
                  "hover:bg-muted flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm",
                  !selectedStep && "bg-muted",
                )}
              >
                {isRunning ? (
                  <Loader2Icon className="text-muted-foreground size-4 animate-spin" />
                ) : (
                  <FlagIcon className="size-4" />
                )}
                Report overview
              </button>
            </div>
          </nav>
          <div className="min-w-0 p-6">
            {selectedStep ? (
              <StepDetail step={selectedStep} />
            ) : (
              <Overview
                report={report}
                error={error}
                isRunning={isRunning}
                onSelectStep={setSelected}
                verifyAction={props.verifyAction}
              />
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Toolbar({
  url,
  onUrlChange,
  onRun,
  canRun,
  isRunning,
  report,
  hasRun,
}: McpDebuggerProps & { hasRun: boolean }): JSX.Element {
  const copy = () => {
    if (!report) return;
    void navigator.clipboard
      .writeText(JSON.stringify(report, null, 2))
      .then(() => toast.success("Trace copied"));
  };
  const outcome = report?.outcome;

  return (
    <div className="flex flex-col gap-3 border-b p-4">
      {hasRun ? (
        <div className="flex min-h-8 items-center gap-2">
          {isRunning || !report ? (
            <Badge variant="outline" className={badgeTone.neutral}>
              {isRunning ? "Running" : "Failed"}
            </Badge>
          ) : (
            <Badge
              variant="outline"
              className={
                outcome === "mcp_available"
                  ? badgeTone.success
                  : outcome === "authentication_required"
                    ? badgeTone.warning
                    : badgeTone.destructive
              }
            >
              {outcome === "mcp_available"
                ? "Completed"
                : outcome === "authentication_required"
                  ? "Sign-in required"
                  : "Stopped"}
            </Badge>
          )}
          {!isRunning && report ? (
            <DurationChip ms={report.durationMs} />
          ) : null}
          <div className="flex-1" />
          {!isRunning && report ? (
            <Button variant="outline" size="sm" onClick={copy}>
              Copy as JSON
              <CopyIcon />
            </Button>
          ) : null}
        </div>
      ) : null}
      <form
        className="flex items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (canRun && !isRunning) onRun();
        }}
      >
        <Input
          aria-label="MCP server URL"
          placeholder="https://mcp.example.com/mcp"
          className="font-mono"
          value={url}
          onChange={(event) => onUrlChange(event.target.value)}
        />
        <Button
          type="submit"
          variant={hasRun ? "outline" : "default"}
          disabled={!canRun || isRunning}
        >
          {hasRun ? "Run again" : "Run"}
        </Button>
      </form>
    </div>
  );
}

function DurationChip({
  ms,
  slow,
}: {
  ms: number;
  slow?: boolean;
}): JSX.Element {
  return (
    <span
      className={cn(
        "text-muted-foreground inline-flex items-center gap-1 font-mono text-xs",
        slow && "font-semibold",
      )}
      title={slow ? "Slow step" : undefined}
    >
      <ClockIcon className="size-3" aria-hidden />
      {formatDuration(ms)}
    </span>
  );
}

const STATUS_LABEL: Record<StepStatus, string> = {
  passed: "Passed",
  warning: "Warning",
  failed: "Failed",
  skipped: "Skipped",
  running: "Running",
};

function StatusMark({ status }: { status: StepStatus }): JSX.Element {
  const label = STATUS_LABEL[status];
  if (status === "running") {
    return (
      <Loader2Icon
        aria-label={label}
        className="text-muted-foreground size-4 shrink-0 animate-spin"
      />
    );
  }
  if (status === "skipped") {
    return (
      <CircleDashedIcon
        aria-label={label}
        className="text-muted-foreground/60 size-4 shrink-0"
      />
    );
  }
  const Icon =
    status === "passed"
      ? CheckIcon
      : status === "warning"
        ? TriangleAlertIcon
        : XIcon;
  return (
    <span
      role="img"
      aria-label={label}
      className={cn(
        "inline-flex size-4 shrink-0 items-center justify-center rounded-full border",
        status === "passed"
          ? tone.success
          : status === "warning"
            ? tone.warning
            : tone.destructive,
      )}
    >
      <Icon className="size-2.5" aria-hidden />
    </span>
  );
}

function TimelineRow({
  label,
  status,
  durationMs,
  selected,
  onClick,
}: {
  label: string;
  status: StepStatus;
  durationMs?: number;
  selected: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={selected ? "step" : undefined}
      className={cn(
        "hover:bg-muted flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm",
        selected && "bg-muted",
        status === "skipped" && "text-muted-foreground",
      )}
    >
      <StatusMark status={status} />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {durationMs !== undefined ? (
        <DurationChip ms={durationMs} slow={durationMs >= SLOW_STEP_MS} />
      ) : null}
    </button>
  );
}

function JsonBlock({
  value,
  label,
}: {
  value: unknown;
  label: string;
}): JSX.Element {
  const text = useMemo(() => JSON.stringify(value, null, 2), [value]);
  return (
    <div className="relative">
      <pre className="bg-muted/50 max-h-[420px] overflow-auto rounded-md border p-3 font-mono text-xs leading-relaxed">
        {text}
      </pre>
      <Button
        variant="ghost"
        size="icon"
        className="absolute top-1.5 right-1.5 size-7"
        aria-label={`Copy ${label}`}
        onClick={() => void navigator.clipboard.writeText(text)}
      >
        <CopyIcon />
      </Button>
    </div>
  );
}

function StepDetail({ step }: { step: AdminMcpDebugStep }): JSX.Element {
  const status = statusOf(step);
  const hasDetails = !!step.details && Object.keys(step.details).length > 0;
  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <div className="pt-1">
          <StatusMark status={status} />
        </div>
        <div className="min-w-0 space-y-1">
          <h2 className="text-lg font-medium">{step.title}</h2>
          <p className="text-muted-foreground text-sm break-words whitespace-pre-line">
            {step.summary}
          </p>
        </div>
      </div>
      {step.requestUrl ||
      step.httpStatus !== undefined ||
      step.durationMs !== undefined ? (
        <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm">
          {step.requestUrl ? (
            <>
              <dt className="text-muted-foreground">Request</dt>
              <dd className="font-mono break-all">{step.requestUrl}</dd>
            </>
          ) : null}
          {step.httpStatus !== undefined ? (
            <>
              <dt className="text-muted-foreground">HTTP status</dt>
              <dd className="font-mono">{step.httpStatus}</dd>
            </>
          ) : null}
          {step.durationMs !== undefined ? (
            <>
              <dt className="text-muted-foreground">Duration</dt>
              <dd className="font-mono">{formatDuration(step.durationMs)}</dd>
            </>
          ) : null}
        </dl>
      ) : null}
      {needsPublicAddress(step) ? <PublicAddressRunbookLink /> : null}
      {hasDetails ? (
        <section className="space-y-2">
          <h3 className="text-sm font-medium">What the server returned</h3>
          <JsonBlock value={step.details} label="step details" />
        </section>
      ) : null}
    </div>
  );
}

type ReportTab = "tools" | "prompts" | "resources" | "instructions";

function Overview({
  report,
  error,
  isRunning,
  onSelectStep,
  verifyAction,
}: {
  report: AdminMcpDebugReport | undefined;
  error: string | undefined;
  isRunning: boolean;
  onSelectStep: (id: string) => void;
  verifyAction?: ReactNode;
}): JSX.Element {
  const [tab, setTab] = useState<ReportTab>("tools");

  if (isRunning) {
    return (
      <div className="space-y-4" aria-busy="true">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="h-4 w-96 max-w-full" />
        <Skeleton className="mt-4 h-64 w-full" />
      </div>
    );
  }
  if (!report) {
    return (
      <div role="alert" className="space-y-1">
        <h2 className="text-lg font-medium">The trace didn't run</h2>
        <p className="text-muted-foreground text-sm break-words">{error}</p>
      </div>
    );
  }

  const ran = report.steps.filter((step) => step.status !== "skipped");
  const passed = ran.filter((step) => step.status === "passed").length;
  const failed = report.steps.find((step) => step.status === "failed");
  const warning = failed
    ? undefined
    : report.steps.find((step) => step.status === "warning");
  const callout = failed ?? warning;
  const listed = report.outcome === "mcp_available";
  const tools = report.tools ?? [];
  const prompts = report.prompts ?? [];
  const resources = report.resources ?? [];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 className="text-lg font-medium">
            {HEADLINE[report.outcome] ?? "Trace finished"}
            {report.serverName ? (
              <span className="text-muted-foreground font-normal">
                {" "}
                · {report.serverName}
                {report.serverVersion ? ` ${report.serverVersion}` : ""}
              </span>
            ) : null}
          </h2>
          {AUTH_METHOD[report.authMethod] ? (
            <p className="text-muted-foreground text-sm">
              {AUTH_METHOD[report.authMethod]}
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {report.protocolVersion ? (
            <Badge variant="outline" className={badgeTone.neutral}>
              Protocol {report.protocolVersion}
            </Badge>
          ) : null}
          {verifyAction}
        </div>
      </div>

      {callout ? (
        <div className="space-y-2">
          <button
            type="button"
            onClick={() => onSelectStep(callout.id)}
            className="hover:bg-muted flex w-full items-start gap-3 rounded-md border p-3 text-left text-sm"
          >
            <span className="pt-0.5">
              <StatusMark status={statusOf(callout)} />
            </span>
            <span className="min-w-0">
              <span className="font-medium">
                {failed
                  ? `Stopped at ${callout.title}.`
                  : `Check ${callout.title}.`}
              </span>{" "}
              <span className="text-muted-foreground break-words">
                {callout.summary}
              </span>
            </span>
          </button>
          {needsPublicAddress(callout) ? <PublicAddressRunbookLink /> : null}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-1 border-b pb-3 text-sm">
        <TabButton
          active={tab === "tools"}
          onClick={() => setTab("tools")}
          label="Tools"
          count={listed ? tools.length : undefined}
        />
        <TabButton
          active={tab === "prompts"}
          onClick={() => setTab("prompts")}
          label="Prompts"
          count={listed ? prompts.length : undefined}
        />
        <TabButton
          active={tab === "resources"}
          onClick={() => setTab("resources")}
          label="Resources"
          count={listed ? resources.length : undefined}
        />
        <TabButton
          active={tab === "instructions"}
          onClick={() => setTab("instructions")}
          label="Instructions"
          count={listed ? (report.instructions ? 1 : 0) : undefined}
        />
        <span className="bg-border mx-2 h-4 w-px" aria-hidden />
        <span className="text-muted-foreground px-2">
          Duration{" "}
          <span className="text-foreground font-mono">
            {formatDuration(report.durationMs)}
          </span>
        </span>
        <span className="text-muted-foreground px-2">
          Checks{" "}
          <span className="text-foreground font-mono">
            {passed}/{ran.length}
          </span>
        </span>
      </div>

      {!listed ? (
        <p className="text-muted-foreground text-sm">
          {report.outcome === "authentication_required"
            ? "Tools, prompts and resources are listed once a signed-in session is open, which this trace never does. To see and run them, sign in through Speakeasy with the button above."
            : "Nothing to list: no MCP session was opened."}
        </p>
      ) : tab === "tools" ? (
        <ToolBrowser tools={tools} />
      ) : tab === "prompts" ? (
        <SimpleList
          empty="This server doesn't offer any prompts."
          items={prompts.map((prompt) => ({
            key: prompt.name,
            title: prompt.title || prompt.name,
            mono: prompt.name,
            description: prompt.description,
          }))}
        />
      ) : tab === "resources" ? (
        <SimpleList
          empty="This server doesn't offer any resources."
          items={resources.map((resource) => ({
            key: resource.uri,
            title: resource.title || resource.name,
            mono: resource.uri,
            description: resource.description,
          }))}
        />
      ) : report.instructions ? (
        <p className="text-sm break-words whitespace-pre-line">
          {report.instructions}
        </p>
      ) : (
        <p className="text-muted-foreground text-sm">
          This server doesn't send instructions.
        </p>
      )}
    </div>
  );
}

function TabButton({
  active,
  onClick,
  label,
  count,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number | undefined;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "hover:bg-muted flex items-center gap-1.5 rounded-md px-2 py-1",
        active ? "bg-muted text-foreground" : "text-muted-foreground",
      )}
    >
      {label}
      <span className="font-mono">{count ?? "–"}</span>
    </button>
  );
}

function SimpleList({
  items,
  empty,
}: {
  items: { key: string; title: string; mono: string; description?: string }[];
  empty: string;
}): JSX.Element {
  if (items.length === 0) {
    return <p className="text-muted-foreground text-sm">{empty}</p>;
  }
  return (
    <ul className="divide-y rounded-md border">
      {items.map((item) => (
        <li key={item.key} className="space-y-0.5 px-4 py-3">
          <p className="text-sm">{item.title}</p>
          <p className="text-muted-foreground truncate font-mono text-xs">
            {item.mono}
          </p>
          {item.description ? (
            <p className="text-muted-foreground text-sm">{item.description}</p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function ToolBrowser({ tools }: { tools: AdminMcpDebugTool[] }): JSX.Element {
  const [query, setQuery] = useState("");
  const [selectedName, setSelectedName] = useState(tools[0]?.name);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return tools;
    return tools.filter(
      (tool) =>
        tool.name.toLowerCase().includes(q) ||
        (tool.title ?? "").toLowerCase().includes(q),
    );
  }, [tools, query]);
  const readOnly = filtered.filter(isReadOnly);
  const other = filtered.filter((tool) => !isReadOnly(tool));
  const selected = tools.find((tool) => tool.name === selectedName) ?? tools[0];

  if (tools.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        This server doesn't offer any tools.
      </p>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(180px,240px)_1fr]">
      <div className="min-w-0 space-y-2">
        <Input
          aria-label="Find a tool"
          placeholder="Find by name…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="flex max-h-[480px] flex-col overflow-y-auto">
          <ToolGroup
            label="Read-only"
            tools={readOnly}
            selected={selected?.name}
            onSelect={setSelectedName}
          />
          <ToolGroup
            label={readOnly.length > 0 ? "Other" : undefined}
            tools={other}
            selected={selected?.name}
            onSelect={setSelectedName}
          />
          {filtered.length === 0 ? (
            <p className="text-muted-foreground px-2 py-3 text-sm">
              No tools match “{query}”.
            </p>
          ) : null}
        </div>
      </div>
      {selected ? <ToolDetail key={selected.name} tool={selected} /> : null}
    </div>
  );
}

function ToolGroup({
  label,
  tools,
  selected,
  onSelect,
}: {
  label?: string;
  tools: AdminMcpDebugTool[];
  selected: string | undefined;
  onSelect: (name: string) => void;
}): JSX.Element | null {
  if (tools.length === 0) return null;
  return (
    <div className="flex flex-col">
      {label ? (
        <p className="text-muted-foreground px-2 pt-3 pb-1 text-xs font-medium">
          {label}
        </p>
      ) : null}
      {tools.map((tool) => (
        <button
          key={tool.name}
          type="button"
          title={tool.name}
          aria-pressed={selected === tool.name}
          onClick={() => onSelect(tool.name)}
          className={cn(
            "hover:bg-muted truncate rounded-md px-2 py-1.5 text-left text-sm",
            selected === tool.name && "bg-muted",
          )}
        >
          {tool.title || tool.name}
        </button>
      ))}
    </div>
  );
}

function ToolDetail({ tool }: { tool: AdminMcpDebugTool }): JSX.Element {
  const hasAnnotations =
    isRecord(tool.annotations) && Object.keys(tool.annotations).length > 0;
  return (
    <div className="min-w-0 space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-0.5">
          <h3 className="font-medium break-all">{tool.title || tool.name}</h3>
          {tool.title ? (
            <p className="text-muted-foreground font-mono text-xs break-all">
              {tool.name}
            </p>
          ) : null}
        </div>
        {isReadOnly(tool) ? (
          <Badge variant="outline" className={badgeTone.neutral}>
            Read-only
          </Badge>
        ) : null}
      </div>
      <section className="space-y-1.5">
        <h4 className="text-sm font-medium">Description</h4>
        <p className="text-sm break-words whitespace-pre-line">
          {tool.description || "No description."}
        </p>
      </section>
      <section className="space-y-1.5">
        <h4 className="text-sm font-medium">Input schema</h4>
        <JsonBlock value={tool.inputSchema ?? {}} label="input schema" />
      </section>
      {hasAnnotations ? (
        <section className="space-y-1.5">
          <h4 className="text-sm font-medium">Annotations</h4>
          <JsonBlock value={tool.annotations} label="annotations" />
        </section>
      ) : null}
    </div>
  );
}
