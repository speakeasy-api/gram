import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type JSX } from "react";
import {
  agentPolicyGrantsFromDraft,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "../agent-policy-grants";
import { buildRequestedGrants } from "../agent-api-key-grants";
import { discoverKeyServerGrants } from "../agent-key-discovery";
import { agentGatewayURL, mintInstallCommand } from "./gateway";
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

/** Keys live 90 days unless the agent page rotates them sooner. */
const KEY_LIFETIME_DAYS = 90;

type Scope = "project" | "organization";

export function ProvisionWizard({
  agent,
  onDone,
  onBusy,
  verifyPollMs = 5000,
}: {
  /**
   * The agent being provisioned again. While creating one there is no agent:
   * the identity does not exist until the choices are made, so every step
   * ahead is still a draft and none of them can be revisited.
   */
  agent?: ManagedAgent;
  /** Leaves the wizard for the agent it created, or the list if it made none. */
  onDone: (agentID?: string) => void;
  onBusy?: (busy: boolean) => void;
  /**
   * How often the verify step re-reads the key. Only a test shortens it: at
   * the real cadence a test either waits five seconds per tick or races one.
   */
  verifyPollMs?: number;
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
    // Minting counts as busy too: the key exists by then, and leaving while
    // the setup command is still being prepared throws away the one chance
    // to copy a secret that is shown once.
    onBusy?.(provisioning || minting);
  }, [provisioning, minting, onBusy]);

  // Project leads once one is available; an explicit pick always wins. Until
  // a project resolves, project scope would send an empty id, which the server
  // reads as "omitted" — an organization-wide agent created on behalf of
  // someone who asked for a project one.
  // An existing agent's scope is its own and is stored on it: the project
  // being browsed is not necessarily the one it was bound to, and an
  // organization-wide agent holds grants in projects this one is not.
  const scope: Scope = existing
    ? agent.projectId
      ? "project"
      : "organization"
    : (chosenScope ?? (project.id ? "project" : "organization"));
  // The project the servers on offer belong to, and the binding a new agent
  // is created with. Undefined reaches every project in the organization.
  const scopedProjectID = existing
    ? agent.projectId
    : scope === "project"
      ? project.id
      : undefined;
  // Falls back to the id, not the browsed project: an agent bound to a project
  // the caller cannot see would otherwise be labelled with the wrong one.
  const scopeName = scopedProjectID
    ? ((organization.projects ?? []).find(
        (candidate) => candidate.id === scopedProjectID,
      )?.name ?? scopedProjectID)
    : organization.name;

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
    const timer = window.setInterval(() => void poll(), verifyPollMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [step, agentID, verify, sdk, verifyPollMs]);

  const regenerate = (key: string, url: string) => {
    setMinting(true);
    setCommandError(null);
    lastSelfUse.current = Date.now();
    mintInstallCommand(url, key)
      .then((next) => {
        lastSelfUse.current = Date.now();
        setCommand(next);
      })
      .catch((failure: Error) => setCommandError(failure.message))
      .finally(() => setMinting(false));
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
      const draft: AgentPolicyDraft = {
        "mcp:connect": selected.map((entry) => ({
          resourceKind: "mcp" as const,
          resourceId: entry.server.resourceId,
          projectId: entry.server.projectId,
        })),
      };
      // An agent that exists keeps its stored ceiling: this flow issues a key
      // against it rather than rewriting what the agent may ever be delegated.
      const target =
        agent ??
        (await sdk.agents.create({
          createAgentForm: {
            name: name.trim(),
            ...(scopedProjectID ? { projectId: scopedProjectID } : {}),
            policyGrants: agentPolicyGrantsFromDraft(draft),
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

      const controller = new AbortController();
      const delegable = await discoverKeyServerGrants(
        sdk.agents,
        target.id,
        selected.map((entry) => ({
          resourceId: entry.server.resourceId,
          projectId: entry.server.projectId,
          kind: entry.server.kind,
        })),
        controller.signal,
      );
      const requestedGrants = buildRequestedGrants(
        delegable.map((grant) => ({ grant, narrowing: {} })),
      );
      // Discovery answers per server, so it can come back covering only some
      // of them. A key issued on that answer reaches the servers it covers
      // and silently fails on the rest, while every selected server is still
      // listed as provisioned — so none of it is issued until the gap is
      // named.
      const reachable = new Set(
        delegable.map(
          (grant) =>
            `${grant.selector.projectId ?? ""}:${grant.selector.resourceId}`,
        ),
      );
      const excluded = selected.filter(
        (entry) =>
          !reachable.has(
            `${entry.server.projectId}:${entry.server.resourceId}`,
          ),
      );
      if (excluded.length > 0 || requestedGrants.length === 0) {
        throw new Error(
          excluded.length === selected.length
            ? existing
              ? "Nothing on these servers can be delegated to this agent. Check its permissions, or choose other servers."
              : "The agent was created, but nothing on these servers could be delegated to it. Open the agent to review its permissions."
            : `Nothing on ${excluded.map((entry) => entry.server.name).join(", ")} can be delegated to this agent, so a key would not reach ${excluded.length === 1 ? "it" : "them"}. Deselect ${excluded.length === 1 ? "that server" : "those servers"}, or check the agent's permissions.`,
        );
      }

      const expiresAt = new Date(Date.now() + KEY_LIFETIME_DAYS * 86_400_000);
      const issued = await sdk.keys.create({
        createKeyForm: {
          agentId: target.id,
          // Unique per issue, not per day: live key names are unique across
          // the organization, so a date alone makes the second key of the day
          // collide and the issue fail with a 409.
          name: `${name.trim()} key ${new Date().toISOString().replace(/[:.]/g, "-")}`,
          expiresAt,
          delegatedGrantsVersion: 2,
          requestedGrants,
          scopes: [],
        },
      });
      keyID.current = issued.id;
      setSecret(issued.key ?? null);
      setStep(3);
      if (issued.key) regenerate(issued.key, agentGatewayURL(target.id));
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
      rows={[
        { label: "Owner", value: user.displayName || user.email },
        {
          label: "Scope",
          value: scopeName,
        },
        {
          label: "Grants",
          value: <code className="text-xs">mcp:connect</code>,
        },
        { label: "Credential", value: "API key" },
      ]}
      servers={selected.map((entry) => ({
        id: entry.server.id,
        name: entry.server.name,
        detail: "all tools",
      }))}
    />
  );

  const body = () => {
    switch (step) {
      case 0:
        return (
          <div className="space-y-6">
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
            {
              <div className="space-y-2">
                <Label>Scope</Label>
                <Text muted small>
                  {existing
                    ? "Set when the agent was created. Which of its servers this key reaches is the next step."
                    : "Scope decides whether this agent can reach servers in every project or only this one. You narrow the list itself in the next step."}
                </Text>
                <RadioCardGroup
                  value={scope}
                  onValueChange={(value) => {
                    const next = value as Scope;
                    setChosenScope(next);
                    // Narrowing to the project takes the other projects'
                    // servers off the next step. A selection made before the
                    // switch would otherwise stay in the draft and be granted
                    // without ever being shown again.
                    if (next === "project")
                      setSelected((current) =>
                        current.filter(
                          (entry) => entry.server.projectId === project.id,
                        ),
                      );
                  }}
                  disabled={existing}
                  // The density the design system defines for a choice that
                  // is one field of a step rather than a section of its own.
                  size="sm"
                  className="sm:grid-cols-2"
                >
                  {/* One line each: the choice is a scope, not a paragraph. */}
                  <RadioCard
                    value="project"
                    title="Project"
                    // Offered only once there is a project to bind to.
                    disabled={!project.id}
                  >
                    <Text muted small>
                      Servers in {project.name}.
                    </Text>
                  </RadioCard>
                  <RadioCard value="organization" title="Organization">
                    <Text muted small>
                      Servers in every {organization.name} project.
                    </Text>
                  </RadioCard>
                </RadioCardGroup>
              </div>
            }
          </div>
        );
      case 1:
        return (
          <StepServers
            servers={inventory.servers.filter(
              (server) =>
                scopedProjectID === undefined ||
                server.projectId === scopedProjectID,
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
        return (
          <StepProvision
            command={command}
            commandError={commandError}
            minting={minting}
            onRegenerate={() => {
              if (secret && gatewayURL) regenerate(secret, gatewayURL);
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
          />
        );
    }
  };

  const blocked =
    step === 0
      ? !name.trim()
        ? "Name this agent."
        : null
      : step === 1
        ? selected.length === 0
          ? "Select at least one MCP server."
          : null
        : null;

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
            note={
              verify === "connected"
                ? "The agent has called the gateway."
                : "Run the setup from the previous step, then wait for the first call."
            }
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
                : `${selected.length} of ${inventory.servers.length} servers selected.`)
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
        steps={STEPS}
        current={step}
        // While creating, only a step already completed is a destination: the
        // ones ahead are built from choices not yet made. Provisioning an
        // existing agent has no such order, so every step is reachable until
        // the key is issued.
        forward={existing && !secret}
        // Verify watches a key being used, so it has nothing to watch until
        // one is issued. Without this an existing agent could jump straight
        // to it and finish the flow having provisioned nothing.
        onJump={(next) => setStep(secret ? next : Math.min(next, 3))}
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
