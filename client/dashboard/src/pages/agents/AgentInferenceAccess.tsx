import { useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";

/** Explicit opt-in on the existing agent policy; never changes MCP grants. */
export function AgentInferenceAccess({
  agent,
  grants,
  onChanged,
}: {
  agent: ManagedAgent;
  grants: AgentPolicyGrant[];
  onChanged: () => Promise<unknown>;
}): JSX.Element {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const [projectId, setProjectId] = useState(agent.projectId ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inference = grants.filter(
    (grant) => grant.scope === "inference:execute" && grant.effect === "allow",
  );
  const covered = inference.some(
    ({ selector }) =>
      selector.resourceId === "*" &&
      (!selector.projectId ||
        selector.projectId === "*" ||
        selector.projectId === projectId),
  );
  const change = async (grantId?: string) => {
    if (busy || !agent.permissions.write || (!grantId && !projectId)) return;
    setBusy(true);
    setError(null);
    try {
      if (grantId)
        await sdk.agents.deletePolicyGrant({
          agentPolicyGrantIDForm: { agentId: agent.id, grantId },
        });
      else
        await sdk.agents.createPolicyGrant({
          createAgentPolicyGrantForm: {
            agentId: agent.id,
            effect: "allow",
            scope: "inference:execute",
            selector: { resourceKind: "inference", resourceId: "*", projectId },
          },
        });
      await onChanged();
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : "Could not change inference access.",
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="border-t p-4 space-y-3">
      <h3 className="font-medium">Inference access</h3>
      <p className="text-muted-foreground text-sm">
        Explicitly allow inference in one project. The owner must independently
        have inference:execute there. This does not grant MCP access or change
        existing credentials.
      </p>
      {inference.map((grant) => (
        <div
          key={grant.id}
          className="flex items-center justify-between gap-3 text-sm"
        >
          <span>
            {grant.selector.projectId
              ? (organization.projects.find(
                  (project) => project.id === grant.selector.projectId,
                )?.name ?? grant.selector.projectId)
              : "All projects"}
          </span>
          <Button
            variant="secondary"
            disabled={busy}
            onClick={() => void change(grant.id)}
          >
            Revoke inference
          </Button>
        </div>
      ))}
      <Label htmlFor="agent-inference-project">Project for inference</Label>
      <select
        id="agent-inference-project"
        value={projectId}
        onChange={(event) => setProjectId(event.target.value)}
        disabled={busy}
        className="border-input bg-background block h-9 w-full border px-2"
      >
        <option value="">Select a project</option>
        {organization.projects.map((project) => (
          <option key={project.id} value={project.id}>
            {project.name}
          </option>
        ))}
      </select>
      <Button
        disabled={busy || !projectId || covered}
        onClick={() => void change()}
      >
        {busy ? "Saving…" : "Allow inference in this project"}
      </Button>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
    </section>
  );
}
