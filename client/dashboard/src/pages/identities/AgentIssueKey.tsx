import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  invalidateAgentsListPolicyGrants,
  useAgentsListPolicyGrants,
} from "@gram/client/react-query/agentsListPolicyGrants.js";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys.js";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { CredentialsWizard } from "@/pages/agents/credentials/CredentialsWizard";

/**
 * Issuing another key to an agent, the same way its first key was issued:
 * Agent sign-in, Install, Verify. The key reaches the servers the agent
 * already reaches, with the sign-in already set up for them, so neither is
 * asked again here. Both are properties of the agent, changed on its own
 * page, not of one key.
 */
export function AgentIssueKey({
  agent,
  onDone,
}: {
  agent: ManagedAgent;
  onDone: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  // The wizard seeds the agent's reach from these grants, so it waits for
  // them: issuing on a partial read would make a key that reaches less.
  const grants = useAgentsListPolicyGrants({ agentId: agent.id }, undefined, {
    throwOnError: false,
  });

  const done = () => {
    // The key list on the section this returns to must show the new key.
    void invalidateAllListAPIKeys(queryClient);
    void invalidateAgentsListPolicyGrants(queryClient, [{ agentId: agent.id }]);
    onDone();
  };

  if (grants.isError) {
    return (
      <div role="alert" className="space-y-2">
        <Text>
          The servers this agent reaches could not be loaded, so a key for it
          cannot be issued yet.
        </Text>
        <Button variant="secondary" onClick={() => void grants.refetch()}>
          Try again
        </Button>
      </div>
    );
  }
  if (grants.data === undefined) {
    return <Text muted>Loading this agent's servers…</Text>;
  }
  return (
    <CredentialsWizard
      agent={agent}
      editing
      signInOnly
      grants={grants.data}
      onDone={done}
    />
  );
}
