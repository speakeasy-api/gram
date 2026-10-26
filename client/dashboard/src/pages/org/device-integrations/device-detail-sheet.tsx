import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { ManagedDevice } from "@gram/client/models/components/manageddevice.js";
import type { ManagedDeviceAgentState } from "@gram/client/models/components/manageddeviceagentstate.js";
import type { ManagedDeviceTool } from "@gram/client/models/components/manageddevicetool.js";
import { format } from "date-fns";
import { ChevronDown } from "lucide-react";
import { Link } from "react-router";
import {
  coverageDisplay,
  deviceCoverageKey,
  osLabel,
  toolLabel,
} from "./coverage-states";

/**
 * One managed device: whether it enforces the policy it was sent, and, when
 * it does not, which tool is holding it back. The agent reports outcomes as
 * closed codes with no error text, so every reason below is ours to word.
 */
export function DeviceDetailSheet({
  device,
  onOpenChange,
}: {
  device: ManagedDevice | null;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Sheet open={device !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex flex-col sm:max-w-xl">
        {device ? <DeviceDetails device={device} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function DeviceDetails({ device }: { device: ManagedDevice }): JSX.Element {
  const display = coverageDisplay(deviceCoverageKey(device));
  const Icon = display.icon;
  const state = device.agentState;
  return (
    <>
      <SheetHeader>
        <SheetTitle className="truncate">
          {device.hostname ?? device.externalId}
        </SheetTitle>
        <SheetDescription className="truncate">
          {device.userEmail ?? "No assigned user"}
        </SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-6 overflow-y-auto px-4 pb-6">
        {state?.enforcementHalted ? <HaltedAlert state={state} /> : null}

        <section className="border-border border p-3">
          <DetailRow label="Status">
            <Badge variant={display.variant} background>
              <Badge.LeftIcon>
                <Icon className="h-3.5 w-3.5" />
              </Badge.LeftIcon>
              <Badge.Text>{display.label}</Badge.Text>
            </Badge>
            {/* The alert above already explains a paused device. */}
            {state?.enforcementHalted ? null : (
              <Text small muted className="mt-1 block">
                {display.detail}
              </Text>
            )}
          </DetailRow>
          {state?.appliedAt ? (
            <DetailRow label="Policy applied">
              <Timestamp date={state.appliedAt} />
            </DetailRow>
          ) : null}
          {state ? (
            <DetailRow label="Last report">
              <Timestamp date={state.reportedAt} />
            </DetailRow>
          ) : (
            // Without a report, the heartbeat is the only sign of the agent.
            <DetailRow label="Agent last seen">
              {device.agentLastSeenAt ? (
                <Timestamp date={device.agentLastSeenAt} />
              ) : (
                <Text small muted>
                  Never
                </Text>
              )}
            </DetailRow>
          )}
        </section>

        {state ? <ToolsSection state={state} /> : null}
        {state ? <AgentSection state={state} /> : null}

        <Collapsible>
          <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
            <ChevronDown
              aria-hidden="true"
              className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
            />
            Technical details
          </CollapsibleTrigger>
          <CollapsibleContent className="border-border mt-2 border p-3">
            <DetailRow label="Policy version">
              <Mono>{state?.appliedEtag ?? "—"}</Mono>
            </DetailRow>
            <DetailRow label="Serial number">
              <Mono>{device.serialNumber ?? "—"}</Mono>
            </DetailRow>
            <DetailRow label="MDM device ID">
              <Mono>{device.externalId}</Mono>
            </DetailRow>
          </CollapsibleContent>
        </Collapsible>
      </div>
    </>
  );
}

// The kill switch is the one state worth interrupting for: the agent is
// installed and running, and enforces nothing.
function HaltedAlert({
  state,
}: {
  state: ManagedDeviceAgentState;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const version = state.speakeasydVersion;
  return (
    <Alert variant="error" title="Enforcement paused">
      This device runs{" "}
      {version ? `agent version ${version}` : "an agent version"}, which your
      organization blocked, so the agent stopped enforcing policy. Update the
      agent on this device to resume, or{" "}
      <Link
        to={orgRoutes.deviceAgent.configuration.href()}
        className="underline underline-offset-2"
      >
        review the blocked versions
      </Link>
      .
    </Alert>
  );
}

function ToolsSection({
  state,
}: {
  state: ManagedDeviceAgentState;
}): JSX.Element {
  // Problems first: the admin opened this device to find what is wrong.
  const tools = [...state.tools].sort(
    (a, b) => toolRank(a) - toolRank(b) || a.tool.localeCompare(b.tool),
  );
  const driftSince = state.driftCountingSince;
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Tools</h3>
      {tools.length === 0 ? (
        <Text small muted>
          The agent manages no tools on this device.
        </Text>
      ) : (
        <ul className="border-border divide-border divide-y border">
          {tools.map((tool) => (
            <ToolRow
              key={tool.tool}
              tool={tool}
              halted={state.enforcementHalted}
            />
          ))}
        </ul>
      )}
      {driftSince && tools.some((t) => (t.driftCorrections ?? 0) > 0) ? (
        <Text small muted className="block">
          Restores are counted since {format(driftSince, "d MMM")}.
        </Text>
      ) : null}
    </section>
  );
}

function ToolRow({
  tool,
  halted,
}: {
  tool: ManagedDeviceTool;
  halted: boolean;
}): JSX.Element {
  // The outcomes come from the reconcile before the kill switch, so a tool
  // that was enforcing then is paused now.
  const status =
    halted && tool.outcome === "armed"
      ? { label: "Paused", className: "text-default-destructive" }
      : toolStatus(tool);
  const notes = [
    tool.actionRequired ? "Needs action from the device's user." : null,
    tool.restartRequired ? "Restart the tool to load the latest policy." : null,
    tool.driftCorrections
      ? `Speakeasy undid a local change to these settings ${
          tool.driftCorrections
        } ${tool.driftCorrections === 1 ? "time" : "times"}.`
      : null,
  ].filter((note): note is string => note !== null);
  return (
    <li className="space-y-1 p-3">
      <div className="flex items-baseline justify-between gap-3">
        <div className="min-w-0">
          <Text className="truncate font-medium">{toolLabel(tool.tool)}</Text>
          {/* Where the policy lands only matters for a tool it reaches. */}
          {tool.outcome === "skipped" || tool.outcome === "unmanaged" ? null : (
            <Text small muted className="block">
              {layerLabel(tool.layer)}
            </Text>
          )}
        </div>
        <span className={cn("shrink-0 text-right text-sm", status.className)}>
          {status.label}
        </span>
      </div>
      {status.reason || notes.length > 0 ? (
        <Text small muted className="block">
          {[status.reason, ...notes].filter(Boolean).join(" ")}
        </Text>
      ) : null}
    </li>
  );
}

function AgentSection({
  state,
}: {
  state: ManagedDeviceAgentState;
}): JSX.Element {
  const platform = [state.os ? osLabel(state.os) : null, state.arch]
    .filter(Boolean)
    .join(" · ");
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Agent</h3>
      <div className="border-border border p-3">
        <DetailRow label="Background service">
          <Version value={state.speakeasydVersion} />
          {state.currentBlocked && !state.enforcementHalted ? (
            <Text small className="text-default-destructive mt-1 block">
              This version is blocked.
            </Text>
          ) : null}
        </DetailRow>
        <DetailRow label="Command line">
          <Version value={state.speakeasyVersion} />
        </DetailRow>
        <DetailRow label="Menu bar app">
          <Version value={state.speakeasyUiVersion} />
        </DetailRow>
        {platform ? <DetailRow label="Platform">{platform}</DetailRow> : null}
      </div>
    </section>
  );
}

type ToolStatus = { label: string; className: string; reason?: string };

function toolStatus(tool: ManagedDeviceTool): ToolStatus {
  switch (tool.outcome) {
    case "armed":
      return { label: "Enforcing", className: "" };
    case "pending":
      return {
        label: "Waiting",
        className: "text-default-warning",
        reason: PENDING_REASONS[tool.pendingReason ?? ""] ?? undefined,
      };
    case "error":
      return {
        label: "Not enforcing",
        className: "text-default-destructive",
        reason:
          ERROR_KINDS[tool.errorKind ?? ""] ??
          "The agent hit an unexpected error applying the policy.",
      };
    case "skipped":
      return { label: "Not installed", className: "text-muted-foreground" };
    case "unmanaged":
      return { label: "Not managed", className: "text-muted-foreground" };
    default:
      return { label: "Unknown", className: "text-muted-foreground" };
  }
}

function toolRank(tool: ManagedDeviceTool): number {
  switch (tool.outcome) {
    case "error":
      return 0;
    case "pending":
      return 1;
    case "armed":
      return 2;
    default:
      return 3;
  }
}

const PENDING_REASONS: Record<string, string> = {
  privileged_writer_not_ready:
    "Managed settings need the agent's admin helper, which is not running yet.",
  plugin_bundle_not_cloned: "The agent is still downloading plugins.",
};

const ERROR_KINDS: Record<string, string> = {
  permission_denied: "The agent is not allowed to write this tool's settings.",
  not_found: "The agent could not find where this tool keeps its settings.",
  invalid_config: "This tool's existing settings could not be read.",
};

function layerLabel(layer: string): string {
  switch (layer) {
    case "managed":
      return "Managed settings";
    case "user":
      return "User settings";
    default:
      return "Unknown settings layer";
  }
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    // Label above value on a phone, so a status badge never spills out.
    <div className="grid gap-1 py-1.5 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-3">
      <Text small muted>
        {label}
      </Text>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

function Timestamp({ date }: { date: Date }): JSX.Element {
  return <span title={format(date, "PPp")}>{formatRelativeTime(date)}</span>;
}

function Version({ value }: { value: string | undefined }): JSX.Element {
  return value ? (
    <Mono>{value}</Mono>
  ) : (
    <Text small muted>
      Not reported
    </Text>
  );
}

function Mono({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <span
      className="block truncate font-mono text-xs"
      title={typeof children === "string" ? children : undefined}
    >
      {children}
    </span>
  );
}
