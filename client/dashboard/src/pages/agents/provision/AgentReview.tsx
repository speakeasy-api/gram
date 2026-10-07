import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { type JSX, type ReactNode } from "react";
import { useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { useServerInventory } from "./useServerInventory";
import { AgentGatewayInstall } from "../AgentGatewayInstall";
import { WizardStepHeader, WizardStepper, WizardSummary } from "./WizardChrome";

/**
 * An agent that exists, read through the same five steps that made it. The
 * steps are the questions provisioning asks — what it is called, what it may
 * reach, how it proves who it is, how its runtime was handed the key, and
 * whether that runtime has called — so they are also the questions someone
 * opening the agent later is asking.
 *
 * Every step is complete and every step is a destination; the panels carry
 * the surfaces that edit them.
 */

const STEPS = [
  "Name & scope",
  "Server selection",
  "Credential",
  "Provision",
  "Verify",
];

/** `?step=` names a step, so a link can open the agent on the one it is about. */
const STEP_SLUGS = ["identity", "servers", "credential", "provision", "verify"];

export function AgentReview({
  agent,
  scopeLabel,
  onIssueKey,
  canIssue,
  panels,
}: {
  agent: ManagedAgent;
  scopeLabel: string;
  onIssueKey: () => void;
  canIssue: boolean;
  /** One panel per step, in step order, supplied by the page that owns them. */
  panels: {
    identity: ReactNode;
    servers: ReactNode;
    credential: ReactNode;
    sessions: ReactNode;
  };
}): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const step = Math.max(STEP_SLUGS.indexOf(searchParams.get("step") ?? ""), 0);
  const setStep = (next: number) => {
    const params = new URLSearchParams(searchParams);
    params.set("step", STEP_SLUGS[next] ?? STEP_SLUGS[0]!);
    setSearchParams(params);
  };
  // The rail lists what the agent's stored ceiling reaches, read from the same
  // cache the permissions panel below it writes to.
  const grants = useQuery({
    queryKey: ["agent-policy-grants", organization.id, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    retry: false,
    throwOnError: false,
  });
  const inventory = useServerInventory();
  // One row per server, but its detail gathers every grant on that server:
  // deduplicating by server id alone kept the first grant and dropped the
  // rest, so an agent reaching three named tools was shown reaching one.
  const serverTools = new Map<string, { name: string; tools: Set<string> }>();
  for (const grant of grants.data ?? []) {
    if (grant.selector.resourceKind !== "mcp") continue;
    const id = grant.selector.resourceId ?? "*";
    const name =
      id === "*"
        ? "Every MCP server"
        : (inventory.servers.find(
            (candidate) => candidate.resourceId === id || candidate.id === id,
          )?.name ?? id);
    const entry = serverTools.get(id) ?? { name, tools: new Set<string>() };
    // A grant with no tool reaches all of them, which no list of named tools
    // can narrow.
    entry.tools.add(grant.selector.tool ?? "*");
    serverTools.set(id, entry);
  }
  const serverSummaries = [...serverTools].map(([id, entry]) => ({
    id,
    name: entry.name,
    detail: entry.tools.has("*")
      ? id === "*"
        ? "all servers"
        : "all tools"
      : [...entry.tools].sort().join(", "),
  }));

  // The scopes the agent's stored policy actually grants. A literal here
  // described every agent as mcp:connect, whatever its policy said two panels
  // below.
  const grantedScopes = [
    ...new Set((grants.data ?? []).map((grant) => grant.scope)),
  ].sort();

  const body = () => {
    switch (step) {
      case 0:
        return (
          <div className="space-y-5">
            <WizardStepHeader
              title="Name and scope"
              description="What this identity is called, and how far it may reach."
            />
            {panels.identity}
          </div>
        );
      case 1:
        return (
          <div className="space-y-5">
            <WizardStepHeader
              title="Server selection"
              description="The ceiling for this agent. Each key is narrowed again when it is issued."
            />
            {panels.servers}
          </div>
        );
      case 2:
        return (
          <div className="space-y-5">
            <WizardStepHeader
              title="Credential"
              description="The keys this agent authenticates with. A key is shown once, when it is issued."
            />
            {panels.credential}
          </div>
        );
      case 3:
        return (
          <div className="space-y-5">
            <WizardStepHeader
              title="Connect the agent"
              description="Where the runtime points. Issuing a key is what hands it the setup command."
            />
            <AgentGatewayInstall agentID={agent.id} secret={null} />
            <div className="border-border flex items-center justify-between gap-4 border-t pt-4">
              <Text muted small>
                A setup command carries a single-use code, so it is minted with
                the key rather than stored here.
              </Text>
              <Button disabled={!canIssue} onClick={onIssueKey}>
                Issue a key
              </Button>
            </div>
          </div>
        );
      default:
        return (
          <div className="space-y-5">
            <WizardStepHeader
              title="Verify"
              description="The clients acting as this agent. Revoking one stops it without touching the agent's keys."
            />
            {panels.sessions}
          </div>
        );
    }
  };

  return (
    <div className="space-y-6">
      <WizardStepper
        steps={STEPS}
        current={step}
        // Nothing here is built from a choice not yet made: the agent exists,
        // so every step is a destination.
        forward
        onJump={setStep}
      />
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_260px] lg:gap-16">
        <div className="min-w-0">{body()}</div>
        <div className="lg:order-last">
          <WizardSummary
            step={step + 1}
            stepCount={STEPS.length}
            name={agent.name}
            rows={[
              {
                label: "Owner",
                value:
                  agent.ownerProfile?.displayName ??
                  (agent.ownerUserId === user.id
                    ? user.displayName || user.email
                    : "Unavailable"),
              },
              { label: "Scope", value: scopeLabel },
              {
                label: "Grants",
                value: grantedScopes.length ? (
                  <span className="flex flex-wrap justify-end gap-x-2">
                    {grantedScopes.map((scope) => (
                      <code key={scope} className="text-xs">
                        {scope}
                      </code>
                    ))}
                  </span>
                ) : grants.isPending ? (
                  <Text muted small>
                    Loading…
                  </Text>
                ) : grants.isError ? (
                  <Text muted small>
                    Unavailable
                  </Text>
                ) : (
                  <Text muted small>
                    None yet
                  </Text>
                ),
              },
              { label: "Organization", value: organization.name },
            ]}
            servers={serverSummaries}
          />
        </div>
      </div>
    </div>
  );
}
