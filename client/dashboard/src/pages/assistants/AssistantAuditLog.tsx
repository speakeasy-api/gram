import {
  ActionIconTile,
  AuditFeedFooter,
  DateGroupHeader,
  FacetSelect,
} from "@/components/auditlogs/feed";
import {
  formatTimeOnly,
  groupLogsByDate,
  type TimestampMode,
} from "@/lib/audit-log-feed";
import { PageEyebrow } from "@/components/page-eyebrow";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { useSlugs } from "@/contexts/Sdk";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAuditLogsInfinite } from "@gram/client/react-query/auditLogs.js";
import { Icon } from "@/components/ui/Icon";
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
}: {
  log: AuditLog;
  assistantName: string;
}) {
  const [paramsExpanded, setParamsExpanded] = useState(false);

  const toolName = metadataString(log, "tool_name") ?? log.subjectDisplayName;
  const toolsetSlug = metadataString(log, "toolset_slug") ?? log.subjectSlug;
  const params = formatParams(log.metadata?.["params"]);
  const paramsTruncated = log.metadata?.["params_truncated"] === true;

  const rowContent = (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <ActionIconTile action={log.action} />
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
            className="text-link-primary ml-2 text-xs hover:underline"
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
        <div className="bg-card border border-b-0">{rowContent}</div>
        <div className="bg-card border border-t-0 px-4 pt-2 pb-3">
          <pre className="bg-muted/30 text-muted-foreground max-h-80 overflow-auto p-3 font-mono text-xs whitespace-pre-wrap">
            {params}
          </pre>
          {paramsTruncated && (
            <Text muted small className="mt-1.5">
              Parameters were truncated for storage.
            </Text>
          )}
        </div>
      </div>
    );
  }

  return <div className="bg-card transition-colors">{rowContent}</div>;
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
        <PageEyebrow className="mb-2" />
        <Heading variant="h4" className="mb-2 text-display-sm font-thin">
          Assistant activity
        </Heading>
        <Text muted small className="mt-1">
          Every autonomous tool call your assistants make. The assistant, tool,
          MCP and parameters. These events are kept out of the organization
          audit log; filter by assistant below.
        </Text>
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

      <div className="bg-card overflow-hidden border">
        {isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <Icon name="loader-circle" className="size-4 animate-spin" />
            <span>Loading assistant activity...</span>
          </div>
        ) : error ? (
          <div className="flex flex-col items-center gap-2 py-12 text-center">
            <Text className="font-medium">
              Error loading assistant activity
            </Text>
            <Text muted small>
              {error.message}
            </Text>
          </div>
        ) : logs.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-12 text-center">
            <Text className="font-medium">No assistant activity yet</Text>
            <Text muted small>
              {selectedAssistant === "all"
                ? "Tool calls made by your assistants will appear here."
                : "This assistant has not made any tool calls yet."}
            </Text>
          </div>
        ) : (
          <div className="divide-border divide-y">
            {dateGroups.map((group) => (
              <React.Fragment key={group.key}>
                <DateGroupHeader date={group.date} mode={TIMESTAMP_MODE} />
                {group.logs.map((log) => (
                  <AssistantAuditLogRow
                    key={log.id}
                    log={log}
                    assistantName={
                      assistantNameById.get(log.subjectId) ??
                      "Deleted assistant"
                    }
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
