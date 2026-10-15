import { Alert } from "@/components/ui/Alert";
import { Text } from "@/components/ui/Text";
import {
  groupByImpact,
  type SharedIssuerImpact,
  type SharedIssuerImpactState,
} from "@/lib/remote-identity/model/upstreamRepointing";
import { mcpServerDisplayName } from "@/pages/security/risk-outcome";

const IMPACT_COPY: Record<SharedIssuerImpact, string> = {
  clear:
    "These servers sign in through the provider this change unlinks, so they stop signing anyone in. Linking a client for it again does not restore them: each one has to have its sign-in configured again on its own settings page:",
  client_removed:
    "These gateways share this user session issuer and lose a client their members sign in through. Those members stop working until a client for their provider is linked again:",
};

/** Names the servers that lose a provider client this change unlinks. */
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
        Could not check which other servers sign in through the providers this
        change unlinks. This change may remove their sign-in.
      </Alert>
    );
  }
  const groups = groupByImpact(impact.servers);
  if (groups.length === 0 && impact.hiddenCount === 0) return null;
  const losesUpstream = impact.servers.some(
    (server) => server.impact === "clear",
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
      {losesUpstream && (
        <Text small className="block">
          Each server's provider is configured on that server, so recovering
          them means opening each one and setting its sign-in up again. Their
          users have to sign in again either way.
        </Text>
      )}
    </Alert>
  );
}
