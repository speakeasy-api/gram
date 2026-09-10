import {
  DangerSettingsSection,
  FormPage,
  ResourceListPage,
  SettingsPage,
  SettingsSection,
} from "@/components/page-templates";
import { Avatar, AvatarImage, AvatarFallback } from "@/components/ui/Avatar";
import { Table, type Column } from "@/components/ui/Table";
import { useSdkClient } from "@/contexts/Sdk";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

import { useAgentsDeleteMutation } from "@gram/client/react-query/agentsDelete.js";
import { useAgentsResumeMutation } from "@gram/client/react-query/agentsResume.js";
import { useAgentsRevokeMutation } from "@gram/client/react-query/agentsRevoke.js";
import { useAgentsSuspendMutation } from "@gram/client/react-query/agentsSuspend.js";
import { useCreateAgentMutation } from "@gram/client/react-query/createAgent.js";
import { useRenameAgentMutation } from "@gram/client/react-query/renameAgent.js";
import { hashKey, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Bot, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { AgentAPIKeys } from "./AgentAPIKeys";
import {
  agentPolicyGrantsFromDraft,
  invalidateAgentPolicy,
  type AgentPolicyDraft,
} from "./agent-policy-grants";
import { AgentPolicyEditor } from "./AgentPolicyEditor";
import { AgentPolicySection } from "./AgentPolicySection";
import { ManagedAgentSessions } from "./ManagedAgentSessions";

export default function AgentsPage(): JSX.Element {
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const [searchParams, setSearchParams] = useSearchParams();
  const agentID = searchParams.get("id");
  const isDemo = organization.slug === DEMO_ORG_SLUG;
  const hasUnsupportedSession =
    session.organizationOverride || Boolean(session.impersonatorEmail);
  const hasScopeOverride =
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) !== null;

  if (hasUnsupportedSession || hasScopeOverride) {
    return (
      <FormPage
        title="Agent management unavailable"
        description={
          hasUnsupportedSession
            ? "Support and impersonated sessions cannot manage agents. Switch to an ordinary Gram session."
            : "Agent management is disabled while an RBAC scope override is active."
        }
      >
        {null}
      </FormPage>
    );
  }

  if (!agentID && searchParams.get("create") === "true") {
    return (
      <CreateAgent
        // Switching any of these would otherwise submit a name and permissions
        // chosen in a different context.
        key={`${organization.id}:${session.user.id}:${isDemo}`}
        disabled={isDemo}
        onCreated={(id) => {
          setSearchParams({ id });
        }}
      />
    );
  }

  if (isDemo) {
    return (
      <FormPage
        title="Agent management unavailable"
        description="Agent management is unavailable in the shared demo because it requires active organization membership."
      >
        {null}
      </FormPage>
    );
  }

  if (!agentID) {
    return (
      <AgentList
        onSelect={(id) => setSearchParams({ id })}
        onCreate={() => setSearchParams({ create: "true" })}
      />
    );
  }

  return (
    <AgentSettings
      key={`${organization.id}-${agentID}`}
      agentID={agentID}
      onBack={() => setSearchParams({})}
    />
  );
}

function AgentList({
  onSelect,
  onCreate,
}: {
  onSelect: (id: string) => void;
  onCreate: () => void;
}) {
  // Ownership is an independent authorization path. Do not gate this query on RBAC.
  const organization = useOrganization();
  const sdk = useSdkClient();
  const agents = useQuery({
    queryKey: ["managed-agents", organization.id, "list"],
    queryKeyHashFn: hashKey,
    queryFn: ({ signal }) => sdk.agents.list(undefined, undefined, { signal }),
    throwOnError: false,
    retry: false,
  });
  const [search, setSearch] = useState("");
  const rows = (agents.data ?? []).filter((agent) =>
    agent.name.toLowerCase().includes(search.trim().toLowerCase()),
  );
  const columns: Column<ManagedAgent>[] = [
    {
      key: "name",
      header: "Name",
      render: (agent) => (
        <Button variant="tertiary" onClick={() => onSelect(agent.id)}>
          {agent.name}
        </Button>
      ),
    },
    {
      key: "ownerUserId",
      header: "Owner",
      render: (agent) => <AgentOwner agent={agent} />,
    },
    {
      key: "lifecycle",
      header: "Status",
      render: (agent) => <LifecycleBadge lifecycle={agent.lifecycle} />,
    },
  ];
  return (
    <ResourceListPage
      title="Agents"
      description="Agents visible to you."
      primaryAction={<Button onClick={onCreate}>Create agent</Button>}
      search={{
        value: search,
        onChange: setSearch,
        placeholder: "Search agents",
      }}
      isLoading={agents.isLoading}
      isEmpty={!agents.isError && (agents.data ?? []).length === 0}
      empty={{
        icon: "bot",
        heading: "No agents yet",
        description: "Create an agent to give it a dedicated identity.",
      }}
      onRefresh={() => void agents.refetch()}
      isRefreshing={agents.isFetching}
    >
      {agents.isError ? (
        <Text role="alert">Unable to load agents. Try again.</Text>
      ) : rows.length === 0 ? (
        <Text>No matching agents</Text>
      ) : (
        <Table columns={columns} data={rows} rowKey={(agent) => agent.id} />
      )}
    </ResourceListPage>
  );
}

function AgentOwner({ agent }: { agent: ManagedAgent }) {
  const { user } = useSession();
  const profile =
    agent.ownerProfile ??
    (agent.ownerUserId === user.id
      ? { displayName: user.displayName || user.email, photoUrl: user.photoUrl }
      : undefined);
  const name = profile?.displayName || "Unavailable owner";
  return (
    <span className="flex items-center gap-2">
      <Avatar>
        <AvatarImage src={profile?.photoUrl} alt="" />
        <AvatarFallback>{name.slice(0, 1).toUpperCase()}</AvatarFallback>
      </Avatar>
      <span>{name}</span>
    </span>
  );
}

function CreateAgent({
  disabled,
  onCreated,
}: {
  disabled: boolean;
  onCreated: (id: string) => void;
}) {
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<AgentPolicyDraft>({});
  const [withoutPermissions, setWithoutPermissions] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const organization = useOrganization();
  const { user } = useSession();
  const queryClient = useQueryClient();
  const create = useCreateAgentMutation({
    onSuccess: (agent) => {
      void invalidateAgentPolicy(
        queryClient,
        organization.id,
        user.id,
        agent.id,
      );
      toast.success("Agent created");
      onCreated(agent.id);
    },
    // The create is one transaction, so a failure leaves no agent behind and
    // the draft is still exactly what to retry.
    onError: (error) =>
      setError(
        error.message ||
          "Unable to create agent. Your name and permissions have been kept.",
      ),
  });

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    if (!trimmedName) return;
    const policyGrants = agentPolicyGrantsFromDraft(draft);
    if (policyGrants.length === 0 && !withoutPermissions) {
      setError(
        "Add permissions or explicitly confirm Create without permissions.",
      );
      return;
    }
    setError(null);
    create.mutate({
      request: {
        createAgentForm: {
          name: trimmedName,
          ...(policyGrants.length > 0 ? { policyGrants } : {}),
        },
      },
    });
  }

  return (
    <FormPage
      title="Agents"
      description="Create a first-class nonhuman principal with ownership and an independent lifecycle."
    >
      <form onSubmit={onSubmit} className="border bg-card p-6">
        <div className="mb-6 flex items-start gap-4">
          <div className="border p-2">
            <Bot className="size-5" aria-hidden="true" />
          </div>
          <div>
            <Text className="font-medium">Create an agent</Text>
            <Text muted small className="mt-1">
              {disabled
                ? "Agent management is unavailable in the shared demo because it requires active organization membership."
                : "You will be the owner. Ownership gives you intrinsic setup access without creating a reusable permission grant."}
            </Text>
          </div>
        </div>
        <div className="space-y-2">
          <Label htmlFor="agent-name">Agent name</Label>
          <Input
            id="agent-name"
            value={name}
            onChange={setName}
            placeholder="Release assistant"
            maxLength={120}
            disabled={disabled}
            autoFocus
          />
        </div>
        <div className="mt-6 space-y-2">
          <Label>Permissions</Label>
          <Text muted small>
            The most this agent may ever be delegated. An agent with no
            permissions can hold API keys, but they will not authorize anything.
            Each key is narrowed again at issuance, against your live
            permissions and the owner's.
          </Text>
          <AgentPolicyEditor
            draft={draft}
            onChange={setDraft}
            disabled={disabled || create.isPending}
          />
        </div>
        <label className="mt-4 flex items-start gap-2">
          <Checkbox
            checked={withoutPermissions}
            onCheckedChange={(value) => setWithoutPermissions(!!value)}
            disabled={disabled || create.isPending}
            className="mt-0.5"
          />
          <span className="min-w-0 flex-1">
            <span className="block text-sm font-medium">
              Create without permissions
            </span>
            <Text as="span" small muted className="block">
              Only needed if you leave the list above empty. You can add
              permissions later.
            </Text>
          </span>
        </label>
        {error && (
          <p role="alert" className="mt-4 text-sm">
            {error}
          </p>
        )}
        <div className="mt-6 flex justify-end">
          <Button
            type="submit"
            disabled={disabled || !name.trim() || create.isPending}
          >
            <Button.LeftIcon>
              <Plus className="size-4" />
            </Button.LeftIcon>
            <Button.Text>
              {create.isPending ? "Creating\u2026" : "Create agent"}
            </Button.Text>
          </Button>
        </div>
      </form>
    </FormPage>
  );
}

function AgentSettings({
  agentID,
  onBack,
}: {
  agentID: string;
  onBack: () => void;
}) {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const sdk = useSdkClient();
  const agentQuery = useQuery({
    queryKey: ["managed-agents", organization.id, "detail", agentID],
    queryKeyHashFn: hashKey,
    queryFn: ({ signal }) =>
      sdk.agents.get({ id: agentID }, undefined, { signal }),
    throwOnError: false,
    retry: false,
  });

  if (agentQuery.isLoading) {
    return (
      <SettingsPage title="Agent" description="Loading agent settings…">
        <SkeletonTable />
      </SettingsPage>
    );
  }

  if (!agentQuery.data || agentQuery.isError) {
    return (
      <FormPage
        title="Agent unavailable"
        description="This agent does not exist or you do not have permission to read it."
      >
        <Button variant="secondary" onClick={onBack}>
          Back to agents
        </Button>
      </FormPage>
    );
  }

  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ["managed-agents", organization.id],
    });
  };

  return (
    <SettingsPage
      title={agentQuery.data.name}
      description="Manage this agent's identity and lifecycle."
      primaryAction={
        <Button variant="secondary" onClick={onBack}>
          <Button.LeftIcon>
            <ArrowLeft className="size-4" />
          </Button.LeftIcon>
          <Button.Text>All agents</Button.Text>
        </Button>
      }
    >
      <AgentIdentity
        key={`identity-${agentQuery.data.id}`}
        agent={agentQuery.data}
        refresh={refresh}
      />
      <AgentPolicySection
        key={`policy-${agentQuery.data.id}`}
        agent={agentQuery.data}
      />
      <AgentAPIKeys agent={agentQuery.data} />
      <ManagedAgentSessions
        key={`sessions-${agentQuery.data.id}`}
        agent={agentQuery.data}
      />
      <AgentLifecycle
        agent={agentQuery.data}
        refresh={refresh}
        onDeleted={onBack}
      />
    </SettingsPage>
  );
}

function AgentIdentity({
  agent,
  refresh,
}: {
  agent: ManagedAgent;
  refresh: () => void;
}) {
  const [name, setName] = useState(agent.name);
  const rename = useRenameAgentMutation({
    onSuccess: () => {
      toast.success("Agent renamed");
      refresh();
    },
    onError: (error) => toast.error(error.message || "Unable to rename agent"),
  });

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Identity</SettingsSection.Title>
        <SettingsSection.Description>
          The principal ID and owner are durable. The display name can change.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-3 text-sm">
            <dt className="text-muted-foreground">Principal</dt>
            <dd className="font-mono">agent:{agent.id}</dd>
            <dt className="text-muted-foreground">Owner</dt>
            <dd>
              <AgentOwner agent={agent} />
            </dd>
            <dt className="text-muted-foreground">Lifecycle</dt>
            <dd>
              <LifecycleBadge lifecycle={agent.lifecycle} />
            </dd>
          </dl>
          <div className="max-w-xl space-y-2 pt-2">
            <Label htmlFor="managed-agent-name">Name</Label>
            <Input
              id="managed-agent-name"
              value={name}
              onChange={setName}
              maxLength={120}
              disabled={!agent.permissions.write || rename.isPending}
            />
          </div>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <Text muted small>
            {agent.permissions.write
              ? "Name changes are recorded in the organization audit log."
              : "You have read-only access to this agent."}
          </Text>
          <Button
            onClick={() =>
              rename.mutate({
                request: {
                  renameAgentForm: { id: agent.id, name: name.trim() },
                },
              })
            }
            disabled={
              !agent.permissions.write ||
              !name.trim() ||
              name.trim() === agent.name ||
              rename.isPending
            }
          >
            Save name
          </Button>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function AgentLifecycle({
  agent,
  refresh,
  onDeleted,
}: {
  agent: ManagedAgent;
  refresh: () => void;
  onDeleted: () => void;
}) {
  const [confirm, setConfirm] = useState<"revoke" | "delete" | null>(null);
  const common = {
    onError: (error: Error) =>
      toast.error(error.message || "Unable to update agent"),
  };
  const suspend = useAgentsSuspendMutation({
    ...common,
    onSuccess: () => {
      toast.success("Agent suspended");
      refresh();
    },
  });
  const resume = useAgentsResumeMutation({
    ...common,
    onSuccess: () => {
      toast.success("Agent resumed");
      refresh();
    },
  });
  const revoke = useAgentsRevokeMutation({
    ...common,
    onSuccess: () => {
      toast.success("Agent revoked");
      setConfirm(null);
      refresh();
    },
  });
  const remove = useAgentsDeleteMutation({
    ...common,
    onSuccess: () => {
      toast.success("Agent deleted");
      refresh();
      onDeleted();
    },
  });
  const pending =
    suspend.isPending ||
    resume.isPending ||
    revoke.isPending ||
    remove.isPending;
  const canWrite = agent.permissions.write;

  return (
    <>
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Availability</SettingsSection.Title>
          <SettingsSection.Description>
            Suspension is reversible. It blocks credentials without changing the
            owner or stored policy.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body className="flex items-center justify-between gap-6">
            <div>
              <Text className="font-medium">Agent is {agent.lifecycle}</Text>
              <Text muted small className="mt-1">
                {canWrite
                  ? "Lifecycle changes take effect for new authorization immediately."
                  : "You do not have permission to change this lifecycle."}
              </Text>
            </div>
            {agent.lifecycle === "suspended" ? (
              <Button
                onClick={() =>
                  resume.mutate({
                    request: { agentIDForm: { agentId: agent.id } },
                  })
                }
                disabled={!canWrite || pending}
              >
                Resume
              </Button>
            ) : agent.lifecycle === "active" ? (
              <Button
                variant="secondary"
                onClick={() =>
                  suspend.mutate({
                    request: { agentIDForm: { agentId: agent.id } },
                  })
                }
                disabled={!canWrite || pending}
              >
                Suspend
              </Button>
            ) : null}
          </SettingsSection.Body>
        </SettingsSection.Panel>
      </SettingsSection>

      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Revocation is terminal. Deletion releases the name but retains audit
            history.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body className="space-y-5">
            {agent.lifecycle !== "revoked" && (
              <DangerAction
                title="Revoke agent"
                description="Permanently prevent this agent from becoming active again."
                confirm={confirm === "revoke"}
                disabled={!canWrite || pending}
                pending={revoke.isPending}
                onStart={() => setConfirm("revoke")}
                onCancel={() => setConfirm(null)}
                onConfirm={() =>
                  revoke.mutate({
                    request: { agentIDForm: { agentId: agent.id } },
                  })
                }
              />
            )}
            <DangerAction
              title="Delete agent"
              description="Tombstone this agent and release its name for reuse."
              confirm={confirm === "delete"}
              disabled={!canWrite || pending}
              pending={remove.isPending}
              onStart={() => setConfirm("delete")}
              onCancel={() => setConfirm(null)}
              onConfirm={() =>
                remove.mutate({
                  request: { agentIDForm: { agentId: agent.id } },
                })
              }
            />
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
    </>
  );
}

function DangerAction({
  title,
  description,
  confirm,
  disabled,
  pending,
  onStart,
  onCancel,
  onConfirm,
}: {
  title: string;
  description: string;
  confirm: boolean;
  disabled: boolean;
  pending: boolean;
  onStart: () => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-6 border-b pb-5 last:border-b-0 last:pb-0">
      <div>
        <Text className="font-medium">{title}</Text>
        <Text muted small className="mt-1">
          {description}
        </Text>
      </div>
      <div className="flex shrink-0 gap-2">
        {confirm ? (
          <>
            <Button variant="tertiary" onClick={onCancel} disabled={pending}>
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={onConfirm}
              disabled={disabled}
            >
              Confirm {title.toLowerCase()}
            </Button>
          </>
        ) : (
          <Button
            variant="destructive-secondary"
            onClick={onStart}
            disabled={disabled}
          >
            {title}
          </Button>
        )}
      </div>
    </div>
  );
}

function LifecycleBadge({
  lifecycle,
}: {
  lifecycle: ManagedAgent["lifecycle"];
}) {
  const variant =
    lifecycle === "active"
      ? "success"
      : lifecycle === "suspended"
        ? "warning"
        : "destructive";
  return (
    <Badge variant={variant} size="sm">
      {lifecycle}
    </Badge>
  );
}
