import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";

/** Who the agent was working for, in the most specific words the report allows. */
export function reporterName(report: McpFeedback): string {
  if (report.userEmail) return report.userEmail;
  // A member who has since left keeps their reports but not their address.
  if (report.userId) return "Former member";
  if (report.externalUserId) return "Signed-in user";
  return "Not signed in";
}

/**
 * The agent's own name for itself, from its handshake with the server. The
 * client chose it, so control and format characters, such as bidi overrides,
 * become spaces rather than change how the name reads.
 */
export function reportingAgent(report: McpFeedback): string | undefined {
  const name = displaySafe(report.clientName);
  if (!name) return undefined;
  const version = displaySafe(report.clientVersion);
  return version ? `${name} ${version}` : name;
}

function displaySafe(value: string | undefined): string {
  return (value ?? "")
    .replace(/[\p{Cc}\p{Cf}]/gu, " ")
    .replace(/\s+/g, " ")
    .trim();
}
