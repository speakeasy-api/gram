import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
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
      <div className="bg-success-default/10 shrink-0 rounded-full p-0.5">
        <CheckIcon className="stroke-success-default size-3 stroke-2" />
      </div>
    );
  }
  return (
    <div className="bg-destructive-default/10 shrink-0 rounded-full p-0.5">
      <XIcon className="stroke-destructive-default size-3 stroke-2" />
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

function formatTimestamp(timeUnixNano: string): string {
  return new Date(
    Number(BigInt(timeUnixNano) / 1_000_000n),
  ).toLocaleTimeString();
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
    error,
    refetch: fetchLogs,
  } = useQuery({
    queryKey: ["playground-logs", gramUrns],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          filter: gramUrns ? { gramUrns } : undefined,
          limit: 50,
          sort: "desc",
        }),
      ),
    refetchInterval: 5000,
    throwOnError: false,
  });

  const logs = data?.logs || [];
  // Logs are disabled if we get a 404 error (endpoint returns 404 when disabled)
  const logsDisabled =
    error instanceof ServiceError && error.statusCode === 404;

  return (
    <div className="flex h-full flex-col border-l">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3">
        <span className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
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
                : logsDisabled
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
                  className="hover:bg-muted/30 border-border/40 flex w-full items-start gap-2.5 border-b px-4 py-2.5 text-left transition-colors"
                >
                  <StatusIcon isSuccess={isSuccess} />
                  <div className="min-w-0 flex-1">
                    <div className="mb-0.5 flex items-center gap-1.5">
                      {toolName && (
                        <span className="truncate text-xs font-medium">
                          {toolName}
                        </span>
                      )}
                      {log.severityText && (
                        <Badge
                          variant={getSeverityVariant(log.severityText)}
                          className="h-4 px-1 py-0 text-[10px] font-semibold"
                        >
                          {log.severityText}
                        </Badge>
                      )}
                    </div>
                    <div className="text-muted-foreground mb-0.5 truncate font-mono text-[10px]">
                      {log.body}
                    </div>
                    <div className="text-muted-foreground flex items-center gap-1.5 text-[10px]">
                      <span className="font-mono">{timestamp}</span>
                      {log.traceId && (
                        <>
                          <span>•</span>
                          <span className="max-w-[100px] truncate font-mono">
                            {log.traceId.slice(0, 8)}...
                          </span>
                        </>
                      )}
                    </div>
                  </div>
                  <ChevronRightIcon className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Detail View */}
      {selectedLog && (
        <div className="bg-muted/20 border-t">
          <div className="flex items-center justify-between border-b bg-inherit px-3 py-2.5">
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
          <div className="h-[280px] overflow-y-auto p-3">
            <div className="space-y-3">
              {/* Log Details */}
              <div className="space-y-1.5">
                <div className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">
                  Details
                </div>
                <div className="bg-background/60 border-border/40 divide-border/40 divide-y rounded border">
                  <div className="flex justify-between px-2.5 py-1.5 text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Service
                    </span>
                    <span className="text-right font-mono">
                      {selectedLog.service?.name}
                      {selectedLog.service?.version &&
                        ` (${selectedLog.service.version})`}
                    </span>
                  </div>
                  <div className="flex justify-between px-2.5 py-1.5 text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Severity
                    </span>
                    <span className="font-mono font-semibold">
                      {selectedLog.severityText || "N/A"}
                    </span>
                  </div>
                  <div className="flex justify-between px-2.5 py-1.5 text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Timestamp
                    </span>
                    <span className="text-right font-mono">
                      {new Date(
                        Number(BigInt(selectedLog.timeUnixNano) / 1_000_000n),
                      ).toISOString()}
                    </span>
                  </div>
                  {selectedLog.traceId && (
                    <div className="flex justify-between px-2.5 py-1.5 text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Trace ID
                      </span>
                      <span className="max-w-[200px] truncate text-right font-mono">
                        {selectedLog.traceId}
                      </span>
                    </div>
                  )}
                  {selectedLog.spanId && (
                    <div className="flex justify-between px-2.5 py-1.5 text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Span ID
                      </span>
                      <span className="text-right font-mono">
                        {selectedLog.spanId}
                      </span>
                    </div>
                  )}
                </div>
              </div>

              {/* Body */}
              {selectedLog.body && (
                <div className="space-y-1.5">
                  <div className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                    Message
                  </div>
                  <pre className="bg-background/60 border-border/40 overflow-x-auto rounded border p-2 font-mono text-[10px] leading-relaxed whitespace-pre-wrap">
                    {selectedLog.body}
                  </pre>
                </div>
              )}

              {/* Attributes */}
              {selectedLog.attributes &&
                typeof selectedLog.attributes === "object" &&
                Object.keys(selectedLog.attributes).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                      Attributes
                    </div>
                    <pre className="bg-background/60 border-border/40 overflow-x-auto rounded border p-2 font-mono text-[10px] leading-relaxed">
                      {JSON.stringify(selectedLog.attributes, null, 2)}
                    </pre>
                  </div>
                )}

              {/* Resource Attributes */}
              {selectedLog.resourceAttributes &&
                typeof selectedLog.resourceAttributes === "object" &&
                Object.keys(selectedLog.resourceAttributes).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                      Resource Attributes
                    </div>
                    <pre className="bg-background/60 border-border/40 overflow-x-auto rounded border p-2 font-mono text-[10px] leading-relaxed">
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
