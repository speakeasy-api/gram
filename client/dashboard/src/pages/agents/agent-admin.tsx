/**
 * The pieces that administer a registered agent: who owns it, what it is
 * called, and whether it may still act.
 *
 * These were a page of their own at `agent-management`, which showed the same
 * agent the Identities pages show. The page is gone; the parts that did real
 * work live here and are rendered on the agent's own page.
 */
import { DangerSettingsSection } from "@/components/page-templates";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { useOrganization, useSession } from "@/contexts/Auth";
import { Text } from "@/components/ui/Text";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

import { useAgentsDeleteMutation } from "@gram/client/react-query/agentsDelete.js";
import { useAgentsResumeMutation } from "@gram/client/react-query/agentsResume.js";
import { useAgentsRevokeMutation } from "@gram/client/react-query/agentsRevoke.js";
import { useAgentsSuspendMutation } from "@gram/client/react-query/agentsSuspend.js";
import { useRenameAgentMutation } from "@gram/client/react-query/renameAgent.js";
import { useState } from "react";
import { toast } from "sonner";

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

export function AgentIdentityPanel({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
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

export function RenameAgentButton({
  agent,
  refresh,
}: {
  agent: ManagedAgent;
  refresh: () => void;
  // Null when the reader may not rename: the control is the whole output.
}): JSX.Element | null {
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

export function AgentLifecycle({
  agent,
  refresh,
  onDeleted,
}: {
  agent: ManagedAgent;
  refresh: () => void;
  onDeleted: () => void;
}): JSX.Element {
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
