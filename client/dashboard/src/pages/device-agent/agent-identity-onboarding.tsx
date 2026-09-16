import { CodeBlock } from "@/components/code";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { HumanizeDateTime } from "@/lib/dates";
import { getServerURL } from "@/lib/utils";
import {
  agentKeyExpiry,
  buildRequestedGrants,
  DEFAULT_AGENT_KEY_EXPIRY_DAYS,
  validateAgentAPIKeyName,
} from "@/pages/agents/agent-api-key-grants";
import {
  DEMO_UNAVAILABLE_REASON,
  useAgentIdentityRollout,
  useAgentManagementAvailability,
} from "@/pages/agents/agent-management-availability";
import { invalidateAgentPolicy } from "@/pages/agents/agent-policy-grants";
import { useOrgRoutes } from "@/routes";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useCreateAgentMutation } from "@gram/client/react-query/createAgent.js";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import {
  hashKey,
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";
import { Link } from "react-router";

import {
  buildAgentIdentitySnippet,
  controlPlaneURLError,
  deviceAgentPolicyGrants,
  ISSUE_AGENT_KEY_MUTATION,
  issuedKeyFor,
  lastSeenKey,
  MANAGED_CONFIG_PATH,
  missingPolicyGrants,
  selectDeviceAgentKeyGrants,
  undelegableScopesMessage,
  type AgentRunMode,
} from "./agent-identity-setup";

const security = { sessionHeaderGramSession: "" };
const LINK_CLASS = "underline underline-offset-2 hover:text-foreground";
const STATUS_POLL_MS = 15_000;

type IssuedKey = {
  agentId: string;
  projectId: string;
  keyId: string;
  value: string;
};

export default function DeviceAgentAgentIdentity(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <AgentIdentityOnboarding />
    </RequireScope>
  );
}

function AgentIdentityOnboarding() {
  const { sessionReason, isDemo } = useAgentManagementAvailability();
  // Both rollouts must be on before any agent binding or key issuance.
  const rolloutReason = useAgentIdentityRollout();
  const unavailable =
    sessionReason ?? (isDemo ? DEMO_UNAVAILABLE_REASON : rolloutReason);
  return (
    <SettingsPage
      title="Agent identity setup"
      description="Run the device agent on a machine no person uses — a CI runner, cloud sandbox, or shared host — as a dedicated agent identity instead of a shared email."
    >
      {unavailable ? <Text muted>{unavailable}</Text> : <OnboardingSteps />}
    </SettingsPage>
  );
}

function OnboardingSteps() {
  const organization = useOrganization();
  const [agent, setAgent] = useState<ManagedAgent | null>(null);
  const [projectId, setProjectId] = useState(
    organization.projects[0]?.id ?? "",
  );
  const [grantedFor, setGrantedFor] = useState<string | null>(null);
  const [issued, setIssued] = useState<IssuedKey | null>(null);
  const grantTarget = agent ? `${agent.id}:${projectId}` : null;
  const granted = grantTarget !== null && grantedFor === grantTarget;
  const issuedKey = issuedKeyFor(issued, agent?.id, projectId);
  // Switching mid-issuance would unmount IssueKey and orphan the new secret.
  const issuing = useIsMutating({ mutationKey: ISSUE_AGENT_KEY_MUTATION }) > 0;
  // A key is minted for one agent and project; changing either starts over.
  const selectAgent = (next: ManagedAgent | null) => {
    if (issuing) return;
    setAgent(next);
    setGrantedFor(null);
    setIssued(null);
  };
  const changeProject = (id: string) => {
    if (issuing) return;
    setProjectId(id);
    setIssued(null);
  };

  return (
    <>
      <Step
        n={1}
        title="Choose an agent"
        description="The identity the device agent runs as. Its activity is attributed to this agent, not to a person."
      >
        <AgentPicker agent={agent} onChange={selectAgent} disabled={issuing} />
      </Step>
      {agent && (
        <Step
          n={2}
          title="Grant device agent access"
          description="Adds the permissions the device agent needs to this agent's policy: plugin sync and hook ingestion for the organization, and read access to the project hook events land in."
        >
          <GrantAccess
            agent={agent}
            projectId={projectId}
            onProjectChange={changeProject}
            granted={granted}
            onGranted={() => setGrantedFor(grantTarget)}
            disabled={issuing}
          />
        </Step>
      )}
      {agent && granted && (
        <Step
          n={3}
          title="Create an API key"
          description={`Delegates exactly those permissions to a new key that expires in ${DEFAULT_AGENT_KEY_EXPIRY_DAYS} days. Manage or revoke it from the agent's page.`}
        >
          <IssueKey
            agent={agent}
            projectId={projectId}
            issued={issuedKey !== null}
            onIssued={setIssued}
          />
        </Step>
      )}
      {issuedKey && (
        <Step
          n={4}
          title="Install on the machine"
          description="Run this on the machine. No email, version, or checksum to maintain: the install script resolves the latest stable release and verifies it."
        >
          <SetupSnippet agentKey={issuedKey.value} />
        </Step>
      )}
      {agent && issuedKey && (
        <Step
          n={5}
          title="Check-in status"
          description="When any of this agent's keys was last used, by the device agent or anything else holding the key."
        >
          <CheckInStatus agent={agent} awaitingKeyId={issuedKey.keyId} />
        </Step>
      )}
    </>
  );
}

function Step({
  n,
  title,
  description,
  children,
}: {
  n: number;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>{`${n}. ${title}`}</SettingsSection.Title>
        <SettingsSection.Description>{description}</SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>{children}</SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

export function AgentPicker({
  agent,
  onChange,
  disabled = false,
}: {
  agent: ManagedAgent | null;
  onChange: (agent: ManagedAgent | null) => void;
  disabled?: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const nameId = useId();
  const [mode, setMode] = useState<"existing" | "new">("existing");
  const [name, setName] = useState("");
  const agents = useQuery({
    queryKey: ["managed-agents", organization.id, "list"],
    queryKeyHashFn: hashKey,
    queryFn: ({ signal }) => sdk.agents.list(undefined, undefined, { signal }),
    throwOnError: false,
    retry: false,
  });
  const active = (agents.data ?? []).filter((a) => a.lifecycle === "active");
  const create = useCreateAgentMutation({
    onSuccess: (created) => {
      void queryClient.invalidateQueries({
        queryKey: ["managed-agents", organization.id],
      });
      setName("");
      setMode("existing");
      onChange(created);
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <SegmentedControl
        value={mode}
        disabled={disabled}
        onChange={(next) => {
          setMode(next);
          onChange(null);
        }}
        options={[
          { value: "existing", label: "Existing agent" },
          { value: "new", label: "New agent" },
        ]}
      />
      {mode === "existing" ? (
        <ExistingAgentSelect
          agents={active}
          isLoading={agents.isLoading}
          isError={agents.isError}
          value={agent}
          onChange={onChange}
          disabled={disabled}
        />
      ) : (
        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            const trimmed = name.trim();
            if (!trimmed) return;
            create.mutate({
              request: { createAgentForm: { name: trimmed } },
            });
          }}
        >
          <Label htmlFor={nameId}>Agent name</Label>
          <Input
            id={nameId}
            value={name}
            onChange={setName}
            placeholder="CI runners"
            maxLength={120}
            disabled={disabled || create.isPending}
          />
          <Text muted small>
            You will be the owner.
          </Text>
          <div>
            <Button
              type="submit"
              disabled={disabled || !name.trim() || create.isPending}
            >
              {create.isPending ? "Creating…" : "Create agent"}
            </Button>
          </div>
          {create.isError && (
            <Text role="alert">
              {create.error.message || "Unable to create agent."}
            </Text>
          )}
        </form>
      )}
    </div>
  );
}

function ExistingAgentSelect({
  agents,
  isLoading,
  isError,
  value,
  onChange,
  disabled,
}: {
  agents: ManagedAgent[];
  isLoading: boolean;
  isError: boolean;
  value: ManagedAgent | null;
  onChange: (agent: ManagedAgent | null) => void;
  disabled: boolean;
}) {
  const agentsHref = useOrgRoutes().agents.href();
  if (isLoading) return <Text muted>Loading agents…</Text>;
  if (isError) return <Text role="alert">Unable to load agents.</Text>;
  if (agents.length === 0)
    return <Text muted>No active agents yet. Create one instead.</Text>;
  return (
    <div className="flex max-w-md flex-col gap-2">
      <Select
        value={value?.id ?? ""}
        disabled={disabled}
        onValueChange={(id) =>
          onChange(agents.find((agent) => agent.id === id) ?? null)
        }
      >
        <SelectTrigger aria-label="Agent">
          <SelectValue placeholder="Select an agent" />
        </SelectTrigger>
        <SelectContent>
          {agents.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Text muted small>
        Only active agents are listed. Manage agents under{" "}
        <Link to={agentsHref} className={LINK_CLASS}>
          Agents
        </Link>
        .
      </Text>
    </div>
  );
}

export function GrantAccess({
  agent,
  projectId,
  onProjectChange,
  granted,
  onGranted,
  disabled = false,
}: {
  agent: ManagedAgent;
  projectId: string;
  onProjectChange: (id: string) => void;
  granted: boolean;
  onGranted: () => void;
  disabled?: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const grant = useMutation({
    mutationFn: async () => {
      const stored = await sdk.agents.listPolicyGrants({ agentId: agent.id });
      const missing = missingPolicyGrants(
        stored,
        deviceAgentPolicyGrants(projectId),
      );
      try {
        for (const form of missing) {
          await sdk.agents.createPolicyGrant({
            createAgentPolicyGrantForm: { agentId: agent.id, ...form },
          });
        }
      } finally {
        await invalidateAgentPolicy(
          queryClient,
          organization.id,
          user.id,
          agent.id,
        );
      }
    },
    onSuccess: onGranted,
  });
  const canWrite = agent.permissions.write;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex max-w-md flex-col gap-2">
        <Label>Hooks project</Label>
        <Select
          value={projectId}
          onValueChange={onProjectChange}
          disabled={disabled || grant.isPending}
        >
          <SelectTrigger aria-label="Hooks project">
            <SelectValue placeholder="Select a project" />
          </SelectTrigger>
          <SelectContent>
            {organization.projects.map((project) => (
              <SelectItem key={project.id} value={project.id}>
                {project.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Text muted small>
        Grants <code>org:device_agent_sync</code>, <code>org:hooks_ingest</code>
        , and <code>project:read</code> on this project. Grants the agent
        already holds are left as they are.
      </Text>
      {!canWrite && (
        <Text role="alert">
          You do not have permission to change this agent's permissions.
        </Text>
      )}
      <div>
        <Button
          onClick={() => grant.mutate()}
          disabled={
            disabled || !canWrite || !projectId || granted || grant.isPending
          }
        >
          {granted ? "Access granted" : "Grant access"}
        </Button>
      </div>
      {grant.isError && (
        <Text role="alert">
          {grant.error.message ||
            "Could not update the agent's permissions. Some grants may have been added; retrying skips those."}
        </Text>
      )}
    </div>
  );
}

export function IssueKey({
  agent,
  projectId,
  issued,
  onIssued,
}: {
  agent: ManagedAgent;
  projectId: string;
  issued: boolean;
  onIssued: (key: IssuedKey) => void;
}): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const nameId = useId();
  const [name, setName] = useState(
    () => `device-agent-${new Date().toISOString().slice(0, 10)}`,
  );
  const createKey = useCreateAPIKeyMutation({ gcTime: 0, retry: false });
  const issue = useMutation({
    mutationKey: ISSUE_AGENT_KEY_MUTATION,
    mutationFn: async () => {
      // Refuse before minting: a key for a plaintext control plane is unusable.
      const urlError = controlPlaneURLError(getServerURL());
      if (urlError) throw new Error(urlError);
      const keyName = validateAgentAPIKeyName(name);
      // Fresh read: the candidates must reflect the grants just added.
      const delegable = await queryClient.fetchQuery({
        queryKey: [
          "agent-delegable-grants",
          organization.id,
          user.id,
          agent.id,
        ],
        queryFn: ({ signal }) =>
          sdk.agents.listDelegableGrants({ agentId: agent.id }, undefined, {
            signal,
          }),
        staleTime: 0,
      });
      const { selections, missingScopes } = selectDeviceAgentKeyGrants(
        delegable,
        deviceAgentPolicyGrants(projectId),
      );
      if (missingScopes.length)
        throw new Error(undelegableScopesMessage(missingScopes));
      const { expiresAt, reason } = agentKeyExpiry(
        DEFAULT_AGENT_KEY_EXPIRY_DAYS,
        "",
        Date.now(),
      );
      if (reason) throw new Error(reason);
      const key = await createKey.mutateAsync({
        security,
        request: {
          createKeyForm: {
            agentId: agent.id,
            name: keyName,
            expiresAt,
            delegatedGrantsVersion: 2,
            requestedGrants: buildRequestedGrants(selections),
            scopes: [],
          },
        },
      });
      if (!key.key) throw new Error("The server returned no key secret.");
      return { agentId: agent.id, projectId, keyId: key.id, value: key.key };
    },
    onSuccess: (key) => {
      onIssued(key);
      void queryClient.invalidateQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });
  const canIssue =
    agent.permissions.authorize &&
    agent.lifecycle === "active" &&
    !agent.ownerReassignmentRequiredAt;

  return (
    <form
      className="flex max-w-md flex-col gap-2"
      onSubmit={(event) => {
        event.preventDefault();
        issue.mutate();
      }}
    >
      <Label htmlFor={nameId}>Key name</Label>
      <Input
        id={nameId}
        value={name}
        onChange={setName}
        disabled={issue.isPending || issued}
      />
      {!canIssue && (
        <Text role="alert">
          Issuance requires an active agent with a valid owner and credential
          authorization.
        </Text>
      )}
      <div>
        <Button
          type="submit"
          disabled={!canIssue || issue.isPending || issued || !name.trim()}
        >
          {issue.isPending ? "Creating…" : "Create API key"}
        </Button>
      </div>
      {issue.isError && (
        <Text role="alert">
          {issue.error.message || "Could not create the API key."}
        </Text>
      )}
    </form>
  );
}

function SetupSnippet({ agentKey }: { agentKey: string }) {
  const [mode, setMode] = useState<AgentRunMode>("ephemeral");
  const setupHref = useOrgRoutes().deviceAgent.href();
  const serverURL = getServerURL();
  const urlError = controlPlaneURLError(serverURL);
  if (urlError)
    return (
      <Alert variant="error">
        <AlertTitle>No install snippet for this control plane</AlertTitle>
        <AlertDescription>{urlError}</AlertDescription>
      </Alert>
    );
  const snippet = buildAgentIdentitySnippet({ agentKey, mode, serverURL });
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-3">
        <SegmentedControl
          value={mode}
          onChange={setMode}
          options={[
            {
              value: "ephemeral",
              label: "Ephemeral",
              tooltip: "Sandboxes and CI: reconcile once per session",
            },
            {
              value: "service",
              label: "Persistent",
              tooltip: "Long-running hosts: install the background service",
            },
          ]}
        />
      </div>
      <CodeBlock language="bash">{snippet}</CodeBlock>
      <Text muted small>
        For Linux hosts. The agent reads its identity from{" "}
        <code>{MANAGED_CONFIG_PATH}</code>.{" "}
        {mode === "ephemeral"
          ? "Run the snippet from the environment's setup script, and rerun speakeasyd sync --once at the start of each session; nothing keeps enforcement reconciled after it exits."
          : "The service keeps plugins and hooks reconciled for as long as the machine runs."}
      </Text>
      <Text muted small>
        macOS agent hosts use the signed <code>.pkg</code> with{" "}
        <code>managed.json</code> deployed through your MDM; see{" "}
        <Link to={setupHref} className={LINK_CLASS}>
          Device Agent setup
        </Link>
        .
      </Text>
      <Alert variant="warning">
        <AlertTitle>Copy this now</AlertTitle>
        <AlertDescription>
          The key is shown only once. The config is readable only by root and
          the account that runs the agent, but that account and anything running
          as it, including AI agents, can still read the key. To rotate, create
          a new key here, redeploy, and revoke the old one.
        </AlertDescription>
      </Alert>
    </div>
  );
}

function CheckInStatus({
  agent,
  awaitingKeyId,
}: {
  agent: ManagedAgent;
  awaitingKeyId?: string;
}) {
  const organization = useOrganization();
  const agentHref = `${useOrgRoutes().agents.href()}?id=${encodeURIComponent(agent.id)}`;
  const keys = useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organization.id, key]),
    enabled: agent.permissions.authorize,
    // Poll only until the new key's first use is recorded.
    refetchInterval: (query) =>
      awaitingKeyId &&
      !query.state.data?.keys.find((key) => key.id === awaitingKeyId)
        ?.lastAccessedAt
        ? STATUS_POLL_MS
        : false,
    retry: false,
    throwOnError: false,
  });
  const agentKeys = keys.data?.keys ?? [];
  const seen = lastSeenKey(agentKeys);
  const awaitedKey = agentKeys.find((key) => key.id === awaitingKeyId);

  if (!agent.permissions.authorize)
    return (
      <Text muted>
        You do not have permission to view this agent's API keys.
      </Text>
    );
  if (keys.isLoading) return <Text muted>Loading…</Text>;
  if (keys.isError)
    return <Text role="alert">Could not load this agent's API keys.</Text>;
  return (
    <div className="flex flex-col gap-2">
      {seen?.lastAccessedAt ? (
        <Text>
          Key last used{" "}
          <time
            title={seen.lastAccessedAt.toLocaleString()}
            dateTime={seen.lastAccessedAt.toISOString()}
          >
            <HumanizeDateTime date={seen.lastAccessedAt} />
          </time>{" "}
          (<code>{seen.name}</code>).
        </Text>
      ) : (
        <Text muted>No key has been used yet.</Text>
      )}
      {awaitingKeyId && !awaitedKey?.lastAccessedAt && (
        <Text muted small>
          Waiting for the new key&apos;s first use. This page checks every{" "}
          {STATUS_POLL_MS / 1000} seconds.
        </Text>
      )}
      <Text muted small>
        <Link to={agentHref} className={LINK_CLASS}>
          Manage this agent
        </Link>
      </Text>
    </div>
  );
}
