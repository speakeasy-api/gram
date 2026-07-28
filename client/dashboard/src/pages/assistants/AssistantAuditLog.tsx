import {
  ActionBadge,
  ActionDot,
  AuditFeedFooter,
  DateGroupHeader,
  FacetSelect,
} from "@/components/auditlogs/feed";
import {
  formatTimeOnly,
  groupLogsByDate,
  type TimestampMode,
} from "@/lib/audit-log-feed";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAuditLogsInfinite } from "@gram/client/react-query/auditLogs.js";
import { Icon } from "@speakeasy-api/moonshine";
import React, { useMemo, useState } from "react";
import { useQueryState } from "nuqs";

const TIMESTAMP_MODE: TimestampMode = "local";

function metadataString(log: AuditLog, key: string): string | undefined {
  const value = log.metadata?.[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function formatParams(params: unknown): string | undefined {
  if (params == null) return undefined;
  if (typeof params === "string") return params;
  try {
    return JSON.stringify(params, null, 2);
  } catch {
    return undefined;
  }
}

function AssistantAuditLogRow({
  log,
  assistantName,
  isOdd,
}: {
  log: AuditLog;
  assistantName: string;
  isOdd: boolean;
}) {
  const [paramsExpanded, setParamsExpanded] = useState(false);

  const toolName = metadataString(log, "tool_name") ?? log.subjectDisplayName;
  const toolsetSlug = metadataString(log, "toolset_slug") ?? log.subjectSlug;
  const params = formatParams(log.metadata?.["params"]);
  const paramsTruncated = log.metadata?.["params_truncated"] === true;

  const rowContent = (
    <div className="flex items-start gap-3.5 px-4 py-2.5">
      <ActionDot action={log.action} />
      <ActionBadge action={log.action} />
      <div className="min-w-0 flex-1 text-sm leading-5">
        <span>
          <strong className="text-foreground font-semibold">
            {assistantName}
          </strong>{" "}
          <span className="text-muted-foreground">called</span>{" "}
          <span className="text-muted-foreground font-mono text-xs">
            {toolName}
          </span>
          {toolsetSlug && (
            <>
              {" "}
              <span className="text-muted-foreground">in</span>{" "}
              <span className="text-muted-foreground font-mono text-xs">
                {toolsetSlug}
              </span>
            </>
          )}
        </span>
        {params && (
          <button
            type="button"
            onClick={() => setParamsExpanded((v) => !v)}
            className="ml-2 text-xs text-blue-500 hover:underline"
          >
            {paramsExpanded ? "Hide params ▴" : "Show params ▾"}
          </button>
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs">
        {formatTimeOnly(log.createdAt, TIMESTAMP_MODE)}
      </span>
    </div>
  );

  if (params && paramsExpanded) {
    return (
      <div>
        <div
          className={cn(
            "rounded-t-lg border border-b-0",
            isOdd ? "bg-muted/30" : "bg-background",
          )}
        >
          {rowContent}
        </div>
        <div className="bg-background rounded-b-lg border border-t-0 px-4 pt-2 pb-3">
          <pre className="bg-muted/30 text-muted-foreground max-h-80 overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">
            {params}
          </pre>
          {paramsTruncated && (
            <Type muted small className="mt-1.5">
              Parameters were truncated for storage.
            </Type>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      className={cn(
        "rounded-none transition-colors",
        isOdd ? "bg-muted/30" : "bg-background",
      )}
    >
      {rowContent}
    </div>
  );
}

/**
 * Audit trail of assistant activity: one entry per tool call an assistant
 * has made, filterable by assistant. Mirrors the styling of the platform
 * audit logs page, which intentionally hides these events.
 */
export function AssistantsAuditLog(): React.JSX.Element {
  const { projectSlug } = useSlugs();
  const [selectedAssistant, setSelectedAssistant] = useQueryState("assistant", {
    defaultValue: "all",
  });

  const { data: assistantsData } = useAssistantsList(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const assistants = useMemo(
    () =>
      [...(assistantsData?.assistants ?? [])].sort((a, b) =>
        a.name.localeCompare(b.name),
      ),
    [assistantsData?.assistants],
  );

  const assistantNameById = useMemo(
    () =>
      new Map(assistants.map((assistant) => [assistant.id, assistant.name])),
    [assistants],
  );

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    isLoading,
  } = useAuditLogsInfinite({
    projectSlug,
    subjectType: "assistant",
    subjectId: selectedAssistant === "all" ? undefined : selectedAssistant,
  });

  const logs = useMemo(
    () => data?.pages.flatMap((page) => page.result.logs) ?? [],
    [data],
  );

  const dateGroups = useMemo(
    () => groupLogsByDate(logs, TIMESTAMP_MODE),
    [logs],
  );

  return (
    <div className="flex w-full flex-col gap-4">
      <div>
        <Heading variant="h3" className="mb-2">
          Assistant activity
        </Heading>
        <Type muted small className="mt-1">
          Every autonomous tool call your assistants make. The assistant, tool,
          MCP and parameters. These events are kept out of the organization
          audit log; filter by assistant below.
        </Type>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <FacetSelect
          label="Assistant"
          value={selectedAssistant}
          onValueChange={(value) => {
            void setSelectedAssistant(value);
          }}
          placeholder="All assistants"
          allLabel="All assistants"
          options={assistants.map((assistant) => ({
            value: assistant.id,
            displayName: assistant.name,
          }))}
        />
      </div>

      <div className="bg-background overflow-hidden rounded-lg border">
        {isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <Icon name="loader-circle" className="size-4 animate-spin" />
            <span>Loading assistant activity...</span>
          </div>
        ) : error ? (
          <div className="flex flex-col items-center gap-2 py-12 text-center">
            <Type className="font-medium">
              Error loading assistant activity
            </Type>
            <Type muted small>
              {error.message}
            </Type>
          </div>
        ) : logs.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-12 text-center">
            <Type className="font-medium">No assistant activity yet</Type>
            <Type muted small>
              {selectedAssistant === "all"
                ? "Tool calls made by your assistants will appear here."
                : "This assistant has not made any tool calls yet."}
            </Type>
          </div>
        ) : (
          <div>
            {dateGroups.map((group) => (
              <React.Fragment key={group.key}>
                <DateGroupHeader date={group.date} mode={TIMESTAMP_MODE} />
                {group.logs.map((log, rowIndex) => (
                  <AssistantAuditLogRow
                    key={log.id}
                    log={log}
                    assistantName={
                      assistantNameById.get(log.subjectId) ??
                      "Deleted assistant"
                    }
                    isOdd={rowIndex % 2 === 1}
                  />
                ))}
              </React.Fragment>
            ))}
          </div>
        )}

        <AuditFeedFooter
          count={logs.length}
          noun="tool call"
          hasNextPage={hasNextPage ?? false}
          isFetching={isFetching}
          isFetchingNextPage={isFetchingNextPage}
          onLoadMore={() => {
            void fetchNextPage();
          }}
          endLabel="End of assistant activity"
        />
      </div>
    </div>
  );
}
