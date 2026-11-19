import { ResourceListPage } from "@/components/page-templates";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { Key } from "@gram/client/models/components/key.js";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Spinner } from "@/components/ui/Spinner";
import type { Column } from "@/components/ui/Table";
import { Table } from "@/components/ui/Table";
import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { RequireScope } from "@/components/require-scope";
import { CreateApiKeySheet } from "./CreateApiKeySheet";
import { projectBindingLabel } from "./api-key-project-binding";
import { KeyPolicySheet } from "./KeyPolicySheet";
import { keyPolicySummary } from "./key-policy";

// Large organizations hold thousands of keys, most of them minted by plugins,
// so the page reads one page at a time and searches on the server.
const API_KEYS_PAGE_SIZE = 50;
const SEARCH_DEBOUNCE_MS = 300;

// Starts with the SDK's keys.list prefix, so every invalidateAllListAPIKeys
// call (create, revoke, policy edits, plugin rotations) also refreshes this
// list. "infinite" keeps it apart from the SDK's single-page cache entries,
// which hold a different data shape. The organization is part of the key so
// switching organizations never shows the previous one's cached pages.
function orgApiKeysQueryKey(organizationId: string, search: string) {
  return [
    "@gram/client",
    "keys",
    "list",
    "infinite",
    { organizationId, limit: API_KEYS_PAGE_SIZE, search },
  ] as const;
}

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
  const [keyForPolicy, setKeyForPolicy] = useState<Key | null>(null);
  const queryClient = useQueryClient();
  const sdk = useSdkClient();
  const [apiKeySearch, setApiKeySearch] = useState("");
  const search = apiKeySearch.trim();

  const keysQuery = useInfiniteQuery({
    queryKey: orgApiKeysQueryKey(organization.id, search),
    queryFn: ({ pageParam, signal }) =>
      sdk.keys.list(
        {
          limit: API_KEYS_PAGE_SIZE,
          cursor: pageParam,
          search: search === "" ? undefined : search,
        },
        undefined,
        { fetchOptions: { signal } },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    // Keep the current rows on screen while a new search loads, so the
    // toolbar (and the search box being typed in) never unmounts.
    placeholderData: keepPreviousData,
  });

  const keys = useMemo(
    () => keysQuery.data?.pages.flatMap((page) => page.keys) ?? [],
    [keysQuery.data],
  );

  const revokeKeyMutation = useRevokeAPIKeyMutation({
    onSuccess: async () => {
      setKeyToRevoke(null);
      await invalidateAllListAPIKeys(queryClient);
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
      key: "policyGrants",
      header: "MCP access",
      width: "1fr",
      render: (key: Key) =>
        key.subjectUrn ? (
          <Text variant="body" className="text-muted-foreground">
            Set by its agent
          </Text>
        ) : (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setKeyForPolicy(key)}
            className="h-auto px-0 py-0 font-sans normal-case tracking-normal underline decoration-dotted underline-offset-4 hover:bg-transparent hover:decoration-solid"
          >
            <Button.Text className="font-sans normal-case tracking-normal">
              {keyPolicySummary(key)}
            </Button.Text>
          </Button>
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

  // Only an unfiltered empty list means the organization has no keys. An
  // empty search result keeps the toolbar so the search can be changed.
  const hasNoKeys = !keysQuery.isPending && search === "" && keys.length === 0;

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
          debounceMs: SEARCH_DEBOUNCE_MS,
        }}
        isLoading={keysQuery.isPending}
        isEmpty={hasNoKeys}
        empty={{
          icon: "key-round",
          heading: "No API keys yet",
          action: createKeyButton,
        }}
      >
        {keys.length > 0 ? (
          <div
            className={cn(
              "transition-opacity",
              keysQuery.isPlaceholderData && "opacity-60",
            )}
            aria-busy={keysQuery.isPlaceholderData}
          >
            <Table
              columns={apiKeyColumns}
              data={keys}
              rowKey={(row) => row.id}
              className="max-h-[500px] overflow-y-auto"
            />
            <ApiKeysFooter
              count={keys.length}
              hasNextPage={keysQuery.hasNextPage}
              isFetchingNextPage={keysQuery.isFetchingNextPage}
              onLoadMore={() => {
                void keysQuery.fetchNextPage();
              }}
            />
          </div>
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

      {keyForPolicy ? (
        <KeyPolicySheet
          key={keyForPolicy.id}
          apiKey={keyForPolicy}
          onOpenChange={(open) => {
            if (!open) setKeyForPolicy(null);
          }}
        />
      ) : null}

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

function ApiKeysFooter({
  count,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
}: {
  count: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="flex items-center justify-between border-x border-b px-4 py-3">
      <Text muted small>
        {count.toLocaleString()} {count === 1 ? "key" : "keys"} shown
      </Text>
      {hasNextPage ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={onLoadMore}
          disabled={isFetchingNextPage}
        >
          {isFetchingNextPage ? (
            <>
              <Spinner className="mr-0 size-4" />
              Loading...
            </>
          ) : (
            "Load more"
          )}
        </Button>
      ) : null}
    </div>
  );
}
