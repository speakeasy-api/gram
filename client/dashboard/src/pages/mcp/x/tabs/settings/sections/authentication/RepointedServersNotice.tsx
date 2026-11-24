import { Alert } from "@/components/ui/Alert";
import { Text } from "@/components/ui/Text";
import {
  groupByImpact,
  type SharedIssuerImpact,
  type SharedIssuerImpactState,
} from "@/lib/remote-identity/model/upstreamRepointing";
import { mcpServerDisplayName } from "@/pages/security/risk-outcome";

const IMPACT_COPY: Record<SharedIssuerImpact, string> = {
  repoint:
    "These servers share this MCP sign-in, and this change moves them to a different upstream authorization server:",
  clear:
    "These servers share this MCP sign-in, and this change leaves either no authorization servers or multiple authorization servers linked, so they lose their upstream authorization server until exactly one authorization server is linked:",
  resignin:
    "These servers share this MCP sign-in, and the client is replaced for all of them. Everyone signed in to them will have to sign in again:",
  client_removed:
    "These gateways share this MCP sign-in and lose a client their members sign in through. Those members stop working until a client for their authorization server is linked again:",
};

/** Names the servers that a change on a shared MCP sign-in would affect. */
export function RepointedServersNotice({
  impact,
  projectId,
}: {
  impact: SharedIssuerImpactState;
  /** The target's project; servers elsewhere are labelled with theirs. */
  projectId: string;
}): JSX.Element | null {
  if (impact.refusal !== undefined) {
    return (
      <Alert variant="error" dismissible={false}>
        {impact.refusal}
      </Alert>
    );
  }
  if (impact.failed) {
    return (
      <Alert variant="warning" dismissible={false}>
        Could not check which other servers share this MCP sign-in. This change
        may alter the upstream authorization server they use.
      </Alert>
    );
  }
  const groups = groupByImpact(impact.servers);
  if (groups.length === 0 && impact.hiddenCount === 0) return null;
  const upstreamMoves = impact.servers.some(
    (server) => server.impact === "repoint" || server.impact === "clear",
  );
  return (
    <Alert variant="warning" dismissible={false}>
      {groups.map(([kind, servers]) => (
        <div key={kind}>
          <Text small className="block">
            {IMPACT_COPY[kind]}
          </Text>
          <ul className="my-2 list-disc pl-5">
            {servers.map((server) => (
              <li key={server.id}>
                <Text small>
                  {mcpServerDisplayName(server)}
                  {server.projectId !== projectId
                    ? ` (${server.projectName})`
                    : ""}
                </Text>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {impact.hiddenCount > 0 && (
        <Text small className="block">
          {impact.hiddenCount === 1
            ? "1 more server you don't have access to shares this MCP sign-in and may be affected."
            : `${impact.hiddenCount} more servers you don't have access to share this MCP sign-in and may be affected.`}{" "}
          Ask someone with access to every server on it to make this change.
        </Text>
      )}
      {upstreamMoves && (
        <Text small className="block">
          One MCP sign-in serves one upstream. To leave them unchanged, first
          move this server to its own MCP sign-in.
        </Text>
      )}
    </Alert>
  );
}
