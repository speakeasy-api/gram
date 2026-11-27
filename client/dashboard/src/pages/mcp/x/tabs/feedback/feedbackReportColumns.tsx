import type { Column } from "@/components/ui/Table";
import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";
import {
  ReportReceivedCell,
  ReportSenderCell,
  ReportSeverityCell,
  ReportSummaryCell,
} from "./FeedbackReportCells";

/**
 * The report list's columns. Severity has its own track so the reports line
 * up whether or not they carry one. The report takes the free width and
 * carries the tool it is about beneath it, which leaves the text room on a
 * narrow screen.
 */
export function feedbackReportColumns(
  onSelectTool: (tool: string) => void,
): Column<McpFeedback>[] {
  return [
    {
      key: "severity",
      header: "Severity",
      width: "110px",
      render: (report) => <ReportSeverityCell report={report} />,
    },
    {
      key: "content",
      header: "Report",
      width: "1fr",
      render: (report) => (
        <ReportSummaryCell report={report} onSelectTool={onSelectTool} />
      ),
    },
    {
      key: "userEmail",
      header: "From",
      width: "200px",
      render: (report) => <ReportSenderCell report={report} />,
    },
    {
      key: "createdAt",
      header: "Received",
      width: "100px",
      render: (report) => <ReportReceivedCell report={report} />,
    },
  ];
}
