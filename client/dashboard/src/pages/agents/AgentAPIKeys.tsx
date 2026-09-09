import { useState } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { SettingsSection } from "@/components/page-templates";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentAPIKey } from "@gram/client/models/components/agentapikey.js";

export function AgentAPIKeys({ agent }: { agent: ManagedAgent }): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const queryKey = ["managed-agent-api-keys", organization.id, agent.id];
  const canManage = agent.permissions.authorize;
  const [selected, setSelected] = useState<AgentAPIKey | null>(null);
  // Never substitute the owner's keys or the generic organization key endpoint.
  const keys = useInfiniteQuery({
    queryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      sdk.agents.listAPIKeys(
        { agentId: agent.id, cursor: pageParam },
        undefined,
        { signal },
      ),
    getNextPageParam: (page) => page.nextCursor || undefined,
    enabled: canManage,
    throwOnError: false,
    retry: false,
  });
  const revoke = useMutation({
    mutationFn: async (key: AgentAPIKey) => {
      if (!canManage) throw new Error("Agent credential permission required");
      await sdk.agents.revokeAPIKey({
        requestBody: { agentId: agent.id, keyId: key.id },
      });
    },
    onSuccess: async () => {
      setSelected(null);
      await queryClient.invalidateQueries({ queryKey });
    },
    throwOnError: false,
  });
  const rows = keys.data?.pages.flatMap((page) => page.items) ?? [];
  const columns: Column<AgentAPIKey>[] = [
    { key: "name", header: "Name" },
    {
      key: "createdAt",
      header: "Created",
      render: (key) => <HumanizeDateTime date={key.createdAt} />,
    },
    {
      key: "lastAccessedAt",
      header: "Last used",
      render: (key) =>
        key.lastAccessedAt ? (
          <HumanizeDateTime date={key.lastAccessedAt} />
        ) : (
          "Unknown"
        ),
    },
    {
      key: "expiresAt",
      header: "Expires",
      render: (key) =>
        key.expiresAt ? (
          <HumanizeDateTime date={key.expiresAt} />
        ) : (
          "No expiration"
        ),
    },
    {
      key: "actions",
      header: "",
      render: (key) => (
        <Button
          size="sm"
          variant="destructive-secondary"
          onClick={() => {
            revoke.reset();
            setSelected(key);
          }}
        >
          Revoke API key
        </Button>
      ),
    },
  ];

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>API keys</SettingsSection.Title>
        <SettingsSection.Description>
          View metadata and revoke existing keys bound to this agent. Agent API
          key creation is not available.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!canManage ? (
            <Text>
              You do not have permission to view this agent's API keys.
            </Text>
          ) : keys.isLoading ? (
            <div role="status" aria-label="Loading agent API keys">
              <SkeletonTable />
            </div>
          ) : keys.isError && !keys.isFetchNextPageError ? (
            <div role="alert" className="space-y-3">
              <Text>Unable to load agent API keys.</Text>
              <Button variant="secondary" onClick={() => void keys.refetch()}>
                Try again
              </Button>
            </div>
          ) : rows.length === 0 ? (
            <InlineEmptyState
              icon="key-round"
              heading="No agent-bound API keys found"
              description="No API keys bound to this agent were returned."
            />
          ) : (
            <Table columns={columns} data={rows} rowKey={(key) => key.id} />
          )}
          {canManage && keys.isFetchNextPageError && (
            <Text role="alert">Unable to load more API keys. Try again.</Text>
          )}
          {canManage && keys.hasNextPage && (
            <Button
              variant="secondary"
              disabled={keys.isFetchingNextPage}
              onClick={() => void keys.fetchNextPage()}
            >
              {keys.isFetchingNextPage ? "Loading more…" : "Load more API keys"}
            </Button>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
      <Dialog
        open={canManage && selected !== null}
        onOpenChange={(open) => {
          if (!open && !revoke.isPending) setSelected(null);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke API key?</Dialog.Title>
            <Dialog.Description>
              This permanently revokes{" "}
              {selected?.name || "the selected API key"}. Any client using it
              will lose access.
            </Dialog.Description>
          </Dialog.Header>
          {revoke.isError && (
            <Text role="alert">Unable to revoke the API key. Try again.</Text>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={revoke.isPending}
              onClick={() => setSelected(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={!canManage || revoke.isPending}
              onClick={() => {
                if (selected && canManage && !revoke.isPending)
                  revoke.mutate(selected);
              }}
            >
              {revoke.isPending ? "Revoking…" : "Confirm revoke"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}
