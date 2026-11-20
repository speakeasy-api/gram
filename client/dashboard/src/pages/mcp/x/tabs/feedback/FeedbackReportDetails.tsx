import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { CopyButton } from "@/components/ui/CopyButton";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";
import type { ReactNode } from "react";
import { reporterName, reportingAgent } from "./feedbackReporter";

/**
 * One report in full: everything the agent wrote, then the identifiers that
 * tie it to a session, an export record, or a webhook delivery.
 */
export function FeedbackReportDetails({
  report,
}: {
  report: McpFeedback;
}): JSX.Element {
  const agent = reportingAgent(report);
  return (
    <div className="bg-muted/20 space-y-4 border-t px-6 py-5">
      {/* The row above shortens what does not fit; here it is all in full. */}
      <dl className="grid max-w-4xl grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
        <Detail label="What the agent reported">{report.content}</Detail>
        {report.toolName && (
          <Detail label="Tool">
            <span className="font-mono">{report.toolName}</span>
          </Detail>
        )}
        {report.intention && (
          <Detail label="What it was trying to do">{report.intention}</Detail>
        )}
        {report.workaround && (
          <Detail label="What it did instead">{report.workaround}</Detail>
        )}
        <Detail label="From">
          {agent
            ? `${reporterName(report)}, through ${agent}`
            : reporterName(report)}
        </Detail>
        {report.model && (
          <Detail label="Model reported by the agent">{report.model}</Detail>
        )}
      </dl>

      <Collapsible>
        <CollapsibleTrigger className="group text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm">
          <Icon
            name="chevron-right"
            className="size-4 transition-transform group-data-[state=open]:rotate-90"
          />
          Technical details
        </CollapsibleTrigger>
        <CollapsibleContent>
          <dl className="mt-3 grid max-w-4xl grid-cols-1 gap-x-8 gap-y-2 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
            <Identifier label="Report ID" value={report.id} />
            {report.sessionId && (
              <Identifier label="MCP session" value={report.sessionId} />
            )}
            {report.externalUserId && (
              <Identifier
                label="User in your identity provider"
                value={report.externalUserId}
              />
            )}
          </dl>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}

function Detail({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      {/* Agents write this, sometimes at length and with their own line breaks. */}
      <dd className="text-sm break-words whitespace-pre-wrap">{children}</dd>
    </>
  );
}

function Identifier({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1">
        <Text small className="min-w-0 truncate font-mono" title={value}>
          {value}
        </Text>
        <CopyButton text={value} size="xs" tooltip={`Copy ${label}`} />
      </dd>
    </>
  );
}
