import {
  DangerSettingsSection,
  FormPage,
  ResourceListPage,
  SettingsPage,
} from "@/components/page-templates";
import { Avatar, AvatarImage, AvatarFallback } from "@/components/ui/Avatar";
import { Table, type Column } from "@/components/ui/Table";
import { useSdkClient } from "@/contexts/Sdk";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  useIsPlatformAdmin,
  useOrganization,
  useProject,
  useSession,
} from "@/contexts/Auth";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

import { useAgentsDeleteMutation } from "@gram/client/react-query/agentsDelete.js";
import { useAgentsResumeMutation } from "@gram/client/react-query/agentsResume.js";
import { useAgentsRevokeMutation } from "@gram/client/react-query/agentsRevoke.js";
import { useAgentsSuspendMutation } from "@gram/client/react-query/agentsSuspend.js";
import { useRenameAgentMutation } from "@gram/client/react-query/renameAgent.js";
import { hashKey, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { AgentAPIKeys } from "./AgentAPIKeys";
import { agentIssuanceBlocked } from "./agent-issuance";
import { ProvisionWizard } from "./provision/ProvisionWizard";
import { AgentReview } from "./provision/AgentReview";
import { AgentPolicySection } from "./AgentPolicySection";
import { ManagedAgentSessions } from "./ManagedAgentSessions";

export default function AgentsPage(): JSX.Element {
  const flag = useFeatureFlag(FEATURE_FLAGS.agentManagement);
  if (flag.status !== "enabled") {
    return (
      <FormPage
        title={
          flag.status === "loading"
            ? "Loading agent management"
            : "Agent management unavailable"
        }
        description={
          flag.status === "loading"
            ? "Checking feature availability."
            : flag.status === "disabled"
              ? "Agent management is not enabled for this organization."
              : "Unable to determine agent management availability. Try again later."
        }
      >
        {null}
      </FormPage>
    );
  }

  // Do not mount inventory, detail, or policy discovery until rollout is enabled.
  return <AgentManagementPage />;
}

function AgentManagementPage(): JSX.Element {
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

  if (!agentID && searchParams.get("create") === "true" && !isDemo) {
    return (
      <NewAgentPage
        // Switching any of these would otherwise carry a name, a server
        // selection and a credential chosen in a different context.
        key={`${organization.id}:${session.user.id}`}
        onDone={(id) => setSearchParams(id ? { id } : {})}
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

/**
 * The page that creates an agent identity and provisions it in one pass. The
 * identity only matters once some runtime elsewhere is holding its key, so the
 * flow does not stop at "created".
 */
function NewAgentPage({ onDone }: { onDone: (id?: string) => void }) {
  const [busy, setBusy] = useState(false);
  return (
    <FormPage
      title="New agent identity"
      description="Name it, choose the servers it can reach, then connect it to its runtime."
      width="wide"
      primaryAction={
        <Button variant="secondary" disabled={busy} onClick={() => onDone()}>
          <Button.LeftIcon>
            <ArrowLeft className="size-4" />
          </Button.LeftIcon>
          <Button.Text>All agents</Button.Text>
        </Button>
      }
    >
      <ProvisionWizard onDone={onDone} onBusy={setBusy} />
    </FormPage>
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
  const agents = useReadableAgents(true);
  const project = useProject();
  const [search, setSearch] = useState("");
  const visibleAgents = (agents.data ?? []).filter(
    (agent) =>
      // An agent bound to another project is that project's to manage, so
      // showing it here reads as a listing bug. Organization-wide agents have
      // no home project and belong in every project's list.
      !agent.projectId || agent.projectId === project.id,
  );
  const rows = visibleAgents.filter((agent) =>
    agent.name.toLowerCase().includes(search.trim().toLowerCase()),
  );
  const columns: Column<ManagedAgent>[] = [
    {
      key: "name",
      header: "Identity",
      // Name over principal: the name is what a person recognizes, the
      // principal is what they will have to match against an audit entry.
      render: (agent) => (
        <button
          type="button"
          // The row reads as initials, name and principal; the control it sits
          // in is named for the agent alone.
          aria-label={agent.name}
          className="flex min-w-0 items-center gap-3 text-left"
          onClick={() => onSelect(agent.id)}
        >
          <Avatar>
            <AvatarFallback>
              {agent.name.slice(0, 1).toUpperCase()}
            </AvatarFallback>
          </Avatar>
          <span className="min-w-0">
            <span className="block truncate">{agent.name}</span>
            <span className="text-muted-foreground block truncate font-mono text-xs">
              agent:{agent.id}
            </span>
          </span>
        </button>
      ),
    },
    {
      key: "kind",
      header: "Kind",
      width: "120px",
      render: () => (
        <Badge variant="neutral" size="sm">
          Agent
        </Badge>
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
      width: "140px",
      render: (agent) => <LifecycleBadge lifecycle={agent.lifecycle} />,
    },
  ];
  return (
    <ResourceListPage
      title="Agents"
      description={`${rows.length} of ${(agents.data ?? []).length} — every agent identity this organization knows about, provisioned here or not.`}
      stage="preview"
      primaryAction={<Button onClick={onCreate}>New agent identity</Button>}
      search={{
        value: search,
        onChange: setSearch,
        placeholder: "Search agents",
      }}
      isLoading={agents.isLoading}
      isEmpty={!agents.isError && visibleAgents.length === 0 && !search.trim()}
      empty={{
        icon: "bot",
        heading: "No agents yet",
        description:
          "An agent identity is a key its runtime holds, with its own permissions and its own line in the audit log.",
      }}
      onRefresh={() => void agents.refetch()}
      isRefreshing={agents.isFetching}
    >
      {agents.isError ? (
        <Text role="alert">
          {agents.error instanceof GramError && agents.error.statusCode === 404
            ? "Agent management is not enabled for this organization."
            : "Unable to load agents. Try again."}
        </Text>
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

/**
 * Names the project an agent belongs to, or says it belongs to none. An agent
 * bound to another project can still be reached by id, so this reports the
 * binding rather than assuming it is the project being viewed.
 */
function AgentScopeLabel({ agent }: { agent: ManagedAgent }) {
  const organization = useOrganization();
  if (!agent.projectId) {
    return <Text>{organization.name} (all projects)</Text>;
  }
  // Named from the organization's own project list rather than the active
  // project, so an agent reached by id from elsewhere still says where it
  // belongs. A project the caller cannot see falls back to its id.
  const bound = organization.projects?.find(
    (candidate) => candidate.id === agent.projectId,
  );
  return <Text>{bound?.name ?? agent.projectId}</Text>;
}

function AgentSettings({
  agentID,
  onBack,
}: {
  agentID: string;
  onBack: () => void;
}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [credentialBusy, setCredentialBusy] = useState(false);
  const credentialsFlag = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
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

  // The same five steps as a new agent, because the work is the same work:
  // which servers, which credential, hand it to the runtime, watch it call.
  // Only the identity is already settled, so its steps are all reachable.
  if (searchParams.get("credential") === "new")
    return (
      <FormPage
        title="Provision agent"
        description={`Issue ${agentQuery.data.name} a key and connect it to its runtime.`}
        width="wide"
        primaryAction={
          <Button
            variant="secondary"
            onClick={() => setSearchParams({ id: agentID })}
            disabled={credentialBusy}
          >
            Back to agent
          </Button>
        }
      >
        <ProvisionWizard
          agent={agentQuery.data}
          onBusy={setCredentialBusy}
          onDone={() => setSearchParams({ id: agentID })}
        />
      </FormPage>
    );

  return (
    <SettingsPage
      title={agentQuery.data.name}
      description={<AgentSummary agent={agentQuery.data} />}
      primaryAction={
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={onBack}>
            <Button.LeftIcon>
              <ArrowLeft className="size-4" />
            </Button.LeftIcon>
            <Button.Text>All agents</Button.Text>
          </Button>
          <RenameAgentButton agent={agentQuery.data} refresh={refresh} />
        </div>
      }
    >
      {/* The agent read through the same five steps that made it: every step
          complete, every step a destination, and the panels below each one
          are the surfaces that change it. Lifecycle is not a step — ending an
          agent is not part of provisioning it. */}
      <AgentReview
        agent={agentQuery.data}
        scopeLabel={organization.name}
        // The same rule the keys panel applies: ownership alone would offer
        // the button for a suspended agent, or with the credential rollout
        // off, and the click would then fail.
        canIssue={
          agentIssuanceBlocked(
            agentQuery.data,
            credentialsFlag.status === "enabled",
          ) === null
        }
        onIssueKey={() => setSearchParams({ id: agentID, credential: "new" })}
        panels={{
          identity: <AgentIdentityPanel agent={agentQuery.data} />,
          servers: (
            <AgentPolicySection
              key={`policy-${agentQuery.data.id}`}
              agent={agentQuery.data}
              variant="bare"
            />
          ),
          credential: (
            <AgentAPIKeys
              agent={agentQuery.data}
              variant="bare"
              onCreate={() =>
                setSearchParams({ id: agentID, credential: "new" })
              }
            />
          ),
          sessions: (
            <ManagedAgentSessions
              key={`sessions-${agentQuery.data.id}`}
              agent={agentQuery.data}
              variant="bare"
            />
          ),
        }}
      />
      <AgentLifecycle
        agent={agentQuery.data}
        refresh={refresh}
        onDeleted={onBack}
      />
    </SettingsPage>
  );
}

/**
 * Step one, for an agent that exists: the name it is known by and the facts
 * that cannot change — its principal and its owner. Renaming is the only edit
 * here, because everything else about an identity is fixed at creation.
 */
function AgentIdentityPanel({ agent }: { agent: ManagedAgent }) {
  const { user } = useSession();
  return (
    <div className="border-border space-y-4 border p-4">
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 text-sm">
        <dt className="text-muted-foreground">Name</dt>
        <dd>{agent.name}</dd>
        <dt className="text-muted-foreground">Principal</dt>
        <dd className="font-mono text-xs">agent:{agent.id}</dd>
        <dt className="text-muted-foreground">Owner</dt>
        <dd>
          {agent.ownerProfile?.displayName ??
            (agent.ownerUserId === user.id
              ? user.displayName || user.email
              : "Unavailable")}
        </dd>
        <dt className="text-muted-foreground">Scope</dt>
        <dd>
          <AgentScopeLabel agent={agent} />
        </dd>
        <dt className="text-muted-foreground">Lifecycle</dt>
        <dd>
          <LifecycleBadge lifecycle={agent.lifecycle} />
        </dd>
      </dl>
      <Text muted small>
        The principal and the owner are durable. The name can change, and the
        change is recorded in the organization audit log.
      </Text>
    </div>
  );
}

/**
 * The durable facts about the identity — who owns it, what principal it is,
 * whether it is live. They belong beside the name, not in a section of their
 * own: nobody comes to this page to read them.
 */
function AgentSummary({ agent }: { agent: ManagedAgent }) {
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <LifecycleBadge lifecycle={agent.lifecycle} />
      <AgentOwner agent={agent} />
      <code className="text-muted-foreground text-xs">agent:{agent.id}</code>
    </span>
  );
}

/** Renaming is rare and never the reason for the visit, so it is a dialog
 * rather than a form the page carries at all times. */
function RenameAgentButton({
  agent,
  refresh,
}: {
  agent: ManagedAgent;
  refresh: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(agent.name);
  const rename = useRenameAgentMutation({
    onSuccess: () => {
      toast.success("Agent renamed");
      setOpen(false);
      refresh();
    },
    onError: (error) => toast.error(error.message || "Unable to rename agent"),
  });

  if (!agent.permissions.write) return null;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (rename.isPending) return;
        setOpen(next);
        if (next) setName(agent.name);
      }}
    >
      <Dialog.Trigger asChild>
        <Button variant="secondary">Rename</Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Rename agent</Dialog.Title>
          <Dialog.Description>
            The principal ID and its issued keys are unaffected. The change is
            recorded in the organization audit log.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-2">
          <Label htmlFor="managed-agent-name">Name</Label>
          <Input
            id="managed-agent-name"
            value={name}
            onChange={setName}
            maxLength={120}
            disabled={rename.isPending}
            autoFocus
          />
        </div>
        <Dialog.Footer>
          <Button
            variant="secondary"
            onClick={() => setOpen(false)}
            disabled={rename.isPending}
          >
            Cancel
          </Button>
          <Button
            onClick={() =>
              rename.mutate({
                request: {
                  renameAgentForm: { id: agent.id, name: name.trim() },
                },
              })
            }
            disabled={
              !name.trim() || name.trim() === agent.name || rename.isPending
            }
          >
            Save
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
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
      {/* Suspension used to carry a section of its own, which put a reversible
          switch as far from revocation as the page allowed. All four lifecycle
          moves now sit together, ordered by how final they are. */}
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Lifecycle</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Suspension is reversible. Revocation is terminal, and deletion
            releases the name while keeping the audit history.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body className="space-y-5">
            {agent.lifecycle === "suspended" ? (
              <LifecycleAction
                title="Resume agent"
                description="Let this agent authenticate again with its existing keys."
                action={
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
                }
              />
            ) : agent.lifecycle === "active" ? (
              <LifecycleAction
                title="Suspend agent"
                description="Block its credentials without changing the owner or stored policy. Reversible."
                action={
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
                }
              />
            ) : null}
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

/** A lifecycle row: what it does, and the control that does it. */
function LifecycleAction({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action: JSX.Element;
}) {
  return (
    <div className="flex items-center justify-between gap-6 border-b pb-5 last:border-b-0 last:pb-0">
      <div>
        <Text className="font-medium">{title}</Text>
        <Text muted small className="mt-1">
          {description}
        </Text>
      </div>
      <div className="flex shrink-0 gap-2">{action}</div>
    </div>
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
