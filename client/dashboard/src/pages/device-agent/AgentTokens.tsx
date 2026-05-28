import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
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

const AGENT_SCOPE = "agent";

export default function AgentTokens() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <AgentTokensInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function AgentTokensInner() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [keyToRevoke, setKeyToRevoke] = useState<Key | null>(null);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<Key | null>(null);
  const [isCopied, setIsCopied] = useState(false);
  const queryClient = useQueryClient();

  const { data: keysData } = useListAPIKeysSuspense();

  const agentKeys = useMemo(
    () =>
      (keysData?.keys ?? []).filter((key) => key.scopes.includes(AGENT_SCOPE)),
    [keysData?.keys],
  );

  const invalidate = async () => {
    await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
    await queryClient.refetchQueries({
      queryKey: ["@gram/client", "keys", "list"],
    });
  };

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
      setNewlyCreatedKey(data);
      await invalidate();
    },
  });

  const revokeKeyMutation = useRevokeAPIKeyMutation({
    onSuccess: async () => {
      setKeyToRevoke(null);
      await invalidate();
    },
  });

  const handleCreateKey: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const formEl = e.currentTarget;
    const newKeyName = new FormData(formEl).get("name");
    assert(typeof newKeyName === "string", "Token name must be a string");

    createKeyMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          createKeyForm: { name: newKeyName, scopes: [AGENT_SCOPE] },
        },
      },
      { onSuccess: () => formEl.reset() },
    );
  };

  const handleRevokeKey = () => {
    if (!keyToRevoke) return;
    revokeKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: keyToRevoke.id },
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

  const columns: Column<Key>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (key) => <Type variant="body">{key.name}</Type>,
    },
    {
      key: "key",
      header: "Token",
      width: "1fr",
      render: (key) => <Type variant="body">{key.keyPrefix}</Type>,
    },
    {
      key: "createdAt",
      header: "Created",
      width: "1fr",
      render: (key) => <HumanizeDateTime date={key.createdAt} />,
    },
    {
      key: "lastAccessedAt",
      header: "Last seen",
      width: "1fr",
      render: (key) =>
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
      render: (key) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setKeyToRevoke(key)}
          className="hover:text-destructive"
        >
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Revoke token</Button.Text>
        </Button>
      ),
    },
  ];

  return (
    <>
      <Heading variant="h4" className="mb-2">
        Device Agent Tokens
      </Heading>
      <Type muted small className="mb-6">
        Mint org-scoped tokens for the Speakeasy device agent. The agent
        presents one of these tokens when fetching the plugins assigned to its
        enrolled user. Each token is shown once at creation time and can be
        revoked at any time.
      </Type>
      <Stack
        direction="horizontal"
        justify="end"
        align="center"
        className="mb-4"
      >
        <RequireScope scope="org:admin" level="component">
          <Button onClick={() => setIsCreateDialogOpen(true)}>New Token</Button>
        </RequireScope>
      </Stack>
      <Table
        columns={columns}
        data={agentKeys}
        rowKey={(row) => row.id}
        className="max-h-[500px] overflow-y-auto"
        noResultsMessage={
          <Stack
            gap={2}
            className="bg-background h-full gap-4 p-4 py-6"
            align="center"
            justify="center"
          >
            <Type variant="body">No agent tokens yet</Type>
            <RequireScope scope="org:admin" level="component">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setIsCreateDialogOpen(true)}
              >
                <Button.LeftIcon>
                  <Icon name="key-round" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Create Token</Button.Text>
              </Button>
            </RequireScope>
          </Stack>
        }
      />

      <Dialog open={isCreateDialogOpen} onOpenChange={handleCloseCreateDialog}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              {newlyCreatedKey
                ? "Agent Token Created"
                : "Create New Agent Token"}
            </Dialog.Title>
          </Dialog.Header>
          {newlyCreatedKey ? (
            <div className="space-y-4 py-4">
              <div className="text-foreground rounded-lg border border-yellow-500/50 bg-yellow-600/50 p-4 text-sm">
                You will not be able to see this token value again once you
                close this dialog. Copy it now and write it into the agent's{" "}
                <code>managed.json</code> as <code>org_token</code> (or hand it
                to the dev for <code>speakeasy enroll</code>).
              </div>
              <div className="bg-muted flex items-center space-x-2 rounded-md p-3">
                <code className="flex-1 break-all">{newlyCreatedKey.key}</code>
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
                label="Token name"
                name="name"
                required
                autoFocus
                autoCapitalize="off"
                autoComplete="off"
                autoCorrect="off"
              />
              <Type muted small>
                Scope is fixed to <code>agent</code> — the token can only fetch
                plugin assignments for the enrolled user, nothing else.
              </Type>
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
            <Dialog.Title>Revoke Agent Token</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to revoke{" "}
              <span className="font-bold italic">{keyToRevoke?.name}</span>? Any
              agent presenting this token will start receiving authentication
              errors immediately.
            </Type>
            <div className="flex justify-end space-x-2">
              <Button variant="secondary" onClick={() => setKeyToRevoke(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleRevokeKey}
                disabled={revokeKeyMutation.isPending}
              >
                Revoke Token
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
