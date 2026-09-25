import { ResourceListPage } from "@/components/page-templates";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { Key } from "@gram/client/models/components/key.js";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Column, Table } from "@/components/ui/Table";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { RequireScope } from "@/components/require-scope";
import { CreateApiKeySheet } from "./CreateApiKeySheet";
import { projectBindingLabel } from "./api-key-project-binding";

export default function OrgApiKeys(): JSX.Element {
  const organization = useOrganization();
  // The key fetching request returns a forbidden error without the org:admin
  // scope; the outer RequireScope gates rendering (and the data hook) on that
  // scope.
  return (
    <RequireScope scope="org:admin" level="page">
      <OrgApiKeysInner key={organization.id} />
    </RequireScope>
  );
}

function OrgApiKeysInner() {
  const organization = useOrganization();
  const projectLabel = (id?: string) =>
    projectBindingLabel(organization.projects, id);
  const [isCreateSheetOpen, setIsCreateSheetOpen] = useState(false);
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const queryClient = useQueryClient();
  const [apiKeySearch, setApiKeySearch] = useState("");

  const { data: keysData } = useListAPIKeysSuspense();

  const filteredKeys = useMemo(() => {
    const keys = keysData?.keys ?? [];
    const search = apiKeySearch.trim().toLowerCase();
    if (!search) return keys;
    return keys.filter((key) => key.name.toLowerCase().includes(search));
  }, [keysData?.keys, apiKeySearch]);

  const revokeKeyMutation = useRevokeAPIKeyMutation({
    onSuccess: async () => {
      setKeyToRevoke(null);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  const handleRevokeKey = () => {
    if (!keyToRevoke) return;

    revokeKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        id: keyToRevoke.id,
      },
    });
  };

  const apiKeyColumns: Column<Key>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (key: Key) => <Text variant="body">{key.name}</Text>,
    },
    {
      key: "key",
      header: "Key",
      width: "1fr",
      render: (key: Key) => <Text variant="body">{key.keyPrefix}</Text>,
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "1fr",
      render: (key: Key) => <Text variant="body">{key.scopes.join(", ")}</Text>,
    },
    {
      key: "projectId",
      header: "Project binding",
      width: "1fr",
      render: (key: Key) => (
        <Text variant="body">{projectLabel(key.projectId)}</Text>
      ),
    },
    {
      key: "createdAt",
      header: "Created At",
      width: "1fr",
      render: (key: Key) => <HumanizeDateTime date={key.createdAt} />,
    },
    {
      key: "lastAccessedAt",
      header: "Last Accessed At",
      width: "1fr",
      render: (key: Key) =>
        key.lastAccessedAt ? (
          <HumanizeDateTime date={key.lastAccessedAt} />
        ) : (
          "-"
        ),
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (key: Key) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setKeyToRevoke(key)}
          className="hover:text-destructive"
        >
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Revoke API key</Button.Text>
        </Button>
      ),
    },
  ];

  const newApiKeyButton = (
    <RequireScope scope="org:admin" level="component">
      <Button onClick={() => setIsCreateSheetOpen(true)}>New API Key</Button>
    </RequireScope>
  );

  const createKeyButton = (
    <RequireScope scope="org:admin" level="component">
      <Button
        size="sm"
        variant="secondary"
        onClick={() => setIsCreateSheetOpen(true)}
      >
        <Button.LeftIcon>
          <Icon name="key-round" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Create Key</Button.Text>
      </Button>
    </RequireScope>
  );

  const hasNoKeys = (keysData?.keys ?? []).length === 0;

  return (
    <>
      <ResourceListPage
        title="API Keys"
        description="Create and manage API keys to authenticate programmatic access to platform services, including MCP service deployments, tool management, and other connections."
        primaryAction={newApiKeyButton}
        search={{
          value: apiKeySearch,
          onChange: setApiKeySearch,
          placeholder: "Search by key name",
        }}
        isEmpty={hasNoKeys}
        empty={{
          icon: "key-round",
          heading: "No API keys yet",
          action: createKeyButton,
        }}
      >
        {filteredKeys.length > 0 ? (
          <Table
            columns={apiKeyColumns}
            data={filteredKeys}
            rowKey={(row) => row.id}
            className="max-h-[500px] overflow-y-auto"
          />
        ) : (
          <div
            role="status"
            className="border-border bg-background flex min-h-32 flex-col items-center justify-center gap-4 border p-6"
          >
            <Text variant="body">No matching API keys</Text>
          </div>
        )}
      </ResourceListPage>

      <CreateApiKeySheet
        open={isCreateSheetOpen}
        onOpenChange={setIsCreateSheetOpen}
      />

      <Dialog
        open={!!keyToRevoke}
        onOpenChange={(open) => {
          void (!open && setKeyToRevoke(null));
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke API Key</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Text variant="body">
              Are you sure you want to revoke the API key{" "}
              <span className="font-bold italic">{keyToRevoke?.name}</span>?
              This action cannot be undone.
            </Text>
            <div className="flex justify-end space-x-2">
              <Button variant="secondary" onClick={() => setKeyToRevoke(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleRevokeKey}
                disabled={revokeKeyMutation.isPending}
              >
                Revoke Key
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
