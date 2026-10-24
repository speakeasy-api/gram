import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import {
  CLIENT_TYPES,
  clientTypeLabel,
  useAllAuthClients,
} from "@/lib/auth-clients";
import {
  AGENT_CREDENTIAL_BLOCK_SCOPE,
  invalidateAgentPolicy,
} from "@/pages/agents/agent-policy-grants";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelectorClientType } from "@gram/client/models/components/agentpolicyselector.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { invalidateAllAgentConnectionBlocks } from "@gram/client/react-query/agentConnectionBlocks.js";
import { invalidateAllAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useBlockAgentOnConnectionMutation } from "@gram/client/react-query/blockAgentOnConnection.js";
import { useRemoveAgentConnectionBlockMutation } from "@gram/client/react-query/removeAgentConnectionBlock.js";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";

const TYPE_PREFIX = "type:";

/**
 * The upstream credentials this agent may not use. An agent manager blocks a
 * kind of credential or one credential everywhere it is used; anyone blocks
 * the agent from their own connections, which needs no say over the agent.
 * Both only take access away, so neither asks what the agent may do.
 */
export function AgentCredentialRestrictions({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  return (
    <>
      {agent.permissions.write ? <ManagerRestrictions agent={agent} /> : null}
      <OwnConnectionBlocks agent={agent} />
    </>
  );
}

function ManagerRestrictions({ agent }: { agent: ManagedAgent }): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const [adding, setAdding] = useState(false);
  const [choice, setChoice] = useState("");
  const grants = useQuery({
    queryKey: ["agent-policy-grants", organization.id, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    retry: false,
    throwOnError: false,
  });
  // Naming one credential needs the organization's list; without it, kinds
  // of credential can still be blocked.
  const clients = useAllAuthClients({ enabled: hasScope("org:read") });
  const names = new Map(
    (clients.data ?? []).map((client) => [client.id, client]),
  );
  const restrictions = (grants.data ?? []).filter(
    (grant) => grant.scope === AGENT_CREDENTIAL_BLOCK_SCOPE,
  );
  const refresh = () =>
    Promise.all([
      invalidateAgentPolicy(queryClient, organization.id, user.id, agent.id),
      invalidateAllAgentsListPolicyGrants(queryClient),
    ]);
  const add = useMutation({
    mutationFn: (value: string) =>
      sdk.agents.createPolicyGrant({
        createAgentPolicyGrantForm: {
          agentId: agent.id,
          effect: "allow",
          scope: AGENT_CREDENTIAL_BLOCK_SCOPE,
          selector: value.startsWith(TYPE_PREFIX)
            ? {
                resourceKind: "mcp_auth_client",
                resourceId: "*",
                clientType: value.slice(
                  TYPE_PREFIX.length,
                ) as AgentPolicySelectorClientType,
              }
            : { resourceKind: "mcp_auth_client", resourceId: value },
        },
      }),
    onSuccess: () => {
      setAdding(false);
      setChoice("");
      toast.success(`${agent.name} can no longer use it`);
      void refresh();
    },
    onError: () =>
      toast.error("The restriction could not be saved. Try again."),
  });
  const remove = useMutation({
    mutationFn: (grant: AgentPolicyGrant) =>
      sdk.agents.deletePolicyGrant({
        deleteAgentPolicyGrantForm: { agentId: agent.id, grantId: grant.id },
      }),
    onSuccess: () => {
      toast.success("Restriction removed");
      void refresh();
    },
    onError: () =>
      toast.error("The restriction could not be removed. Try again."),
  });

  const describe = (grant: AgentPolicyGrant) => {
    const { selector } = grant;
    if (selector.connectionOwner) return "A person's own connection";
    if (selector.resourceId === "*") {
      return selector.clientType
        ? `${clientTypeLabel(selector.clientType)} · every credential of this kind`
        : "Every upstream credential";
    }
    return names.get(selector.resourceId)?.name ?? "One credential";
  };

  return (
    <IdentityPanel
      title="Credentials this agent can't use"
      action={
        <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Block a credential</Button.Text>
        </Button>
      }
      loading={grants.isLoading}
      error={grants.isError}
      onRetry={() => void grants.refetch()}
      footer="A block applies on every server that offers the credential, even if the agent's keys would allow it."
    >
      {restrictions.length === 0 ? (
        <IdentityPanelEmpty>
          This agent may use any credential its keys and owner allow.
        </IdentityPanelEmpty>
      ) : (
        <ul className="divide-y">
          {restrictions.map((grant) => (
            <li
              key={grant.id}
              className="flex items-center justify-between gap-3 px-4 py-3"
            >
              <Text small>{describe(grant)}</Text>
              {grant.selector.connectionOwner ? (
                <Text small muted>
                  Only that person can lift it
                </Text>
              ) : (
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Remove restriction on ${describe(grant)}`}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(grant)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={adding}
        onOpenChange={(open) => {
          setAdding(open);
          if (!open) setChoice("");
        }}
        title={`Block ${agent.name} from a credential`}
        description="The agent can no longer use it on any server, whatever its keys allow."
        confirmLabel="Block"
        isPending={add.isPending}
        confirmDisabled={!choice}
        onConfirm={() => add.mutate(choice)}
      >
        <Select value={choice} onValueChange={setChoice}>
          <SelectTrigger aria-label="Credential to block" className="w-full">
            <SelectValue placeholder="Choose what to block" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel>A kind of credential</SelectLabel>
              {CLIENT_TYPES.map((type) => (
                <SelectItem key={type} value={`${TYPE_PREFIX}${type}`}>
                  {clientTypeLabel(type)}
                </SelectItem>
              ))}
            </SelectGroup>
            {names.size > 0 ? (
              <SelectGroup>
                <SelectLabel>One credential</SelectLabel>
                {[...names.values()].map((client) => (
                  <SelectItem key={client.id} value={client.id}>
                    {client.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            ) : null}
          </SelectContent>
        </Select>
      </ConfirmDialog>
    </IdentityPanel>
  );
}

function OwnConnectionBlocks({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element | null {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [choice, setChoice] = useState("");
  const blocks = useQuery({
    queryKey: ["@gram/client", "agents", "listConnectionBlocks", { all: true }],
    queryFn: ({ signal }) =>
      collectPageItems(
        sdk.agents.listConnectionBlocks({ limit: 100 }, undefined, { signal }),
      ),
    throwOnError: false,
  });
  const sessions = useQuery({
    queryKey: [
      "@gram/client",
      "remoteSessions",
      "list",
      { subjectUrn: `user:${user.id}`, all: true },
    ],
    queryFn: ({ signal }) =>
      collectPageItems(
        sdk.remoteSessions.list(
          { subjectUrn: `user:${user.id}`, limit: 100 },
          undefined,
          { signal },
        ),
      ),
    throwOnError: false,
    retry: false,
  });
  // One connection per upstream OAuth client; a person reconnecting keeps it.
  const connections = new Map<string, string>();
  for (const session of sessions.data ?? []) {
    if (connections.has(session.remoteSessionClientId)) continue;
    connections.set(
      session.remoteSessionClientId,
      session.upstreamDisplayName?.trim() ||
        session.upstreamEmail?.trim() ||
        "A connected account",
    );
  }
  const mine = (blocks.data ?? []).filter(
    (block) => block.agentId === agent.id,
  );
  const blockable = [...connections].filter(
    ([clientId]) =>
      !mine.some((block) => block.remoteSessionClientId === clientId),
  );
  // A connection block is also a grant in the agent's policy.
  const refresh = () =>
    Promise.all([
      invalidateAllAgentConnectionBlocks(queryClient),
      invalidateAgentPolicy(queryClient, organization.id, user.id, agent.id),
      invalidateAllAgentsListPolicyGrants(queryClient),
    ]);
  const add = useBlockAgentOnConnectionMutation({
    onSuccess: () => {
      setAdding(false);
      setChoice("");
      toast.success(`${agent.name} can no longer use that connection`);
      void refresh();
    },
    onError: () => toast.error("The block could not be saved. Try again."),
  });
  const remove = useRemoveAgentConnectionBlockMutation({
    onSuccess: () => {
      toast.success("Block removed");
      void refresh();
    },
    onError: () => toast.error("The block could not be removed. Try again."),
  });

  if (mine.length === 0 && blockable.length === 0 && !blocks.isLoading) {
    return null;
  }

  return (
    <IdentityPanel
      title="Your connections this agent can't use"
      action={
        blockable.length > 0 ? (
          <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Block on a connection</Button.Text>
          </Button>
        ) : undefined
      }
      loading={blocks.isLoading}
      error={blocks.isError}
      onRetry={() => void blocks.refetch()}
      footer="Only you can lift these. They stay in place when you reconnect, and they never affect anyone else's connection."
    >
      {mine.length === 0 ? (
        <IdentityPanelEmpty>
          This agent can use your connections when it acts for you.
        </IdentityPanelEmpty>
      ) : (
        <ul className="divide-y">
          {mine.map((block) => {
            const label =
              connections.get(block.remoteSessionClientId) ??
              "One of your connections";
            return (
              <li
                key={block.id}
                className="flex items-center justify-between gap-3 px-4 py-3"
              >
                <Text small>{label}</Text>
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Lift block on ${label}`}
                  disabled={remove.isPending}
                  onClick={() =>
                    remove.mutate({
                      request: {
                        agentConnectionBlockIDForm: { blockId: block.id },
                      },
                    })
                  }
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </li>
            );
          })}
        </ul>
      )}
      <ConfirmDialog
        open={adding}
        onOpenChange={(open) => {
          setAdding(open);
          if (!open) setChoice("");
        }}
        title={`Block ${agent.name} on one of your connections`}
        description="When this agent acts for you, it can no longer use the connection you choose."
        confirmLabel="Block"
        isPending={add.isPending}
        confirmDisabled={!choice}
        onConfirm={() =>
          add.mutate({
            request: {
              blockAgentOnConnectionForm: {
                agentId: agent.id,
                remoteSessionClientId: choice,
              },
            },
          })
        }
      >
        <Select value={choice} onValueChange={setChoice}>
          <SelectTrigger aria-label="Connection to block" className="w-full">
            <SelectValue placeholder="Choose a connection" />
          </SelectTrigger>
          <SelectContent>
            {blockable.map(([clientId, label]) => (
              <SelectItem key={clientId} value={clientId}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </ConfirmDialog>
    </IdentityPanel>
  );
}
