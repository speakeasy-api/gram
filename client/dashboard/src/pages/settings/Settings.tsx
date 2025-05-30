import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { Key } from "@gram/client/models/components";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Column, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Copy, Trash2 } from "lucide-react";
import { useState } from "react";

export default function Settings() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();

  const { data: keysData } = useListAPIKeysSuspense();

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
      setNewKeyName("");
      setNewlyCreatedKey(data);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  const revokeKeyMutation = useRevokeAPIKeyMutation({
    onSuccess: async () => {
      setKeyToRevoke(null);
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
      await queryClient.refetchQueries({
        queryKey: ["@gram/client", "keys", "list"],
      });
    },
  });

  const handleCreateKey = () => {
    createKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          name: newKeyName,
        },
      },
    });
  };

  const handleRevokeKey = () => {
    if (!keyToRevoke) return;

    revokeKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        id: keyToRevoke.id,
      },
    });
  };

  const handleCopyToken = async () => {
    if (newlyCreatedKey?.token) {
      await navigator.clipboard.writeText(newlyCreatedKey.token);
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 2000);
    }
  };

  const handleCloseCreateDialog = () => {
    setIsCreateDialogOpen(false);
    setNewlyCreatedKey(null);
    setIsCopied(false);
  };

  const apiKeyColumns: Column<Key>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.name}</Type>,
    },
    {
      key: "token",
      header: "Token",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.token}</Type>,
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.scopes.join(",")}</Type>,
    },
    {
      key: "createdAt",
      header: "Created At",
      width: "1fr",
      render: (key: Key) => <HumanizeDateTime date={key.createdAt} />,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (key: Key) => (
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setKeyToRevoke(key)}
          className="hover:text-destructive"
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      ),
    },
  ];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex justify-between items-center mb-4">
          <Heading variant="h4">API Keys</Heading>
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            Create API Key
          </Button>
        </div>
        <Table
          columns={apiKeyColumns}
          data={keysData?.keys ?? []}
          rowKey={(row) => row.id}
        />

        <Dialog
          open={isCreateDialogOpen}
          onOpenChange={handleCloseCreateDialog}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>
                {newlyCreatedKey ? "API Key Created" : "Create New API Key"}
              </Dialog.Title>
            </Dialog.Header>
            {newlyCreatedKey ? (
              <div className="space-y-4 py-4">
                <div className="rounded-lg border-yellow-500/50 bg-yellow-50/50 text-yellow-600 p-4 text-sm">
                  You won't be able to see this token value again once you close
                  this dialog.
                </div>
                <div className="flex items-center space-x-2 bg-muted p-3 rounded-md">
                  <code className="flex-1 break-all">
                    {newlyCreatedKey.token}
                  </code>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleCopyToken}
                    className="shrink-0"
                  >
                    {isCopied ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                </div>
                <div className="flex justify-end">
                  <Button onClick={handleCloseCreateDialog}>Close</Button>
                </div>
              </div>
            ) : (
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Input
                    placeholder="Enter key name"
                    value={newKeyName}
                    onChange={setNewKeyName}
                  />
                </div>
                <div className="flex justify-end space-x-2">
                  <Button variant="secondary" onClick={handleCloseCreateDialog}>
                    Cancel
                  </Button>
                  <Button
                    onClick={handleCreateKey}
                    disabled={!newKeyName || createKeyMutation.isPending}
                  >
                    Create
                  </Button>
                </div>
              </div>
            )}
          </Dialog.Content>
        </Dialog>

        <Dialog
          open={!!keyToRevoke}
          onOpenChange={(open) => !open && setKeyToRevoke(null)}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Revoke API Key</Dialog.Title>
            </Dialog.Header>
            <div className="space-y-4 py-4">
              <Type variant="body">
                Are you sure you want to revoke the API key{" "}
                <span className="italic font-bold">{keyToRevoke?.name}</span>?
                This action cannot be undone.
              </Type>
              <div className="flex justify-end space-x-2">
                <Button
                  variant="secondary"
                  onClick={() => setKeyToRevoke(null)}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={handleRevokeKey}
                  disabled={revokeKeyMutation.isPending}
                >
                  Revoke Key
                </Button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
