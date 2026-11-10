import { useState } from "react";
import { useSearchParams } from "react-router";
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
 * `?view=servers` opens the server picker, for links that came to change
 * which servers there are rather than how they sign in.
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
  const [params] = useSearchParams();
  const grants = useAgentsListPolicyGrants({ agentId: agent.id }, undefined, {
    throwOnError: false,
    enabled: agent.permissions.write,
  });
  // Bumped once the saved grants are read back, so the views start again from
  // what is now stored rather than from the draft that was just saved.
  const [generation, setGeneration] = useState(0);
  // When the last save landed, so the views that reopen after it say so.
  const [savedAt, setSavedAt] = useState<number | undefined>(undefined);

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
        key={generation}
        agent={agent}
        section
        initialEditView={
          params.get("view") === "servers" ? "servers" : "sign-in"
        }
        grants={grants.data}
        savedAt={savedAt}
        onDone={() => {
          toast.success("MCP sign-in saved");
          setSavedAt(Date.now());
          refresh();
          // The views seed from these grants, so they reopen only once the
          // saved ones are read back: a cached pre-save read would show the
          // servers the save just changed, and the next save would undo it.
          void invalidateAgentsListPolicyGrants(queryClient, [
            { agentId: agent.id },
          ]).then(() => setGeneration((value) => value + 1));
        }}
      />
    </div>
  );
}
