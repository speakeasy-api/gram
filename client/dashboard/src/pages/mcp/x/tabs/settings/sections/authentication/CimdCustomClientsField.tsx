import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerCimdClient } from "@gram/client/models/components/usersessionissuercimdclient.js";
import { useCreateUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/createUserSessionIssuerCimdClient.js";
import { useDeleteUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/deleteUserSessionIssuerCimdClient.js";
import {
  invalidateAllUserSessionIssuerCimdClients,
  useUserSessionIssuerCimdClientsInfinite,
} from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { useVerifyUserSessionIssuerCimdClientURLMutation } from "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
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

  return (
    <Field data-invalid={addError ? true : undefined}>
      <FieldLabel htmlFor={inputId}>Custom CIMD Client URLs</FieldLabel>

      {/* One bordered region holds both the allowed URLs and the control
          that adds to them, so it is unambiguous which list the input
          writes to. */}
      <div className="border-border rounded-md border">
        <CustomClientList
          clients={clients}
          isLoading={query.isLoading || (hasNextPage && !isFetchNextPageError)}
          isError={query.isError || isFetchNextPageError}
          removingId={
            remove.isPending ? remove.variables?.request.id : undefined
          }
          onRemove={(id) => remove.mutate({ request: { id } })}
        />

        <div className="border-border border-t p-3">
          <RequireScope
            scope="project:write"
            level="component"
            className="w-full"
          >
            {({ disabled }) => (
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                {/* Input applies className to its inner field container, not
                    the outer wrapper, so the wrapper is what has to grow.
                    CIMD document URLs are long and need the width. */}
                <div className="min-w-0 flex-1">
                  <Input
                    id={inputId}
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
                    {verify.isPending ? "Verifying" : "Verify"}
                  </Button.Text>
                </Button>
                <Button
                  variant="secondary"
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
                    {create.isPending ? "Adding" : "Add"}
                  </Button.Text>
                </Button>
              </div>
            )}
          </RequireScope>

          {/* Only Verify fetches the document. Showing this during Add would
              claim a check the server deliberately stopped performing. */}
          {verify.isPending && (
            <Text muted small className="mt-2 block">
              Checking that the document is reachable and valid…
            </Text>
          )}

          {addError && <FieldError className="mt-2">{addError}</FieldError>}
        </div>
      </div>

      <FieldDescription>
        Allow additional client ID metadata document URLs beyond the verified
        list. The URL must exactly match.
      </FieldDescription>
    </Field>
  );
}

function CustomClientList({
  clients,
  isLoading,
  isError,
  removingId,
  onRemove,
}: {
  clients: UserSessionIssuerCimdClient[];
  isLoading: boolean;
  isError: boolean;
  removingId: string | undefined;
  onRemove: (id: string) => void;
}) {
  if (isLoading) {
    return (
      <Text muted small className="block p-3">
        Loading custom client URLs…
      </Text>
    );
  }

  // Never fall through to the empty state on a failed fetch: "no custom
  // client URLs" is an affirmative claim about the issuer's policy, and an
  // operator who believes it may re-add entries that already exist.
  if (isError) {
    return (
      <div className="p-3">
        <Alert variant="error" dismissible={false}>
          Could not load the custom client URLs for this issuer.
        </Alert>
      </div>
    );
  }

  // No enforcement claim in the empty state: what an empty list means
  // depends on the admission mode, and this panel also renders against an
  // unsaved selection, before that mode applies to anything.
  if (clients.length === 0) {
    return (
      <Text muted small className="block p-3">
        No custom client URLs are currently configured.
      </Text>
    );
  }

  // Divided rows rather than a Table: one column of content, and the URLs
  // need to wrap rather than truncate.
  return (
    <ul className="divide-border divide-y">
      {clients.map((client) => (
        <li key={client.id} className="p-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="min-w-0 flex-1">
              <Text muted mono variant="small" className="break-all">
                {client.clientIdMetadataUri}
              </Text>
            </div>
            <RequireScope scope="project:write" level="component">
              {({ disabled }) => (
                <div className="flex shrink-0 items-center gap-2">
                  <Button
                    size="md"
                    variant="destructive-secondary"
                    aria-label={`Remove ${client.clientIdMetadataUri}`}
                    disabled={disabled || removingId === client.id}
                    onClick={() => onRemove(client.id)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Remove</Button.Text>
                  </Button>
                </div>
              )}
            </RequireScope>
          </div>
        </li>
      ))}
    </ul>
  );
}
