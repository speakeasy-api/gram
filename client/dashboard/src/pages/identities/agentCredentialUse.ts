import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import { upstreamCredentialPhrase } from "@/components/ui/ActingPrincipal/credential";

/** The auth client type of a person's own connection. */
const PERSONAL_CONNECTION = "remote_session";

/** Auth client types one credential serves for everyone: they act for no one. */
const SHARED_TYPES = new Set(["service_account", "static_credential"]);

/**
 * One server and the credential the agent's calls to it were signed in with.
 * A server reached with two credentials (two people's connections, say) is
 * two rows: each names whom the agent acted for there.
 */
export type AgentCredentialUse = {
  key: string;
  serverLabel: string;
  /**
   * Whose credential it was, in words: "Ada Lovelace's connection", "Shared
   * credential". Undefined when the calls recorded no credential.
   */
  credentialLabel: string | undefined;
  /** The person the agent acted for, for a personal connection. */
  personUserId: string | undefined;
  /** A shared credential signed the calls in, so they acted for no one. */
  shared: boolean;
  calls: number;
};

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * Groups an agent's tool calls by server and by the credential that signed
 * each call in upstream, busiest first. The credential, not the agent's key,
 * says whom the agent acted for: a person's connection acts for that person,
 * and a shared credential acts for no one.
 */
export function summarizeAgentCredentialUse(
  traces: ToolUsageTraceSummary[],
  viewerUserId?: string,
): AgentCredentialUse[] {
  const byKey = new Map<string, AgentCredentialUse>();
  for (const trace of traces) {
    const serverId = trace.mcpServerId || trace.targetId;
    const type = trace.authClientType ?? "";
    const owner =
      type === PERSONAL_CONNECTION ? (trace.connectionOwnerUserId ?? "") : "";
    const key = `${serverId}\u0000${type}\u0000${owner}`;
    const seen = byKey.get(key);
    if (seen) {
      seen.calls += 1;
      continue;
    }
    const phrase = type
      ? upstreamCredentialPhrase(
          {
            type,
            clientId: trace.authClientId,
            ownerUserId: trace.connectionOwnerUserId,
            ownerName: trace.connectionOwnerLabel,
          },
          viewerUserId,
        )
      : undefined;
    byKey.set(key, {
      key,
      serverLabel: trace.targetLabel || serverId,
      // A type this dashboard does not know still signed the call in: say
      // so rather than reporting that nothing was recorded.
      credentialLabel: type
        ? capitalize(phrase ?? "an unrecognized credential")
        : undefined,
      personUserId: owner || undefined,
      shared: SHARED_TYPES.has(type),
      calls: 1,
    });
  }
  return [...byKey.values()].sort((a, b) => b.calls - a.calls);
}
