import { toast } from "sonner";

import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { CredentialsWizard } from "@/pages/agents/credentials/CredentialsWizard";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  invalidateAgentsListPolicyGrants,
  useAgentsListPolicyGrants,
} from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useQueryClient } from "@tanstack/react-query";

/**
 * The agent's MCP sign-in tab: which servers it calls, and how Speakeasy
 * signs in to each. The same views the setup wizard shows for these two
 * questions, without walking the rest of setup again.
 *
 * It always opens on the servers and their sign-in. The picker opens only
 * from its own button: links land on the tab, never on a dialog.
 */
export function AgentMcpSignIn({
  agent,
  refresh,
}: {
  agent: ManagedAgent;
  /** Re-reads the agent, whose "needs refresh" state a save can clear. */
  refresh: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const grants = useAgentsListPolicyGrants({ agentId: agent.id }, undefined, {
    throwOnError: false,
    enabled: agent.permissions.write,
  });

  if (!agent.permissions.write) {
    return (
      <InlineEmptyState
        icon="lock"
        heading="Only people who can change this agent can see its MCP sign-in"
        // The servers come from its policy, which only they can read, so no
        // other tab lists them either.
        description="Ask one of them to change its servers or reconnect a server it can no longer reach."
      />
    );
  }
  if (grants.isError) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the servers this agent calls"
        description="Nothing can be changed until they are known."
        action={
          <Button variant="secondary" onClick={() => void grants.refetch()}>
            Try again
          </Button>
        }
      />
    );
  }
  if (grants.data === undefined) return <SkeletonTable />;

  return (
    <div className="bg-card border-border border p-6">
      <CredentialsWizard
        agent={agent}
        section
        grants={grants.data}
        // Each change saves as it is made. The views keep what they saved as
        // their baseline, so the next save diffs against it; the agent and
        // its grants are read again for the rest of the page.
        onDone={() => {
          toast.success("MCP sign-in saved");
          refresh();
          void invalidateAgentsListPolicyGrants(queryClient, [
            { agentId: agent.id },
          ]);
        }}
      />
    </div>
  );
}
