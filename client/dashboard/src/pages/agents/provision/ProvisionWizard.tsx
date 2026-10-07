import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type JSX } from "react";
import {
  agentPolicyGrantsFromDraft,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "../agent-policy-grants";
import { buildRequestedGrants } from "../agent-api-key-grants";
import { discoverKeyServerGrants } from "../agent-key-discovery";
import {
  agentPurposeFromPolicy,
  DEVICE_AGENT_SCOPES,
  deviceAgentPolicyGrants,
  deviceAgentPurposeBlocked,
  missingPolicyGrants,
  policyConnectsToMCP,
  selectDeviceAgentKeyGrants,
  undelegableScopesMessage,
  type AgentPurpose,
  type DeviceAgentRunMode,
} from "./device-agent";
import {
  StepDeviceAgentProject,
  StepProvisionDeviceAgent,
} from "./DeviceAgentSteps";
import {
  agentGatewayURL,
  mintInstallCommand,
  type InstallRequest,
} from "./gateway";
import { StepProvision } from "./StepProvision";
import { StepServers, type ServerSelection } from "./StepServers";
import { StepVerify, type VerifyState } from "./StepVerify";
import { useServerInventory } from "./useServerInventory";
import {
  WizardFooter,
  WizardStepHeader,
  WizardStepper,
  WizardSummary,
} from "./WizardChrome";

/**
 * Creating an agent identity here is only ever a means to provisioning one
 * somewhere else, so the flow runs to that end: name it, choose what it may
 * reach, pick how it proves who it is, hand the runtime its credential, and
 * watch for the first call.
 *
 * The identity is created at the end of the choices, not the start: everything
 * before Provision is still a draft the user can abandon without leaving a
 * half-made agent behind.
 */

const STEPS = [
  "Name & scope",
  "Server selection",
  "Credential",
  "Provision",
  "Verify",
];

/** A device agent reaches no servers; its second step picks a project. */
const DEVICE_AGENT_STEPS = [
  "Name & scope",
  "Project",
  "Credential",
  "Provision",
  "Verify",
];

/** Keys live 90 days unless the agent page rotates them sooner. */
const KEY_LIFETIME_DAYS = 90;

type Scope = "project" | "organization";

function installRequestFor(
  purpose: AgentPurpose,
  mode: DeviceAgentRunMode,
): InstallRequest {
  switch (purpose) {
    case "device-agent":
      return { flavor: "device_agent", mode };
    case "mcp":
      return { flavor: "mcp" };
  }
}

/** Key names are unique per organization, so a random suffix follows the date. */
function keyName(agentName: string): string {
  const issued = new Date().toISOString().slice(0, 16).replace("T", " ");
  const suffix = crypto.randomUUID().slice(0, 6);
  return `${agentName} key ${issued} ${suffix}`;
}

function verifyNote(
  purpose: AgentPurpose | undefined,
  connected: boolean,
): string {
  if (!connected)
    return "Run the setup from the previous step, then wait for the first call.";
  return purpose === "device-agent"
    ? "The device agent has checked in."
    : "The agent has called the gateway.";
}

export function ProvisionWizard({
  agent,
  initialPurpose,
  onDone,
  onBusy,
}: {
  /**
   * The agent being provisioned again. While creating one there is no agent:
   * the identity does not exist until the choices are made, so every step
   * ahead is still a draft and none of them can be revisited.
   */
  agent?: ManagedAgent;
  /** Preselects a new agent's purpose. An existing agent's comes from its policy. */
  initialPurpose?: AgentPurpose;
  /** Leaves the wizard for the agent it created, or the list if it made none. */
  onDone: (agentID?: string) => void;
  onBusy?: (busy: boolean) => void;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const project = useProject();
  const { user } = useSession();
  const queryClient = useQueryClient();
  const inventory = useServerInventory();

  // Provisioning an agent that already exists: its name and scope are settled,
  // so the flow is only about which servers this key reaches and how the key
  // is delivered. Every step is a destination because none of them creates
  // anything until the key is issued.
  const existing = agent !== undefined;

  const [step, setStep] = useState(0);
  const [name, setName] = useState(agent?.name ?? "");
  // null until the person picks, so the default can follow the project as it
  // resolves. A useState initializer would run once, before useProject has an
  // id, and strand the form on organization scope.
  const [chosenScope, setChosenScope] = useState<Scope | null>(null);
  const [selected, setSelected] = useState<ServerSelection[]>([]);
  const [chosenPurpose, setChosenPurpose] = useState<AgentPurpose>(
    initialPurpose ?? "mcp",
  );
  const [hooksProjectID, setHooksProjectID] = useState(
    project.id || organization.projects[0]?.id || "",
  );
  const [mode, setMode] = useState<DeviceAgentRunMode>("ephemeral");
  const [error, setError] = useState<string | null>(null);
  const [provisioning, setProvisioning] = useState(false);

  // What provisioning produced. The secret is held only while this page is
  // open; nothing writes it to storage or to the query cache.
  const [agentID, setAgentID] = useState<string | null>(agent?.id ?? null);
  const [secret, setSecret] = useState<string | null>(null);
  const [command, setCommand] = useState<string | null>(null);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  const [verify, setVerify] = useState<VerifyState>("waiting");
  const [firstCallAt, setFirstCallAt] = useState<Date | undefined>();
  const keyID = useRef<string | null>(null);
  // Minting a setup command authenticates with the key, so the key's own
  // access time moves before any runtime has called. Only an access after the
  // last thing this page did counts as the agent's first call.
  const lastSelfUse = useRef<number>(0);

  useEffect(() => {
    onBusy?.(provisioning);
  }, [provisioning, onBusy]);

  // An existing agent's purpose comes from its stored policy (same cache as the
  // permissions panel).
  const storedPolicy = useQuery({
    queryKey: ["agent-policy-grants", organization.id, agent?.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent!.id }, undefined, {
        signal,
      }),
    enabled: existing,
    retry: false,
    throwOnError: false,
  });
  const purpose: AgentPurpose | undefined = existing
    ? storedPolicy.data && agentPurposeFromPolicy(storedPolicy.data)
    : chosenPurpose;

  // Only an org admin can delegate the device agent's organization scopes.
  const { hasScope } = useRBAC();
  const deviceAgentFlag = useFeatureFlag(FEATURE_FLAGS.deviceAgent);
  const deviceAgentBlocked = deviceAgentPurposeBlocked({
    deviceAgentEnabled: deviceAgentFlag.status === "enabled",
    isOrgAdmin: hasScope("org:admin"),
  });

  // Project leads once one is available; an explicit pick always wins. Until
  // a project resolves, project scope would send an empty id, which the server
  // reads as "omitted" — an organization-wide agent created on behalf of
  // someone who asked for a project one.
  const scope: Scope = chosenScope ?? (project.id ? "project" : "organization");
  const scopedProjectID = scope === "project" ? project.id : undefined;

  const gatewayURL = agentID ? agentGatewayURL(agentID) : "";

  // The verify step reads the key itself: a key with an access time is a
  // runtime that reached the gateway and was admitted.
  useEffect(() => {
    if (step !== 4 || !agentID || verify === "connected") return;
    let cancelled = false;
    const poll = async () => {
      try {
        const listed = await sdk.keys.list({ agentId: agentID });
        const issued = listed.keys.find((key) => key.id === keyID.current);
        if (
          !cancelled &&
          issued?.lastAccessedAt &&
          issued.lastAccessedAt.getTime() > lastSelfUse.current
        ) {
          setVerify("connected");
          setFirstCallAt(issued.lastAccessedAt);
        }
      } catch {
        // A failed poll says nothing about the agent; the next one retries.
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [step, agentID, verify, sdk]);

  const regenerate = (key: string, url: string, request: InstallRequest) => {
    setMinting(true);
    setCommandError(null);
    lastSelfUse.current = Date.now();
    mintInstallCommand(url, key, request)
      .then((next) => {
        lastSelfUse.current = Date.now();
        setCommand(next);
      })
      .catch((failure: Error) => setCommandError(failure.message))
      .finally(() => setMinting(false));
  };

  /** An MCP key carries everything delegable on the chosen servers. */
  const mcpKeyGrants = async (
    targetID: string,
  ): Promise<AgentPolicyGrantForm[]> => {
    const controller = new AbortController();
    const delegable = await discoverKeyServerGrants(
      sdk.agents,
      targetID,
      selected.map((entry) => ({
        resourceId: entry.server.resourceId,
        projectId: entry.server.projectId,
        kind: entry.server.kind,
      })),
      controller.signal,
    );
    return buildRequestedGrants(
      delegable.map((grant) => ({ grant, narrowing: {} })),
    );
  };

  /**
   * A device agent key carries exactly sync, hooks, and read on the project.
   * An existing agent gets any missing grants first, and is refused if its
   * policy reaches MCP servers.
   */
  const deviceAgentKeyGrants = async (
    target: ManagedAgent,
  ): Promise<AgentPolicyGrantForm[]> => {
    const required = deviceAgentPolicyGrants(hooksProjectID);
    if (existing) {
      const stored = await sdk.agents.listPolicyGrants({ agentId: target.id });
      if (policyConnectsToMCP(stored))
        throw new Error(
          "This agent can connect to MCP servers, so it cannot run the device agent. Create a separate agent for that.",
        );
      const missing = missingPolicyGrants(stored, required);
      if (missing.length > 0 && !target.permissions.write)
        throw new Error(
          "You do not have permission to change this agent's permissions.",
        );
      try {
        for (const form of missing)
          await sdk.agents.createPolicyGrant({
            createAgentPolicyGrantForm: { agentId: target.id, ...form },
          });
      } finally {
        if (missing.length > 0)
          void invalidateAgentPolicy(
            queryClient,
            organization.id,
            user.id,
            target.id,
          );
      }
    }
    const delegable = await sdk.agents.listDelegableGrants({
      agentId: target.id,
    });
    const { selections, missingScopes } = selectDeviceAgentKeyGrants(
      delegable,
      required,
    );
    if (missingScopes.length > 0)
      throw new Error(undelegableScopesMessage(missingScopes));
    return buildRequestedGrants(selections);
  };

  /**
   * Create the agent, discover what it may delegate, issue its first key, and
   * mint the setup command. Each step depends on the one before it, so a
   * failure stops here and says which part did not happen.
   */
  const provision = async () => {
    if (provisioning) return;
    setProvisioning(true);
    setError(null);
    try {
      if (!purpose)
        throw new Error("This agent's permissions are still loading.");
      // Existing agents too: a new key delegates the same organization scopes.
      if (purpose === "device-agent" && deviceAgentBlocked)
        throw new Error(deviceAgentBlocked);
      const draft: AgentPolicyDraft = {
        "mcp:connect": selected.map((entry) => ({
          resourceKind: "mcp" as const,
          resourceId: entry.server.resourceId,
          projectId: entry.server.projectId,
        })),
      };
      // An agent that exists keeps its stored ceiling: this flow issues a key
      // against it rather than rewriting what the agent may ever be delegated.
      // A device agent is organization-wide, like its grants.
      const target =
        agent ??
        (await sdk.agents.create({
          createAgentForm: {
            name: name.trim(),
            ...(purpose === "mcp" && scopedProjectID
              ? { projectId: scopedProjectID }
              : {}),
            policyGrants:
              purpose === "device-agent"
                ? deviceAgentPolicyGrants(hooksProjectID)
                : agentPolicyGrantsFromDraft(draft),
          },
        }));
      setAgentID(target.id);
      if (!agent) {
        void invalidateAgentPolicy(
          queryClient,
          organization.id,
          user.id,
          target.id,
        );
        void queryClient.invalidateQueries({
          queryKey: ["managed-agents", organization.id],
        });
      }

      const requestedGrants =
        purpose === "device-agent"
          ? await deviceAgentKeyGrants(target)
          : await mcpKeyGrants(target.id);
      if (requestedGrants.length === 0) {
        throw new Error(
          existing
            ? "Nothing on these servers can be delegated to this agent. Check its permissions, or choose other servers."
            : "The agent was created, but nothing on these servers could be delegated to it. Open the agent to review its permissions.",
        );
      }

      const expiresAt = new Date(Date.now() + KEY_LIFETIME_DAYS * 86_400_000);
      const issued = await sdk.keys.create({
        createKeyForm: {
          agentId: target.id,
          name: keyName(name.trim()),
          expiresAt,
          delegatedGrantsVersion: 2,
          requestedGrants,
          scopes: [],
        },
      });
      keyID.current = issued.id;
      setSecret(issued.key ?? null);
      setStep(3);
      if (issued.key)
        regenerate(
          issued.key,
          agentGatewayURL(target.id),
          installRequestFor(purpose, mode),
        );
    } catch (failure) {
      const conflict =
        failure instanceof GramError && failure.statusCode === 409
          ? failure.message
          : null;
      setError(
        conflict ??
          (failure instanceof Error
            ? failure.message
            : "Could not provision this agent. Try again."),
      );
    } finally {
      setProvisioning(false);
    }
  };

  const summary = (
    <WizardSummary
      step={step + 1}
      stepCount={STEPS.length}
      name={name}
      rows={
        purpose === "device-agent"
          ? [
              { label: "Owner", value: user.displayName || user.email },
              { label: "Used for", value: "Device agent" },
              {
                label: "Project",
                value:
                  organization.projects.find(
                    (candidate) => candidate.id === hooksProjectID,
                  )?.name ?? "—",
              },
              {
                label: "Grants",
                value: (
                  <span className="flex flex-col items-end">
                    {DEVICE_AGENT_SCOPES.map((grant) => (
                      <code key={grant} className="text-xs">
                        {grant}
                      </code>
                    ))}
                  </span>
                ),
              },
              { label: "Credential", value: "API key" },
            ]
          : [
              { label: "Owner", value: user.displayName || user.email },
              {
                label: "Scope",
                value: scope === "project" ? project.name : organization.name,
              },
              {
                label: "Grants",
                value: <code className="text-xs">mcp:connect</code>,
              },
              { label: "Credential", value: "API key" },
            ]
      }
      servers={
        purpose === "device-agent"
          ? undefined
          : selected.map((entry) => ({
              id: entry.server.id,
              name: entry.server.name,
              detail: "all tools",
            }))
      }
    />
  );

  const body = () => {
    switch (step) {
      case 0:
        return (
          <div className="space-y-6">
            {storedPolicy.isError && (
              // Without the policy, the agent's purpose is unknown.
              <div
                role="alert"
                className="border-destructive text-destructive flex items-center justify-between gap-4 border p-3 text-sm"
              >
                Could not read this agent&apos;s permissions.
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={storedPolicy.isFetching}
                  onClick={() => void storedPolicy.refetch()}
                >
                  Retry
                </Button>
              </div>
            )}
            <WizardStepHeader
              title="Name and scope"
              description="What this identity is called, and how far it may reach."
            />
            <div className="max-w-xl space-y-2">
              <Label htmlFor="agent-name">Name</Label>
              <Input
                id="agent-name"
                value={name}
                onChange={setName}
                placeholder="Release Bot"
                maxLength={120}
                // An existing agent's name and scope are its own; this flow
                // issues it a key and does not rewrite its identity.
                disabled={existing}
                autoFocus={!existing}
              />
              <Text muted small>
                {existing
                  ? "Rename this agent from its own page."
                  : "Shown in audit logs and session lists. You can rename it later."}
              </Text>
            </div>
            <div className="space-y-2">
              <Label>Used for</Label>
              <Text muted small>
                {existing
                  ? "Set when the agent was created. An agent is used for one of these, never both."
                  : "What this agent's key is for. An agent is used for one of these, never both."}
              </Text>
              <RadioCardGroup
                value={purpose ?? ""}
                onValueChange={(value) =>
                  setChosenPurpose(value as AgentPurpose)
                }
                disabled={existing}
                className="sm:grid-cols-2"
              >
                <RadioCard value="mcp" title="MCP servers" className="p-3">
                  <Text muted small>
                    Calls MCP servers through one gateway endpoint.
                  </Text>
                </RadioCard>
                <RadioCard
                  value="device-agent"
                  title="Device agent"
                  className="p-3"
                  disabled={deviceAgentBlocked !== null}
                >
                  <Text muted small>
                    {deviceAgentBlocked ||
                      "Runs the device agent on a Linux host no person uses."}
                  </Text>
                </RadioCard>
              </RadioCardGroup>
            </div>
            {purpose === "mcp" && (
              <div className="space-y-2">
                <Label>Scope</Label>
                <Text muted small>
                  {existing
                    ? "Set when the agent was created. Which of its servers this key reaches is the next step."
                    : "Scope decides whether this agent can reach servers in every project or only this one. You narrow the list itself in the next step."}
                </Text>
                <RadioCardGroup
                  value={scope}
                  onValueChange={(value) => setChosenScope(value as Scope)}
                  disabled={existing}
                  className="sm:grid-cols-2"
                >
                  {/* One line each: the choice is a scope, not a paragraph. */}
                  <RadioCard
                    value="project"
                    title="Project"
                    className="p-3"
                    // Offered only once there is a project to bind to.
                    disabled={!project.id}
                  >
                    <Text muted small>
                      Servers in {project.name}.
                    </Text>
                  </RadioCard>
                  <RadioCard
                    value="organization"
                    title="Organization"
                    className="p-3"
                  >
                    <Text muted small>
                      Servers in every {organization.name} project.
                    </Text>
                  </RadioCard>
                </RadioCardGroup>
              </div>
            )}
          </div>
        );
      case 1:
        if (purpose === "device-agent")
          return (
            <StepDeviceAgentProject
              projectId={hooksProjectID}
              onChange={setHooksProjectID}
            />
          );
        return (
          <StepServers
            servers={inventory.servers.filter(
              (server) =>
                scope === "organization" || server.projectId === project.id,
            )}
            isLoading={inventory.isLoading}
            isError={inventory.isError}
            onRetry={inventory.refetch}
            selected={selected}
            onChange={setSelected}
          />
        );
      case 2:
        return (
          <div className="space-y-6">
            <WizardStepHeader
              title="Credential"
              description="How the agent proves who it is when it connects."
            />
            <RadioCardGroup
              value="api-key"
              onValueChange={() => undefined}
              className="sm:grid-cols-2"
            >
              <RadioCard value="api-key" title="API key" className="p-3">
                <Text muted small>
                  A scoped key, delivered by a one-line setup script or copied
                  by hand. Works in any runtime.
                </Text>
                <span className="text-muted-foreground mt-1 block font-mono text-xs">
                  Long-lived · revocable
                </span>
              </RadioCard>
              <RadioCard
                value="workload"
                disabled
                className="p-3"
                title={
                  <span className="flex items-center gap-2">
                    Workload identity
                    <Badge variant="neutral" size="sm">
                      Not yet available
                    </Badge>
                  </span>
                }
              >
                <Text muted small>
                  Federate a token the platform already issues — GitHub Actions,
                  Kubernetes, SPIFFE, GCP. No Gram secret is created.
                </Text>
                <span className="text-muted-foreground mt-1 block font-mono text-xs">
                  Short-lived tokens · no refresh token
                </span>
              </RadioCard>
            </RadioCardGroup>
          </div>
        );
      case 3:
        if (purpose === "device-agent")
          return (
            <StepProvisionDeviceAgent
              command={command}
              commandError={commandError}
              minting={minting}
              mode={mode}
              canRegenerate={!!secret}
              onModeChange={(next) => {
                setMode(next);
                // Each code renders one script, so a new mode needs a new code.
                if (secret && gatewayURL)
                  regenerate(
                    secret,
                    gatewayURL,
                    installRequestFor("device-agent", next),
                  );
              }}
              onRegenerate={() => {
                if (secret && gatewayURL)
                  regenerate(
                    secret,
                    gatewayURL,
                    installRequestFor("device-agent", mode),
                  );
              }}
            />
          );
        return (
          <StepProvision
            command={command}
            commandError={commandError}
            minting={minting}
            onRegenerate={() => {
              if (secret && gatewayURL)
                regenerate(secret, gatewayURL, installRequestFor("mcp", mode));
            }}
            secret={secret}
            gatewayURL={gatewayURL}
            serverCount={selected.length}
          />
        );
      default:
        return (
          <StepVerify
            state={verify}
            firstCallAt={firstCallAt}
            gatewayURL={gatewayURL}
            purpose={purpose}
          />
        );
    }
  };

  const blocked = blockedReason();

  function blockedReason(): string | null {
    switch (step) {
      case 0:
        if (!name.trim()) return "Name this agent.";
        if (storedPolicy.isError)
          return "Could not read this agent's permissions.";
        if (!purpose) return "Reading this agent's permissions…";
        if (purpose === "device-agent") return deviceAgentBlocked;
        return null;
      case 1:
        if (purpose === "device-agent")
          return hooksProjectID ? null : "Choose a project.";
        return selected.length === 0 ? "Select at least one MCP server." : null;
      default:
        return null;
    }
  }

  const selectionNote =
    purpose === "device-agent"
      ? "Hook events are recorded in the chosen project."
      : `${selected.length} of ${inventory.servers.length} servers selected.`;

  const footer = () => {
    switch (step) {
      case 2:
        return (
          <WizardFooter
            note={
              existing
                ? "Issuing a key is recorded in the organization audit log."
                : "Creating the agent is recorded in the organization audit log."
            }
            onBack={() => setStep(1)}
            primary={
              <Button disabled={provisioning} onClick={() => void provision()}>
                {provisioning
                  ? existing
                    ? "Issuing…"
                    : "Creating…"
                  : existing
                    ? "Issue key"
                    : "Create agent"}
              </Button>
            }
          />
        );
      case 3:
        return (
          <WizardFooter
            note="The API key can be rotated or revoked from the agent page."
            primary={
              <Button onClick={() => setStep(4)}>
                Continue to verification
              </Button>
            }
          />
        );
      case 4:
        return (
          <WizardFooter
            note={verifyNote(purpose, verify === "connected")}
            onBack={() => setStep(3)}
            primary={
              <Button onClick={() => onDone(agentID ?? undefined)}>
                {verify === "connected" ? "Done" : "Skip for now"}
              </Button>
            }
          />
        );
      default:
        return (
          <WizardFooter
            note={
              blocked ??
              (step === 0
                ? existing
                  ? "This agent's identity is settled; this flow issues it a key."
                  : "Name and scope can be changed later."
                : selectionNote)
            }
            onBack={step > 0 ? () => setStep(step - 1) : undefined}
            primary={
              <Button disabled={!!blocked} onClick={() => setStep(step + 1)}>
                Continue
              </Button>
            }
          />
        );
    }
  };

  return (
    <div className="space-y-6">
      <WizardStepper
        steps={purpose === "device-agent" ? DEVICE_AGENT_STEPS : STEPS}
        current={step}
        // While creating, only a step already completed is a destination: the
        // ones ahead are built from choices not yet made. Provisioning an
        // existing agent has no such order, so every step is reachable until
        // the key is issued.
        forward={existing && !secret}
        onJump={setStep}
      />
      {error && (
        <p
          role="alert"
          className="border-destructive text-destructive border p-3 text-sm"
        >
          {error}
        </p>
      )}
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_260px] lg:gap-16">
        <div>{body()}</div>
        <div className="lg:order-last">{summary}</div>
      </div>
      {footer()}
    </div>
  );
}
