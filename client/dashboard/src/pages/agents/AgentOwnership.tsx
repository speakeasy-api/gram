import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useTransferAgentMutation } from "@gram/client/react-query/transferAgent.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import { toast } from "sonner";
import { AgentOwner } from "./agent-admin";

/**
 * Platform MCP intentionally does not expose ownership transfer: the existing
 * service requires an ordinary human dashboard session and rejects OAuth,
 * assistant, and acting-surface contexts. This UI preserves that boundary
 * rather than expanding transfer authorization to automated callers.
 */
export function AgentOwnership({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element | null {
  const [open, setOpen] = useState(false);
  const projectSlug = useProjectSlugForRequests();
  if (!agent.permissions.transfer) return null;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Ownership</SettingsSection.Title>
        <SettingsSection.Description>
          Choose who owns this agent within your organization.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-center justify-between gap-6">
            <div>
              <Text className="font-medium">Transfer ownership</Text>
              <Text muted small className="mt-1">
                Assign this agent to another member of your organization.
              </Text>
            </div>
            <Button variant="secondary" onClick={() => setOpen(true)}>
              Transfer
            </Button>
          </div>
        </SettingsSection.Body>
      </SettingsSection.Panel>
      {open && (
        <TransferOwnershipDialog
          key={`${agent.id}:${agent.ownerUserId}:${projectSlug}`}
          agent={agent}
          onClose={() => setOpen(false)}
        />
      )}
    </SettingsSection>
  );
}

function TransferOwnershipDialog({
  agent,
  onClose,
}: {
  agent: ManagedAgent;
  onClose: () => void;
}) {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const projectSlug = useProjectSlugForRequests();
  const organization = useOrganization();
  const { user } = useSession();
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string>();
  const [confirm, setConfirm] = useState(false);
  const members = useQuery({
    queryKey: ["agent-transfer-members", organization.id, user.id, projectSlug],
    queryFn: ({ signal }) => sdk.access.listMembers({}, undefined, { signal }),
    // access.listMembers is deliberately unpaginated (Limit: 0 on the server).
    // It returns all active local members, including those without roles or directory profiles.
    staleTime: 0,
    retry: false,
  });
  const candidates = (members.data?.members ?? []).filter(
    (member) => member.id !== agent.ownerUserId,
  );
  const selected = candidates.find((member) => member.id === selectedId);
  const needle = search.trim().toLowerCase();
  const matching = candidates.filter((member) =>
    `${member.name} ${member.email}`.toLowerCase().includes(needle),
  );
  const transfer = useTransferAgentMutation({
    onSuccess: async (updatedAgent) => {
      const detailKey = ["managed-agents", organization.id, "detail", agent.id];
      await queryClient.cancelQueries({ queryKey: detailKey, exact: true });
      // The mutation returns freshly computed permissions, even if the caller
      // no longer has read access. Never retain the previous owner's controls
      // while the authoritative read is in flight (or fails).
      queryClient.setQueriesData<ManagedAgent>(
        { queryKey: detailKey, exact: true },
        updatedAgent,
      );
      toast.success("Agent ownership transferred");
      onClose();
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["managed-agents", organization.id],
        }),
        queryClient.invalidateQueries({
          queryKey: ["identities", "registered-agents", organization.id],
        }),
      ]);
    },
  });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !transfer.isPending) onClose();
      }}
    >
      <Dialog.Content closeable={!transfer.isPending}>
        <Dialog.Header>
          <Dialog.Title>
            {confirm ? "Confirm ownership transfer" : "Transfer ownership"}
          </Dialog.Title>
          <Dialog.Description>
            {confirm
              ? "Review the ownership change before continuing."
              : "Choose a member of your organization to own this agent."}
          </Dialog.Description>
        </Dialog.Header>
        {members.isError && (
          <div role="alert" className="space-y-3">
            <Text>
              Unable to load organization members. You may not have permission
              to view the member list.
            </Text>
            <Button
              variant="secondary"
              disabled={transfer.isPending || members.isFetching}
              onClick={() => void members.refetch()}
            >
              Retry
            </Button>
          </div>
        )}
        {confirm && selected ? (
          <>
            <dl className="space-y-4 border p-4">
              <div>
                <dt className="mb-2 text-sm text-muted-foreground">
                  Current owner
                </dt>
                <dd>
                  <AgentOwner agent={agent} />
                </dd>
              </div>
              <div>
                <dt className="mb-2 text-sm text-muted-foreground">
                  New owner
                </dt>
                <dd className="text-sm font-medium">
                  {selected.name || selected.email}
                </dd>
                <dd className="text-sm text-muted-foreground">
                  {selected.email}
                </dd>
              </div>
            </dl>
            <ul className="list-disc space-y-2 pl-5 text-sm text-muted-foreground">
              <li>
                The new owner will manage this agent. The previous owner will
                lose ownership-based access, but may retain access through their
                roles.
              </li>
              <li>
                Existing keys and stored policies will not change. Effective
                access remains limited by the new owner's permissions.
              </li>
              <li>
                Transferring ownership does not resume a suspended agent or
                reverse revocation.
              </li>
            </ul>
          </>
        ) : (
          <>
            <Input
              aria-label="Search members"
              placeholder="Search by name or email"
              value={search}
              onChange={setSearch}
            />
            {members.isPending ? (
              <Text role="status">Loading members…</Text>
            ) : members.isError ? null : matching.length === 0 ? (
              <Text muted>
                {candidates.length === 0
                  ? "No other eligible members in your organization."
                  : "No members match your search."}
              </Text>
            ) : (
              <fieldset
                className="max-h-72 space-y-1 overflow-y-auto"
                aria-label="New owner"
              >
                {matching.map((member) => (
                  <label
                    key={member.id}
                    className="flex cursor-pointer items-center gap-3 border p-3 has-[:checked]:border-primary hover:bg-muted/50"
                  >
                    <input
                      type="radio"
                      name="new-owner"
                      value={member.id}
                      checked={selectedId === member.id}
                      onChange={() => setSelectedId(member.id)}
                    />
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-medium">
                        {member.name || member.email}
                      </span>
                      <span className="block truncate text-sm text-muted-foreground">
                        {member.email}
                      </span>
                    </span>
                  </label>
                ))}
              </fieldset>
            )}
          </>
        )}
        {transfer.isError && (
          <Text role="alert" className="text-destructive">
            {transfer.error.message ||
              "Unable to transfer ownership. Please try again."}
          </Text>
        )}
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={transfer.isPending}
            onClick={() => {
              if (confirm) {
                setConfirm(false);
                transfer.reset();
              } else onClose();
            }}
          >
            {confirm ? "Back" : "Cancel"}
          </Button>
          {confirm ? (
            <Button
              disabled={!selected || members.isError || transfer.isPending}
              onClick={() => {
                if (selected)
                  transfer.mutate({
                    request: {
                      agentOwnerAssignmentForm: {
                        agentId: agent.id,
                        ownerUserId: selected.id,
                      },
                    },
                  });
              }}
            >
              {transfer.isPending ? "Transferring…" : "Transfer ownership"}
            </Button>
          ) : (
            <Button
              disabled={!selected || members.isError || members.isPending}
              onClick={() => setConfirm(true)}
            >
              Continue
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
