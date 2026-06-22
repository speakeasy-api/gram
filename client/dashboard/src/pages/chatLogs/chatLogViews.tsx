import type { JSX } from "react";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import type { TelemetryLogRecord } from "@gram/client/models/components";
import { cn } from "@/lib/utils";
import { formatLogTimestamp, getSeverityBadgeVariant } from "./chatLogFilters";

export function TelemetryLogsView({
  logs,
  isLoading,
  error,
}: {
  logs: TelemetryLogRecord[];
  isLoading: boolean;
  error: Error | null;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        Loading telemetry logs...
      </div>
    );
  }
  if (error) {
    return (
      <div className="text-destructive p-6 text-center">
        Failed to load logs: {error.message}
      </div>
    );
  }
  if (logs.length === 0) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        No telemetry logs found for this agent session.
      </div>
    );
  }

  return (
    <div className="divide-border divide-y">
      {logs.map((log) => (
        <div key={log.id} className="hover:bg-muted/30 p-4 transition-colors">
          <div className="flex items-start gap-3">
            <Badge
              variant={getSeverityBadgeVariant(log.severityText)}
              className="mt-0.5 shrink-0"
            >
              {log.severityText || "INFO"}
            </Badge>
            <div className="min-w-0 flex-1 space-y-1">
              <div className="text-sm font-medium wrap-break-word">
                {log.body.trim()}
              </div>
              <div className="text-muted-foreground flex items-center gap-3 text-xs">
                <span>{formatLogTimestamp(log.timeUnixNano)}</span>
                {log.service?.name && (
                  <span className="flex items-center gap-1">
                    <Icon name="server" className="size-3" />
                    {log.service.name}
                  </span>
                )}
                {log.traceId && (
                  <span className="font-mono text-[10px]">
                    {log.traceId.slice(0, 8)}...
                  </span>
                )}
              </div>
              {log.attributes && Object.keys(log.attributes).length > 0 && (
                <details className="mt-2">
                  <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-xs">
                    Show attributes
                  </summary>
                  <pre className="bg-muted/50 mt-1 overflow-x-auto rounded p-2 text-xs">
                    {JSON.stringify(log.attributes, null, 2)}
                  </pre>
                </details>
              )}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

export function ToolCallsView({
  toolLogs,
  isLoading,
  error,
}: {
  toolLogs: TelemetryLogRecord[];
  isLoading: boolean;
  error: Error | null;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        Loading tool call logs...
      </div>
    );
  }
  if (error) {
    return (
      <div className="text-destructive p-6 text-center">
        Failed to load tool calls: {error.message}
      </div>
    );
  }
  if (toolLogs.length === 0) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        No tool call logs found for this agent session.
      </div>
    );
  }

  return (
    <div className="divide-border divide-y">
      {toolLogs.map((log) => {
        const attrs = log.attributes || {};
        const toolName = attrs.tool_name || attrs.function_name || "Unknown";
        const gramUrn = attrs.gram_urn;
        const status = attrs.http_status_code;
        const isError = !!status && status >= 400;

        return (
          <div key={log.id} className="hover:bg-muted/30 p-4 transition-colors">
            <div className="flex items-start gap-3">
              <div
                className={cn(
                  "flex size-8 shrink-0 items-center justify-center rounded-full",
                  isError ? "bg-destructive/10" : "bg-primary/10",
                )}
              >
                <Icon
                  name="zap"
                  className={cn(
                    "size-4",
                    isError ? "text-destructive" : "text-primary",
                  )}
                />
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{toolName}</span>
                  {status && (
                    <Badge variant={isError ? "destructive" : "neutral"}>
                      {status}
                    </Badge>
                  )}
                </div>
                {gramUrn && (
                  <div className="text-muted-foreground font-mono text-xs">
                    {gramUrn}
                  </div>
                )}
                <div className="text-muted-foreground text-xs">
                  {formatLogTimestamp(log.timeUnixNano)}
                </div>
                {log.body && (
                  <div className="text-muted-foreground mt-1 text-sm">
                    {log.body.trim()}
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
