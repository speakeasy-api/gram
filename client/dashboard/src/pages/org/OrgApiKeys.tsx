import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { assert } from "@/lib/utils";
import { Key } from "@gram/client/models/components";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeysSuspense,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Copy } from "lucide-react";
import { useMemo, useState } from "react";

export default function OrgApiKeys() {
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
      render: (key: Key) => <Type variant="body">{key.name}</Type>,
    },
    {
      key: "key",
      header: "Key",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.keyPrefix}</Type>,
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "1fr",
      render: (key: Key) => <Type variant="body">{key.scopes.join(", ")}</Type>,
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

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>API Keys</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Heading variant="h4" className="mb-2">
          API Keys
        </Heading>
        <Type muted small className="mb-6">
          Create and manage API keys to authenticate programmatic access to Gram
          services, including deployments, toolset management, and MCP server
          connections.
        </Type>
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-4"
        >
          <SearchBar
            value={apiKeySearch}
            onChange={setApiKeySearch}
            placeholder="Search by key name"
            className="w-64"
          />
          <Button onClick={() => setIsCreateDialogOpen(true)}>
            New API Key
          </Button>
        </Stack>
        <Table
          columns={apiKeyColumns}
          data={filteredKeys}
          rowKey={(row) => row.id}
          className="max-h-[500px] overflow-y-auto"
          noResultsMessage={
            <Stack
              gap={2}
              className="bg-background h-full p-4"
              align="center"
              justify="center"
            >
              <Type variant="body">
                {apiKeySearch ? "No matching API keys" : "No API keys yet"}
              </Type>
              {!apiKeySearch && (
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
              )}
            </Stack>
          }
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
                <div className="text-foreground rounded-lg border border-yellow-500/50 bg-yellow-600/50 p-4 text-sm">
                  You will not be able to see this token value again once you
                  close this dialog. Copy it now and store it securely.
                </div>
                <div className="bg-muted flex items-center space-x-2 rounded-md p-3">
                  <code className="flex-1 break-all">
                    {newlyCreatedKey.key}
                  </code>
                  <Button
                    variant="tertiary"
                    size="sm"
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
                            deployments, query/modify toolsets, read data and
                            access MCP servers.
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
                            Hooks: can send hook events and OTEL logs from agent
                            integrations.
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
          onOpenChange={(open) => !open && setKeyToRevoke(null)}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Revoke API Key</Dialog.Title>
            </Dialog.Header>
            <div className="space-y-4 py-4">
              <Type variant="body">
                Are you sure you want to revoke the API key{" "}
                <span className="font-bold italic">{keyToRevoke?.name}</span>?
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
      </Page.Body>
    </Page>
  );
}
