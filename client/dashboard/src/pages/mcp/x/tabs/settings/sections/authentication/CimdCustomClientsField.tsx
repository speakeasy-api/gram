import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useCimdClientPresets } from "@gram/client/react-query/cimdClientPresets.js";
import type { UserSessionIssuerCimdClient } from "@gram/client/models/components/usersessionissuercimdclient.js";
import { useCreateUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/createUserSessionIssuerCimdClient.js";
import { useDeleteUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/deleteUserSessionIssuerCimdClient.js";
import {
  invalidateAllUserSessionIssuerCimdClients,
  useUserSessionIssuerCimdClientsInfinite,
} from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { useVerifyUserSessionIssuerCimdClientURLMutation } from "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { toast } from "sonner";

export function CimdCustomClientsField({
  userSessionIssuer,
}: {
  userSessionIssuer: UserSessionIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const inputId = useId();
  const [draftUrl, setDraftUrl] = useState("");
  const [addError, setAddError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const presets = useCimdClientPresets();

  const query = useUserSessionIssuerCimdClientsInfinite({
    userSessionIssuerId: userSessionIssuer.id,
  });
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = query;

  // Drain every page: the list is rendered whole, and a single-page fetch
  // would silently hide entries once an issuer crosses the default page
  // size.
  //
  // isFetchNextPageError is load-bearing, not defensive. hasNextPage stays
  // true after a page fails, so without it this effect re-fires the moment
  // isFetchingNextPage drops back to false and retries forever, while the
  // list below reports a permanent "Loading…". Stop on the failure and let
  // the list render an error instead.
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
      void fetchNextPage();
    }
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);

  const clients = useMemo<UserSessionIssuerCimdClient[]>(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  const invalidate = () =>
    invalidateAllUserSessionIssuerCimdClients(queryClient, {
      refetchType: "all",
    });

  const create = useCreateUserSessionIssuerCimdClientMutation({
    onSuccess: async () => {
      await invalidate();
      setDraftUrl("");
      setAddError(null);
      setAdding(false);
      toast.success("Client URL allowed");
    },
    onError: (error) => {
      setAddError(
        error instanceof Error ? error.message : "Failed to allow client URL",
      );
    },
  });

  // Pre-flight for the add: the same fetch and validation the authorization
  // server performs, so an operator gets assurance before committing a URL
  // rather than a warning toast after it is already saved.
  const verify = useVerifyUserSessionIssuerCimdClientURLMutation({
    onSuccess: (result) => {
      if (result.verified) {
        toast.success(
          result.clientName
            ? `Verified: ${result.clientName}`
            : "Verified: the document is reachable and valid",
        );
        return;
      }
      toast.error(result.detail);
    },
    // Toast, not inline: a refused probe and a refused request (rate limit,
    // authorization, transport) are both outcomes of the Verify action, so
    // they belong in the same place. Inline errors are reserved for what is
    // wrong with the field itself, which is Add's syntax rejection.
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to verify client URL",
      );
    },
  });

  const remove = useDeleteUserSessionIssuerCimdClientMutation({
    onSuccess: async () => {
      await invalidate();
      toast.success("Client URL removed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to remove client URL",
      );
    },
  });

  const trimmedUrl = draftUrl.trim();

  const handleAdd = () => {
    if (!trimmedUrl || create.isPending) return;

    // The create endpoint is idempotent — a repeat URL returns 200 with the
    // existing row and records no audit event — so a duplicate would toast
    // "allowed" while nothing happened. Catch it here instead.
    if (clients.some((client) => client.clientIdMetadataUri === trimmedUrl)) {
      setAddError("This URL is already allowed on this issuer.");
      return;
    }

    setAddError(null);
    create.mutate({
      request: {
        createUserSessionIssuerCimdClientForm: {
          userSessionIssuerId: userSessionIssuer.id,
          clientIdMetadataUri: trimmedUrl,
        },
      },
    });
  };

  const handleVerify = () => {
    if (!trimmedUrl || verify.isPending) return;
    setAddError(null);
    verify.mutate({
      request: { verifyURLRequestBody: { clientIdMetadataUri: trimmedUrl } },
    });
  };

  const busy = create.isPending || verify.isPending;

  const verified = (presets.data?.items ?? []).filter(
    (preset) => preset.enabled,
  );

  // One table, verified first: an operator asking "who can connect?" wants a
  // single answer, not a curated list in one panel and their own additions
  // in another. The Source column is what tells the two apart.
  const rows: AllowedClientRow[] = [
    ...verified.map((preset) => ({
      id: `preset:${preset.clientIdMetadataUri}`,
      name: preset.displayName,
      url: preset.clientIdMetadataUri,
      custom: false as const,
    })),
    ...clients.map((client) => ({
      id: client.id,
      name: null,
      url: client.clientIdMetadataUri,
      custom: true as const,
    })),
  ];

  const listLoading =
    presets.isLoading ||
    query.isLoading ||
    (hasNextPage && !isFetchNextPageError);
  const listError = query.isError || isFetchNextPageError;

  const columns: Column<AllowedClientRow>[] = [
    {
      key: "url",
      header: "Client",
      width: "3fr",
      render: (row) => (
        <div className="min-w-0">
          {row.name && (
            <Text small className="block truncate font-medium">
              {row.name}
            </Text>
          )}
          <Text muted mono variant="small" className="block truncate">
            {row.url}
          </Text>
        </div>
      ),
    },
    {
      key: "source",
      header: "Source",
      width: "1fr",
      render: (row) => (
        <Badge variant={row.custom ? "neutral" : "success"} size="sm">
          {row.custom ? "Custom" : "Speakeasy"}
        </Badge>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "auto",
      render: (row) =>
        row.custom ? (
          <RequireScope scope="project:write" level="component">
            {({ disabled }) => (
              <Button
                size="sm"
                variant="tertiary"
                aria-label={`Remove ${row.url}`}
                disabled={disabled || remove.variables?.request.id === row.id}
                onClick={() => remove.mutate({ request: { id: row.id } })}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-3.5" />
                </Button.LeftIcon>
                <Button.Text>Remove</Button.Text>
              </Button>
            )}
          </RequireScope>
        ) : null,
    },
  ];

  return (
    <div
      role="group"
      aria-label="Allowed clients"
      data-invalid={addError ? true : undefined}
      className="flex h-full flex-col space-y-2"
    >
      {/* Never fall through to an empty table on a failed fetch: "nothing is
          allowed here" is an affirmative claim about the issuer's policy,
          and an operator who believes it may re-add entries that exist. */}
      {listError ? (
        <Alert variant="error" dismissible={false}>
          Could not load the clients allowed on this issuer.
        </Alert>
      ) : (
        <div className="flex max-h-full flex-col border">
          {/* The scroll lives on the table itself, not a wrapper: the table
              is already its own overflow context, so a sticky header inside
              it only sticks when the table is what scrolls. */}
          <Table
            className="[&_thead]:bg-card max-h-[48vh] min-h-0 flex-1 overflow-y-auto border-0 [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
            columns={columns}
            data={rows}
            rowKey={(row) => row.id}
            noResultsMessage={
              listLoading ? "Loading…" : "No clients are allowed yet."
            }
          />

          {/* The table's footer: adding belongs to the list it writes to,
              and it stays put while the list scrolls past it. */}
          <div className="bg-muted/20 shrink-0 border-t p-2">
            <RequireScope
              scope="project:write"
              level="component"
              className="w-full"
            >
              {({ disabled }) =>
                adding ? (
                  <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                    <div className="min-w-0 flex-1">
                      <Input
                        id={inputId}
                        aria-label="Client ID metadata document URL"
                        autoFocus
                        value={draftUrl}
                        onChange={(value) => {
                          setDraftUrl(value);
                          setAddError(null);
                        }}
                        onEnter={handleAdd}
                        placeholder="https://example.com/oauth/client-metadata.json"
                        disabled={disabled || busy}
                      />
                    </div>
                    <Button
                      variant="tertiary"
                      size="md"
                      disabled={disabled || !trimmedUrl || busy}
                      onClick={handleVerify}
                    >
                      {verify.isPending && (
                        <Button.LeftIcon>
                          <Loader2
                            aria-hidden="true"
                            className="size-4 animate-spin"
                          />
                        </Button.LeftIcon>
                      )}
                      <Button.Text>
                        {verify.isPending ? "Verifying" : "Check it works"}
                      </Button.Text>
                    </Button>
                    <Button
                      variant="primary"
                      size="md"
                      disabled={disabled || !trimmedUrl || busy}
                      onClick={handleAdd}
                    >
                      {create.isPending && (
                        <Button.LeftIcon>
                          <Loader2
                            aria-hidden="true"
                            className="size-4 animate-spin"
                          />
                        </Button.LeftIcon>
                      )}
                      <Button.Text>
                        {create.isPending ? "Adding" : "Allow"}
                      </Button.Text>
                    </Button>
                  </div>
                ) : (
                  <Button
                    variant="tertiary"
                    size="md"
                    disabled={disabled}
                    onClick={() => setAdding(true)}
                  >
                    <Button.LeftIcon>
                      <Plus className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add new</Button.Text>
                  </Button>
                )
              }
            </RequireScope>

            {/* Only Verify fetches the document. Showing this during Add
                would claim a check the server deliberately stopped
                performing. */}
            {verify.isPending && (
              <Text muted small className="mt-2 block">
                Checking that the document is reachable and valid…
              </Text>
            )}

            {addError && <FieldError className="mt-2">{addError}</FieldError>}
          </div>
        </div>
      )}
    </div>
  );
}

type AllowedClientRow = {
  id: string;
  name: string | null;
  url: string;
  custom: boolean;
};
