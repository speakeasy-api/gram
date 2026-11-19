import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { dateTimeFormatters, formatRelativeTime } from "@/lib/dates";
import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";
import { reporterName, reportingAgent } from "./feedbackReporter";
import { feedbackSeverity } from "./feedbackSeverity";

/** Nothing for a report with no severity: a comment or praise is not a problem. */
export function ReportSeverityCell({
  report,
}: {
  report: McpFeedback;
}): JSX.Element | null {
  const severity = feedbackSeverity(report.severity);
  if (!severity) return null;
  return (
    <SimpleTooltip tooltip={severity.meaning}>
      <Badge variant={severity.badge}>{severity.label}</Badge>
    </SimpleTooltip>
  );
}

/**
 * The start of what the agent wrote, with the tool it is about beneath it.
 * Pressing the tool narrows the list to that tool.
 */
export function ReportSummaryCell({
  report,
  onSelectTool,
}: {
  report: McpFeedback;
  onSelectTool: (tool: string) => void;
}): JSX.Element {
  const { toolName } = report;
  return (
    <div className="min-w-0">
      <Text small className="line-clamp-2 break-words">
        {report.content}
      </Text>
      {toolName && (
        <SimpleTooltip tooltip={`Show reports about ${toolName}`}>
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground mt-1 block max-w-full truncate text-left font-mono text-xs hover:underline"
            onClick={(event) => {
              // The row itself opens the report; this narrows the list.
              event.stopPropagation();
              onSelectTool(toolName);
            }}
          >
            {toolName}
          </button>
        </SimpleTooltip>
      )}
    </div>
  );
}

/** The person the agent was working for, and the agent beneath them. */
export function ReportSenderCell({
  report,
}: {
  report: McpFeedback;
}): JSX.Element {
  const name = reporterName(report);
  const agent = reportingAgent(report);
  return (
    <div className="min-w-0">
      <Text small className="block truncate" title={name}>
        {name}
      </Text>
      {agent && (
        <Text small muted className="block truncate" title={agent}>
          {agent}
        </Text>
      )}
    </div>
  );
}

/** How long ago the report arrived, with the exact time on hover. */
export function ReportReceivedCell({
  report,
}: {
  report: McpFeedback;
}): JSX.Element {
  return (
    <Text
      small
      muted
      className="whitespace-nowrap"
      title={dateTimeFormatters.full.format(report.createdAt)}
    >
      {formatRelativeTime(report.createdAt)}
    </Text>
  );
}
