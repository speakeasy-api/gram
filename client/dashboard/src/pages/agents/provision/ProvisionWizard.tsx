import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
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
  onDone,
  onBusy,
}: {
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

  const [step, setStep] = useState(0);
  const [name, setName] = useState("");
  const [scope, setScope] = useState<Scope>("project");
  const [selected, setSelected] = useState<ServerSelection[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [provisioning, setProvisioning] = useState(false);

  // What provisioning produced. The secret is held only while this page is
  // open; nothing writes it to storage or to the query cache.
  const [agentID, setAgentID] = useState<string | null>(null);
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
      const agent = await sdk.agents.create({
        createAgentForm: {
          name: name.trim(),
          ...(scopedProjectID ? { projectId: scopedProjectID } : {}),
          policyGrants: agentPolicyGrantsFromDraft(draft),
        },
      });
      setAgentID(agent.id);
      void invalidateAgentPolicy(
        queryClient,
        organization.id,
        user.id,
        agent.id,
      );
      void queryClient.invalidateQueries({
        queryKey: ["managed-agents", organization.id],
      });

      const controller = new AbortController();
      const delegable = await discoverKeyServerGrants(
        sdk.agents,
        agent.id,
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
      if (requestedGrants.length === 0) {
        throw new Error(
          "The agent was created, but nothing on these servers could be delegated to it. Open the agent to review its permissions.",
        );
      }

      const expiresAt = new Date(Date.now() + KEY_LIFETIME_DAYS * 86_400_000);
      const issued = await sdk.keys.create({
        createKeyForm: {
          agentId: agent.id,
          name: `${name.trim()} key`,
          expiresAt,
          delegatedGrantsVersion: 2,
          requestedGrants,
          scopes: [],
        },
      });
      keyID.current = issued.id;
      setSecret(issued.key ?? null);
      setStep(3);
      if (issued.key) regenerate(issued.key, agentGatewayURL(agent.id));
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
          value: scope === "project" ? project.name : organization.name,
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
                autoFocus
              />
              <Text muted small>
                Shown in audit logs and session lists. You can rename it later.
              </Text>
            </div>
            {
              <div className="space-y-2">
                <Label>Scope</Label>
                <Text muted small>
                  Scope decides whether this agent can reach servers in every
                  project or only this one. You narrow the list itself in the
                  next step.
                </Text>
                <RadioCardGroup
                  value={scope}
                  onValueChange={(value) => setScope(value as Scope)}
                  className="sm:grid-cols-2"
                >
                  <RadioCard value="project" title="Project">
                    <Text muted small>
                      {project.name} only. Reaches servers in this project.
                    </Text>
                  </RadioCard>
                  <RadioCard value="organization" title="Organization">
                    <Text muted small>
                      All projects in {organization.name}. Reaches servers
                      across projects.
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
              <RadioCard value="api-key" title="API key">
                <Text muted small>
                  A scoped key, delivered by a one-line setup script or copied
                  manually. Works in any runtime.
                </Text>
                <span className="text-muted-foreground mt-2 block font-mono text-xs">
                  Long-lived · revocable
                </span>
              </RadioCard>
              <RadioCard
                value="workload"
                disabled
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
                <span className="text-muted-foreground mt-2 block font-mono text-xs">
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
            note="Creating the agent is recorded in the organization audit log."
            onBack={() => setStep(1)}
            primary={
              <Button disabled={provisioning} onClick={() => void provision()}>
                {provisioning ? "Creating…" : "Create agent"}
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
                ? "Name and scope can be changed later."
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
        // Once the agent exists its choices are made; going back would offer
        // edits this screen can no longer apply.
        onJump={(index) => {
          if (!agentID) setStep(index);
        }}
      />
      {error && (
        <p
          role="alert"
          className="border-destructive text-destructive border p-3 text-sm"
        >
          {error}
        </p>
      )}
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div>{body()}</div>
        <div className="lg:order-last">{summary}</div>
      </div>
      {footer()}
    </div>
  );
}
