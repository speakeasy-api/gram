import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { useGramContext, useToolset } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import {
  CheckIcon,
  ChevronRightIcon,
  RefreshCwIcon,
  XIcon,
  XCircleIcon,
} from "lucide-react";
import { useMemo, useState } from "react";

function StatusIcon({ isSuccess }: { isSuccess: boolean }) {
  if (isSuccess) {
    return (
      <div className="rounded-full bg-success-default/10 p-0.5 shrink-0">
        <CheckIcon className="size-3 stroke-success-default stroke-2" />
      </div>
    );
  }
  return (
    <div className="rounded-full bg-destructive-default/10 p-0.5 shrink-0">
      <XIcon className="size-3 stroke-destructive-default stroke-2" />
    </div>
  );
}

function getSeverityVariant(
  severity?: string | null,
): "default" | "secondary" | "destructive" | "outline" {
  switch (severity?.toUpperCase()) {
    case "INFO":
      return "default";
    case "WARN":
      return "outline";
    case "ERROR":
    case "FATAL":
      return "destructive";
    default:
      return "secondary";
  }
}

function formatTimestamp(timeUnixNano: number): string {
  return new Date(timeUnixNano / 1_000_000).toLocaleTimeString();
}

function isSuccessLog(log: TelemetryLogRecord): boolean {
  const severity = log.severityText?.toUpperCase();
  return severity !== "ERROR" && severity !== "FATAL";
}

function getToolName(log: TelemetryLogRecord): string {
  const resourceAttrs = log.resourceAttributes as
    | { gram?: { tool?: { urn?: string } } }
    | undefined;
  const urn = resourceAttrs?.gram?.tool?.urn;

  if (!urn) return "";

  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

interface PlaygroundLogsPanelProps {
  chatId?: string;
  toolsetSlug?: string;
  onClose: () => void;
}

export function PlaygroundLogsPanel({
  chatId: _chatId,
  toolsetSlug,
  onClose,
}: PlaygroundLogsPanelProps) {
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );

  // Fetch toolset to get tool URNs for filtering
  const { data: toolsetData } = useToolset(
    { slug: toolsetSlug ?? "" },
    undefined,
    { enabled: !!toolsetSlug },
  );

  // Extract tool URNs from the toolset
  const gramUrns = useMemo(() => {
    if (!toolsetData?.toolUrns || toolsetData.toolUrns.length === 0)
      return undefined;
    return toolsetData.toolUrns;
  }, [toolsetData]);

  const client = useGramContext();

  const {
    data,
    isPending,
    refetch: fetchLogs,
  } = useQuery({
    queryKey: ["playground-logs", gramUrns],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            filter: gramUrns ? { gramUrns } : undefined,
            limit: 50,
            sort: "desc",
          },
        }),
      ),
    refetchInterval: 5000,
  });

  const logs = data?.logs || [];
  const logsEnabled = data?.enabled ?? true;

  return (
    <div className="h-full flex flex-col border-l">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
          Logs ({logs.length})
        </span>
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => fetchLogs()}
            disabled={isPending}
            className="h-7 w-7 p-0"
          >
            <RefreshCwIcon
              className={`size-3.5 ${isPending ? "animate-spin" : ""}`}
            />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={onClose}
            className="h-7 w-7 p-0"
          >
            <XCircleIcon className="size-3.5" />
          </Button>
        </div>
      </div>

      {/* Logs List */}
      <div className="flex-1 overflow-y-auto">
        {logs.length === 0 ? (
          <div className="px-4 py-6 text-center">
            <Type variant="small" className="text-muted-foreground">
              {isPending
                ? "Loading logs..."
                : !logsEnabled
                  ? "Logs are not enabled for this organization"
                  : "No logs yet"}
            </Type>
          </div>
        ) : (
          <div>
            {logs.map((log) => {
              const isSuccess = isSuccessLog(log);
              const timestamp = formatTimestamp(log.timeUnixNano);
              const toolName = getToolName(log);

              return (
                <button
                  key={log.id}
                  onClick={() => setSelectedLog(log)}
                  className="w-full text-left px-4 py-2.5 hover:bg-muted/30 transition-colors flex items-start gap-2.5 border-b border-border/40"
                >
                  <StatusIcon isSuccess={isSuccess} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 mb-0.5">
                      {toolName && (
                        <span className="font-medium text-xs truncate">
                          {toolName}
                        </span>
                      )}
                      {log.severityText && (
                        <Badge
                          variant={getSeverityVariant(log.severityText)}
                          className="text-[10px] px-1 py-0 h-4 font-semibold"
                        >
                          {log.severityText}
                        </Badge>
                      )}
                    </div>
                    <div className="font-mono text-[10px] text-muted-foreground truncate mb-0.5">
                      {log.body}
                    </div>
                    <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                      <span className="font-mono">{timestamp}</span>
                      {log.traceId && (
                        <>
                          <span>•</span>
                          <span className="font-mono truncate max-w-[100px]">
                            {log.traceId.slice(0, 8)}...
                          </span>
                        </>
                      )}
                    </div>
                  </div>
                  <ChevronRightIcon className="size-3.5 text-muted-foreground shrink-0 mt-0.5" />
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Detail View */}
      {selectedLog && (
        <div className="border-t bg-muted/20">
          <div className="px-3 py-2.5 border-b flex items-center justify-between bg-inherit">
            <span className="text-xs font-semibold">
              {getToolName(selectedLog) || "Log Details"}
            </span>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setSelectedLog(null)}
              className="h-6 w-6 p-0"
            >
              <XCircleIcon className="size-3.5" />
            </Button>
          </div>
          <div className="h-[280px] p-3 overflow-y-auto">
            <div className="space-y-3">
              {/* Log Details */}
              <div className="space-y-1.5">
                <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">
                  Details
                </div>
                <div className="bg-background/60 rounded border border-border/40 divide-y divide-border/40">
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Service
                    </span>
                    <span className="font-mono text-right">
                      {selectedLog.service?.name}
                      {selectedLog.service?.version &&
                        ` (${selectedLog.service.version})`}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Severity
                    </span>
                    <span className="font-mono font-semibold">
                      {selectedLog.severityText || "N/A"}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Timestamp
                    </span>
                    <span className="font-mono text-right">
                      {new Date(
                        selectedLog.timeUnixNano / 1_000_000,
                      ).toISOString()}
                    </span>
                  </div>
                  {selectedLog.traceId && (
                    <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Trace ID
                      </span>
                      <span className="font-mono text-right truncate max-w-[200px]">
                        {selectedLog.traceId}
                      </span>
                    </div>
                  )}
                  {selectedLog.spanId && (
                    <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Span ID
                      </span>
                      <span className="font-mono text-right">
                        {selectedLog.spanId}
                      </span>
                    </div>
                  )}
                </div>
              </div>

              {/* Body */}
              {selectedLog.body && (
                <div className="space-y-1.5">
                  <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                    Message
                  </div>
                  <pre className="bg-background/60 border border-border/40 p-2 rounded text-[10px] overflow-x-auto font-mono leading-relaxed whitespace-pre-wrap">
                    {selectedLog.body}
                  </pre>
                </div>
              )}

              {/* Attributes */}
              {selectedLog.attributes &&
                typeof selectedLog.attributes === "object" &&
                Object.keys(selectedLog.attributes).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Attributes
                    </div>
                    <pre className="bg-background/60 border border-border/40 p-2 rounded text-[10px] overflow-x-auto font-mono leading-relaxed">
                      {JSON.stringify(selectedLog.attributes, null, 2)}
                    </pre>
                  </div>
                )}

              {/* Resource Attributes */}
              {selectedLog.resourceAttributes &&
                typeof selectedLog.resourceAttributes === "object" &&
                Object.keys(selectedLog.resourceAttributes).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Resource Attributes
                    </div>
                    <pre className="bg-background/60 border border-border/40 p-2 rounded text-[10px] overflow-x-auto font-mono leading-relaxed">
                      {JSON.stringify(selectedLog.resourceAttributes, null, 2)}
                    </pre>
                  </div>
                )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
