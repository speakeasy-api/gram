import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { ResourceListPage } from "@/components/page-templates";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { assert } from "@/lib/utils";
import { Key } from "@gram/client/models/components/key.js";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Column, Table } from "@/components/ui/Table";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Copy } from "lucide-react";
import { useMemo, useState } from "react";
import { RequireScope } from "@/components/require-scope";

export default function OrgApiKeys(): JSX.Element {
  // The key fetching request returns a forbidden error without the org:admin
  // scope; the outer RequireScope gates rendering (and the data hook) on that
  // scope.
  return (
    <RequireScope scope="org:admin" level="page">
      <OrgApiKeysInner />
    </RequireScope>
  );
}

function OrgApiKeysInner() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();
  const [apiKeySearch, setApiKeySearch] = useState("");

  const { data: keysData } = useListAPIKeysSuspense();

  const filteredKeys = useMemo(() => {
    const keys = keysData?.keys ?? [];
    const search = apiKeySearch.trim().toLowerCase();
    if (!search) return keys;
    return keys.filter((key) => key.name.toLowerCase().includes(search));
  }, [keysData?.keys, apiKeySearch]);

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
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

  const handleCreateKey: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const formEl = e.currentTarget;
    const formData = new FormData(formEl);
    const newKeyName = formData.get("name");
    assert(typeof newKeyName === "string", "Key name must be a string");
    const scope = formData.get("scope");
    assert(typeof scope === "string", "Scope must be a string");

    createKeyMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          createKeyForm: {
            name: newKeyName,
            scopes: [scope],
          },
        },
      },
      {
        onSuccess: () => {
          formEl.reset();
        },
      },
    );
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
    if (newlyCreatedKey?.key) {
      await navigator.clipboard.writeText(newlyCreatedKey.key);
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
      <Button onClick={() => setIsCreateDialogOpen(true)}>New API Key</Button>
    </RequireScope>
  );

  const createKeyButton = (
    <RequireScope scope="org:admin" level="component">
      <Button
        size="sm"
        variant="secondary"
        onClick={() => setIsCreateDialogOpen(true)}
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

      <Dialog open={isCreateDialogOpen} onOpenChange={handleCloseCreateDialog}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              {newlyCreatedKey ? "API Key Created" : "Create New API Key"}
            </Dialog.Title>
          </Dialog.Header>
          {newlyCreatedKey ? (
            <div className="space-y-4 py-4">
              <div className="text-foreground border border-yellow-500/50 bg-yellow-600/50 p-4 text-sm">
                You will not be able to see this token value again once you
                close this dialog. Copy it now and store it securely.
              </div>
              <div className="bg-muted flex items-center space-x-2 p-3">
                <code className="flex-1 break-all">{newlyCreatedKey.key}</code>
                <Button
                  aria-label={isCopied ? "API key copied" : "Copy API key"}
                  variant="tertiary"
                  size="sm"
                  onClick={() => void handleCopyToken()}
                  className="shrink-0"
                >
                  <Button.Icon>
                    {isCopied ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button.Icon>
                </Button>
              </div>
              <div className="flex justify-end">
                <Button onClick={handleCloseCreateDialog}>Close</Button>
              </div>
            </div>
          ) : (
            <form className="space-y-4 py-4" onSubmit={handleCreateKey}>
              <InputField
                label="Key name"
                name="name"
                required
                autoFocus
                autoCapitalize="off"
                autoComplete="off"
                autoCorrect="off"
              />

              <AnyField
                label="Scope"
                optionality="hidden"
                render={() => {
                  return (
                    <RadioGroup name="scope" defaultValue="consumer">
                      <div className="flex items-center gap-3">
                        <RadioGroupItem value="consumer" id="r1" />
                        <Label className="leading-normal" htmlFor="r1">
                          Consumer: can query/modify toolsets, read data and
                          access MCP servers.
                        </Label>
                      </div>
                      <div className="flex items-center gap-3">
                        <RadioGroupItem value="producer" id="r2" />
                        <Label className="leading-normal" htmlFor="r2">
                          Producer: can upload OpenAPI documents, trigger
                          deployments, query/modify toolsets, read data
                          (including exporting chat transcripts), and access MCP
                          servers.
                        </Label>
                      </div>
                      <div className="flex items-center gap-3">
                        <RadioGroupItem value="chat" id="r3" />
                        <Label className="leading-normal" htmlFor="r3">
                          Chat: can use the chat API to interact with models.
                        </Label>
                      </div>
                      <div className="flex items-center gap-3">
                        <RadioGroupItem value="hooks" id="r4" />
                        <Label className="leading-normal" htmlFor="r4">
                          Hooks: can ingest authenticated AI traffic, including
                          hook events and OpenTelemetry data.
                        </Label>
                      </div>
                      <div className="flex items-center gap-3">
                        <RadioGroupItem value="agent" id="r5" />
                        <Label className="leading-normal" htmlFor="r5">
                          Agent: presents to the Speakeasy device agent endpoint
                          to fetch the user's assigned plugins. Store it in
                          managed.json as org_token, or hand it to a dev for
                          speakeasy enroll.
                        </Label>
                      </div>
                    </RadioGroup>
                  );
                }}
              />
              <div className="flex justify-end space-x-2">
                <Button
                  type="button"
                  variant="secondary"
                  onClick={handleCloseCreateDialog}
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={createKeyMutation.isPending}>
                  Create
                </Button>
              </div>
            </form>
          )}
        </Dialog.Content>
      </Dialog>

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
