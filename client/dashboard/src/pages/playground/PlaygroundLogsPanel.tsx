import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { formatDuration } from "@/lib/dates";
import { HTTPToolLog } from "@gram/client/models/components";
import { useListToolLogs, useToolset } from "@gram/client/react-query";
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

function getToolNameFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

function isSuccessfulCall(log: HTTPToolLog): boolean {
  return (
    log.statusCode !== undefined &&
    log.statusCode >= 200 &&
    log.statusCode < 300
  );
}

function getHttpMethodVariant(
  method?: string,
): "default" | "secondary" | "destructive" | "outline" {
  switch (method?.toUpperCase()) {
    case "GET":
      return "default";
    case "POST":
      return "secondary";
    case "PUT":
    case "PATCH":
      return "outline";
    case "DELETE":
      return "destructive";
    default:
      return "secondary";
  }
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
  const [selectedLog, setSelectedLog] = useState<HTTPToolLog | null>(null);

  // Fetch toolset to get tool URNs for filtering
  const { data: toolsetData } = useToolset(
    { slug: toolsetSlug ?? "" },
    undefined,
    { enabled: !!toolsetSlug },
  );

  // Extract tool URNs from the toolset
  const toolUrns = useMemo(() => {
    if (!toolsetData?.toolUrns || toolsetData.toolUrns.length === 0)
      return undefined;
    return toolsetData.toolUrns;
  }, [toolsetData]);

  const { data, isLoading, refetch } = useListToolLogs(
    {
      perPage: 50,
      toolUrns: toolUrns,
    },
    undefined,
    {
      refetchInterval: 5000, // Auto-refresh every 5 seconds
      refetchOnWindowFocus: true,
    },
  );

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
            onClick={() => refetch()}
            disabled={isLoading}
            className="h-7 w-7 p-0"
          >
            <RefreshCwIcon
              className={`size-3.5 ${isLoading ? "animate-spin" : ""}`}
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
              {isLoading
                ? "Loading logs..."
                : !logsEnabled
                  ? "Logs are not enabled for this organization"
                  : "No logs yet"}
            </Type>
          </div>
        ) : (
          <div>
            {logs.map((log) => {
              const isSuccess = isSuccessfulCall(log);
              const toolName = getToolNameFromUrn(log.toolUrn);
              const timestamp = log.ts
                ? new Date(log.ts).toLocaleTimeString()
                : "";

              return (
                <button
                  key={log.id}
                  onClick={() => setSelectedLog(log)}
                  className="w-full text-left px-4 py-2.5 hover:bg-muted/30 transition-colors flex items-start gap-2.5 border-b border-border/40"
                >
                  <StatusIcon isSuccess={isSuccess} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 mb-0.5">
                      <span className="font-medium text-xs truncate">
                        {toolName}
                      </span>
                      {log.httpMethod && (
                        <Badge
                          variant={getHttpMethodVariant(log.httpMethod)}
                          className="text-[10px] px-1 py-0 h-4 font-semibold"
                        >
                          {log.httpMethod}
                        </Badge>
                      )}
                    </div>
                    {log.httpRoute && (
                      <div className="font-mono text-[10px] text-muted-foreground truncate mb-0.5">
                        {log.httpRoute}
                      </div>
                    )}
                    <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                      <span className="font-mono">{timestamp}</span>
                      {log.durationMs !== undefined && (
                        <>
                          <span>•</span>
                          <span className="font-mono">
                            {formatDuration(log.durationMs)}
                          </span>
                        </>
                      )}
                      {log.statusCode !== undefined && (
                        <>
                          <span>•</span>
                          <span
                            className={`font-mono font-semibold ${
                              isSuccess
                                ? "text-success-default"
                                : "text-destructive-default"
                            }`}
                          >
                            {log.statusCode}
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
              {getToolNameFromUrn(selectedLog.toolUrn)}
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
                      Tool URN
                    </span>
                    <span className="font-mono text-right">
                      {selectedLog.toolUrn}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Method
                    </span>
                    <span className="font-mono font-semibold">
                      {selectedLog.httpMethod}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Route
                    </span>
                    <span className="font-mono text-right truncate max-w-[200px]">
                      {selectedLog.httpRoute}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Status
                    </span>
                    <span
                      className={`font-mono font-semibold ${
                        isSuccessfulCall(selectedLog)
                          ? "text-success-default"
                          : "text-destructive-default"
                      }`}
                    >
                      {selectedLog.statusCode}
                    </span>
                  </div>
                  <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                    <span className="text-muted-foreground font-medium">
                      Duration
                    </span>
                    <span className="font-mono">
                      {formatDuration(selectedLog.durationMs)}
                    </span>
                  </div>
                  {selectedLog.requestBodyBytes !== undefined && (
                    <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Request Size
                      </span>
                      <span className="font-mono">
                        {selectedLog.requestBodyBytes} bytes
                      </span>
                    </div>
                  )}
                  {selectedLog.responseBodyBytes !== undefined && (
                    <div className="px-2.5 py-1.5 flex justify-between text-[11px]">
                      <span className="text-muted-foreground font-medium">
                        Response Size
                      </span>
                      <span className="font-mono">
                        {selectedLog.responseBodyBytes} bytes
                      </span>
                    </div>
                  )}
                </div>
              </div>

              {/* Request Headers */}
              {selectedLog.requestHeaders &&
                Object.keys(selectedLog.requestHeaders).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Request Headers
                    </div>
                    <pre className="bg-background/60 border border-border/40 p-2 rounded text-[10px] overflow-x-auto font-mono leading-relaxed">
                      {JSON.stringify(selectedLog.requestHeaders, null, 2)}
                    </pre>
                  </div>
                )}

              {/* Response Headers */}
              {selectedLog.responseHeaders &&
                Object.keys(selectedLog.responseHeaders).length > 0 && (
                  <div className="space-y-1.5">
                    <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                      Response Headers
                    </div>
                    <pre className="bg-background/60 border border-border/40 p-2 rounded text-[10px] overflow-x-auto font-mono leading-relaxed">
                      {JSON.stringify(selectedLog.responseHeaders, null, 2)}
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
