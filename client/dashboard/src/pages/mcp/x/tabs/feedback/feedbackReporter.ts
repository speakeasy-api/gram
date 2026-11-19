import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";

/** Who the agent was working for, in the most specific words the report allows. */
export function reporterName(report: McpFeedback): string {
  if (report.userEmail) return report.userEmail;
  // A member who has since left keeps their reports but not their address.
  if (report.userId) return "Former member";
  if (report.externalUserId) return "Signed-in user";
  return "Not signed in";
}

/** The agent's own name for itself, from its handshake with the server. */
export function reportingAgent(report: McpFeedback): string | undefined {
  if (!report.clientName) return undefined;
  return report.clientVersion
    ? `${report.clientName} ${report.clientVersion}`
    : report.clientName;
}
