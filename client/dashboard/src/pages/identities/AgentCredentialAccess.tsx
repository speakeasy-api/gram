import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useAllAuthClients } from "@/lib/useAllAuthClients";
import { CredentialAccessTable } from "@/pages/access/CredentialAccessTable";
import {
  credentialHolderCopy,
  type CredentialRule,
  credentialRuleChanges,
  credentialRuleKey,
  rebaseCredentialRules,
  REPRESENTED_USER,
} from "@/pages/access/credentialAccessModel";
import {
  AGENT_CREDENTIAL_BLOCK_SCOPE,
  invalidateAgentPolicy,
  isAgentCredentialScope,
} from "@/pages/agents/agent-policy-grants";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { invalidateAllAgentConnectionBlocks } from "@gram/client/react-query/agentConnectionBlocks.js";
import { invalidateAllAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useBlockAgentOnConnectionMutation } from "@gram/client/react-query/blockAgentOnConnection.js";
import { useRemoveAgentConnectionBlockMutation } from "@gram/client/react-query/removeAgentConnectionBlock.js";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { authGrantParts } from "./auth-grant-label";
import { AuthGrantLabel } from "./AuthGrantLabel";
import { useAuthGrantNames } from "./useAuthGrantNames";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { useAgentPolicyGrants } from "./useAgentPolicyGrants";

/**
 * Which upstream credentials this agent may use. An agent manager sets that
 * the same way a role does; anyone can block the agent from their own
 * connections, which needs no say over the agent.
 */
export function AgentCredentialAccess({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  return (
    <>
      {agent.permissions.write ? <ManagerCredentials agent={agent} /> : null}
      <OwnConnectionBlocks agent={agent} />
    </>
  );
}

type StoredCredentialRule = CredentialRule & { id: string };

/** A block a person put on their own connection: only they can lift it. */
function isOwnerBlock(grant: AgentPolicyGrant): boolean {
  const owner = grant.selector.connectionOwner;
  return (
    grant.scope === AGENT_CREDENTIAL_BLOCK_SCOPE &&
    !!owner &&
    owner !== REPRESENTED_USER
  );
}

function ManagerCredentials({ agent }: { agent: ManagedAgent }): JSX.Element {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const grants = useAgentPolicyGrants(agent);
  // Naming one credential needs the organization's list; without it, kinds
  // of credential can still be set.
  const clients = useAllAuthClients({ enabled: hasScope("org:read") });
  const policy = grants.data ?? [];
  const stored: StoredCredentialRule[] = useMemo(
    () =>
      (grants.data ?? [])
        .filter((grant) => isAgentCredentialScope(grant.scope))
        .filter((grant) => !isOwnerBlock(grant))
        .map((grant) => ({
          id: grant.id,
          scope: grant.scope as CredentialRule["scope"],
          selector: grant.selector,
        })),
    [grants.data],
  );
  const ownerBlocks = policy.filter(isOwnerBlock).length;
  const names = useAuthGrantNames(stored);
  // What the manager changed, against the rules as they read when they
  // started. Everything they did not touch follows the stored policy.
  const [edit, setEdit] = useState<{
    baseline: CredentialRule[];
    current: CredentialRule[];
  } | null>(null);
  const rules = edit
    ? rebaseCredentialRules({ ...edit, stored })
    : (stored as CredentialRule[]);
  const changes = credentialRuleChanges(stored, rules);
  const dirty = changes.add.length + changes.remove.length > 0;

  const save = useMutation({
    mutationFn: async () => {
      // Additions first, so taking a wide rule apart never leaves a gap.
      for (const rule of changes.add) {
        await sdk.agents.createPolicyGrant({
          createAgentPolicyGrantForm: {
            agentId: agent.id,
            effect: "allow",
            scope: rule.scope,
            selector: rule.selector as AgentPolicySelector,
          },
        });
      }
      const removing = new Set(changes.remove.map(credentialRuleKey));
      for (const rule of stored) {
        if (!removing.has(credentialRuleKey(rule))) continue;
        await sdk.agents.deletePolicyGrant({
          deleteAgentPolicyGrantForm: { agentId: agent.id, grantId: rule.id },
        });
      }
    },
    onSuccess: () => {
      setEdit(null);
      toast.success("Credentials saved");
    },
    onError: (error) =>
      toast.error(
        error instanceof GramError && error.statusCode === 403
          ? "Some changes weren't saved: an agent can only be given credentials both you and its owner can use."
          : "Some changes weren't saved. Try again.",
      ),
    onSettled: () =>
      Promise.all([
        invalidateAgentPolicy(queryClient, organization.id, user.id, agent.id),
        invalidateAllAgentsListPolicyGrants(queryClient),
      ]),
  });

  return (
    <IdentityPanel
      title="Upstream credentials"
      action={
        dirty ? (
          <div className="flex items-center gap-2">
            <Button
              variant="tertiary"
              size="sm"
              disabled={save.isPending}
              onClick={() => setEdit(null)}
            >
              <Button.Text>Discard</Button.Text>
            </Button>
            <Button
              size="sm"
              disabled={save.isPending}
              onClick={() => save.mutate()}
            >
              <Button.Text>{save.isPending ? "Saving…" : "Save"}</Button.Text>
            </Button>
          </div>
        ) : undefined
      }
      loading={grants.isLoading}
      error={grants.isError}
      onRetry={() => void grants.refetch()}
      footer={
        ownerBlocks > 0
          ? `${ownerBlocks} ${ownerBlocks === 1 ? "person has" : "people have"} also blocked this agent from their own connection. Only they can lift that.`
          : "A block wins over every other rule, whatever the agent's keys allow."
      }
      contentClassName="p-4"
    >
      <div className="space-y-3">
        <Text muted small className="block">
          {credentialHolderCopy("agent").description}
        </Text>
        <CredentialAccessTable
          holder="agent"
          bare
          rules={rules}
          onChange={(next) =>
            setEdit((prev) => ({
              baseline: prev?.baseline ?? stored,
              current: next,
            }))
          }
          clients={clients.data ?? (clients.isError ? [] : null)}
          clientsError={clients.isError}
          onRetry={() => void clients.refetch()}
          projects={organization.projects}
          disabled={save.isPending}
          renderOtherRule={(rule) => (
            <AuthGrantLabel
              parts={authGrantParts(
                rule.selector as AgentPolicySelector,
                names,
              )}
            />
          )}
        />
      </div>
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
