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
    "These servers share this user session issuer, and this change moves them to a different upstream authorization server:",
  clear:
    "These servers share this user session issuer, and this change leaves it with more than one provider, so they lose their upstream authorization server until a single provider remains linked:",
  resignin:
    "These servers share this user session issuer, and the client is replaced for all of them. Everyone signed in to them will have to sign in again:",
  client_removed:
    "These gateways share this user session issuer and lose a client their members sign in through. Those members stop working until a client for their provider is linked again:",
};

/** Names the servers that a change on a shared user session issuer would affect. */
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
        Could not check which other servers share this user session issuer. This
        change may alter the upstream authorization server they use.
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
            ? "1 more server you don't have access to shares this user session issuer and may be affected."
            : `${impact.hiddenCount} more servers you don't have access to share this user session issuer and may be affected.`}{" "}
          Ask someone with access to every server on it to make this change.
        </Text>
      )}
      {upstreamMoves && (
        <Text small className="block">
          One user session issuer serves one upstream. To leave them unchanged,
          move this server to a dedicated user session issuer under Sessions
          first.
        </Text>
      )}
    </Alert>
  );
}
