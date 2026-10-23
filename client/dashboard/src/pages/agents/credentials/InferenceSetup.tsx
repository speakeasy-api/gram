import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useOrgRoutes } from "@/routes";
import { useRBAC } from "@/hooks/useRBAC";
import { Button } from "@/components/ui/Button";
import { TextField } from "./SetupFields";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import {
  inferencePolicyGrants,
  inferenceSetupInstructions,
} from "./inferenceSetupInstructions";
import { useWorkloadSetup } from "./useWorkloadSetup";
import { invalidateAgentPolicy } from "../agent-policy-grants";
import { getServerURL } from "@/lib/utils";

/** Uses the same agent, issuer, admission, and policy records as MCP federation. */
export function InferenceSetup({
  onDone,
  onBusy,
}: {
  onDone: (agentID?: string) => void;
  onBusy?: (busy: boolean) => void;
}): JSX.Element {
  const project = useProject();
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const orgRoutes = useOrgRoutes();
  const canManageRoles = hasScope("org:admin", organization.id);
  const [name, setName] = useState("");
  const [consent, setConsent] = useState(false);
  const [connected, setConnected] = useState(false);
  const agentID = useRef<string | undefined>(undefined);
  const canConfigure =
    hasScope("workload:write") &&
    hasScope("inference:execute", "*", project.id);
  const completionEndpoint = new URL(
    "/chat/completions",
    getServerURL() || window.location.origin,
  ).toString();
  const setup = useWorkloadSetup({
    entry: undefined,
    platformKey: CUSTOM_PLATFORM_KEY,
    agentName: name,
    setupOverride: inferenceSetupInstructions(project.id, completionEndpoint),
    ensureAgent: async () => {
      if (!canConfigure || !consent || !name.trim())
        throw new Error(
          "Name the identity and explicitly allow inference in this project.",
        );
      if (agentID.current) return agentID.current;
      const agent = await sdk.agents.create({
        createAgentForm: {
          name: name.trim(),
          projectId: project.id,
          policyGrants: inferencePolicyGrants(project.id),
        },
      });
      agentID.current = agent.id;
      await invalidateAgentPolicy(
        queryClient,
        organization.id,
        user.id,
        agent.id,
      );
      await queryClient.invalidateQueries({
        queryKey: ["identities", "registered-agents", organization.id],
      });
      return agent.id;
    },
    onConnected: () => setConnected(true),
  });
  useEffect(() => {
    onBusy?.(setup.connecting);
    return () => onBusy?.(false);
  }, [setup.connecting, onBusy]);
  const configuration = setup.steps.filter((step) => step.phase !== "connect");
  return (
    <div className="max-w-3xl space-y-6">
      <p>
        Connect any workload that can obtain a signed JWT. No Kubernetes
        cluster, cloud provider, MCP server, or managed assistant is required.
      </p>
      {!canConfigure && (
        <p role="alert">
          You need workload:write and an explicit inference:execute permission
          for this project. Ask an organization administrator to grant these
          through Access roles; administrator status alone does not enable
          inference.
          {canManageRoles && (
            <>
              {" "}
              As an administrator, you can enable the owner (including yourself)
              without already holding inference access:{" "}
              <a className="underline" href={orgRoutes.access.roles.href()}>
                open Roles &amp; Permissions
              </a>
              , create a custom role, add inference:execute, choose Specific
              projects and {project.name}, then assign the role to the agent
              owner. Keep workload:write separate for operators who configure
              issuer trust. Return here after saving.
            </>
          )}
        </p>
      )}
      <fieldset
        disabled={
          !canConfigure || setup.connecting || connected || !!agentID.current
        }
        className="space-y-5"
      >
        <TextField
          id="inference-agent-name"
          label="Agent identity name"
          value={name}
          onChange={setName}
          placeholder="Inference worker"
          help="A regular agent identity, managed in the existing identities directory."
        />
        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            checked={consent}
            onChange={(event) => setConsent(event.target.checked)}
          />
          Allow inference:execute in {project.name}. No MCP or administration
          permissions.
        </label>
      </fieldset>
      <fieldset
        disabled={!canConfigure || setup.connecting || connected}
        className="space-y-6"
      >
        {configuration.map((step) => (
          <section key={step.id} className="space-y-3">
            <h2 className="text-lg font-medium">{step.title}</h2>
            {setup.render(step)}
          </section>
        ))}
      </fieldset>
      {setup.error && (
        <p role="alert" className="text-destructive">
          {setup.error}
        </p>
      )}
      {!connected ? (
        <Button
          disabled={
            !canConfigure ||
            !project.id ||
            !name.trim() ||
            !consent ||
            setup.connecting ||
            !setup.canConnect ||
            !configuration.every(setup.complete)
          }
          onClick={() => void setup.connect()}
        >
          {setup.connecting ? "Connecting…" : "Create inference identity"}
        </Button>
      ) : (
        <>
          <p role="status">
            Identity and workload admission created. Inference is limited to{" "}
            {project.name} and the owner’s live permissions.
          </p>
          {setup.steps
            .filter((step) => step.phase === "connect")
            .map((step) => (
              <section key={step.id} className="space-y-3">
                <h2 className="text-lg font-medium">{step.title}</h2>
                {setup.render(step)}
              </section>
            ))}
          <Button onClick={() => onDone(agentID.current)}>
            View agent identity
          </Button>
        </>
      )}
    </div>
  );
}
