import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";
import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient } from "@/contexts/Sdk";
import { handleError } from "@/lib/errors";
import {
  proxyRegisterUpstreamClient,
  registrationProvenance,
  type ProxyRegisteredClient,
} from "@/lib/proxyRegisterUpstreamClient";
import { RadioRows } from "@/lib/remote-identity";
import { useRoutes } from "@/routes";
import {
  DuplicateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod,
  type DuplicateOrganizationRemoteSessionClientForm,
} from "@gram/client/models/components/duplicateorganizationremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { invalidateAllOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { isKnownEnumValue } from "@/lib/enum";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { dynamicClientRegistrationAvailability } from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { remoteSessionClientDisplayName } from "./clientDisplay";
import { ClientCard } from "./tabs/client/ClientCard";

type Credentials = "copy" | "register";

// signsWithRegisteredKeySet mirrors the server's refusal: a private_key_jwt
// client holding a provider-issued client_id is known to the provider by its
// own key set address, which a copy cannot take over. A client metadata
// document client publishes its key set in its document, so its copy can.
function signsWithRegisteredKeySet(client: RemoteSessionClient): boolean {
  // The SDK enum does not list private_key_jwt until it becomes selectable,
  // but the server already stores it.
  const authMethod: string | undefined = client.tokenEndpointAuthMethod;
  return !client.clientIdMetadataUri && authMethod === "private_key_jwt";
}

// canRegisterReplacement mirrors the server's rule for a copy with its own
// registration: only a client that signs users in with a client secret or no
// client authentication can be reproduced by dynamic registration. A client
// metadata document client always gets its own document instead.
function canRegisterReplacement(client: RemoteSessionClient): boolean {
  return (
    !client.clientIdMetadataUri &&
    client.credentialOwner !== "self" &&
    !signsWithRegisteredKeySet(client)
  );
}

// duplicateRefusal turns a refusal the dialog cannot rule out in advance into
// plain words, or returns null for errors that belong in a toast.
function duplicateRefusal(error: unknown): string | null {
  if (!(error instanceof GramError) || error.statusCode !== 409) return null;
  if (error.message.includes("identity provider connection")) {
    return "This client is managed by an identity provider connection, so it can't be duplicated. Change it from that connection instead.";
  }
  return "This client changed while it was being duplicated. Close this dialog, reload the page, and try again.";
}

// registrationAuthMethod narrows a token endpoint auth method to the ones a
// duplicate's registration may declare, dropping anything else so the server
// keeps the source's method.
function registrationAuthMethod(
  method: string | undefined,
):
  | DuplicateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod
  | undefined {
  return isKnownEnumValue(
    DuplicateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod,
    method,
  )
    ? method
    : undefined;
}

// DuplicateClientFlow confirms a duplicate of client, then opens the copy in
// an edit sheet, since a copy is usually made to change something about it.
// MCP servers keep using the source.
export function DuplicateClientFlow({
  client,
  issuer,
  issuerId,
  onClose,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  issuerId: string;
  onClose: () => void;
}): JSX.Element {
  const [duplicateId, setDuplicateId] = useState<string | null>(null);

  if (duplicateId) {
    return (
      <DuplicatedClientSheet
        clientId={duplicateId}
        issuer={issuer}
        issuerId={issuerId}
        onClose={onClose}
      />
    );
  }

  return (
    <DuplicateClientDialog
      client={client}
      issuer={issuer}
      onClose={onClose}
      onDuplicated={(created) => setDuplicateId(created.id)}
    />
  );
}

function DuplicateClientDialog({
  client,
  issuer,
  onClose,
  onDuplicated,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  onClose: () => void;
  onDuplicated: (created: RemoteSessionClient) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const sdk = useSdkClient();
  const organization = useOrganization();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { fetch: authedFetch } = useFetcher();

  const issuerName = issuer?.name || "the authorization server";
  const registrationEndpoint = issuer?.registrationEndpoint?.trim() ?? "";
  const { available: dcrAvailable } = dynamicClientRegistrationAvailability({
    registrationEndpoint,
    tunneled: !!issuer?.tunneledMcpServerId,
    isPlatformAdmin,
  });
  const issuerProjectSlug = organization.projects.find(
    (project) => project.id === issuer?.projectId,
  )?.slug;
  // A tunneled provider is reached through its project, so a registration
  // needs that project's slug, as in the create sheet.
  const tunnelRouteReady =
    !issuer?.tunneledMcpServerId || issuerProjectSlug !== undefined;
  const metadataDocument = !!client.clientIdMetadataUri;
  const keySetBound = signsWithRegisteredKeySet(client);
  const offerRegistration =
    dcrAvailable && tunnelRouteReady && canRegisterReplacement(client);
  const copyRefused = !metadataDocument && !!client.upstreamRejectedAt;

  let blocked: string | null = null;
  if (keySetBound) {
    blocked = `This client signs in with a key set that ${issuerName} knows by this client's own address, so a copy couldn't sign in. Create a new client instead.`;
  } else if (copyRefused && issuer && !offerRegistration) {
    blocked = `${issuerName} no longer accepts this client's credentials, and Speakeasy can't register a new client with it. Create a new client instead.`;
  }
  // The provider decides whether a rejected client can be registered again,
  // so wait for it rather than briefly showing the wrong answer.
  const waitingForIssuer = copyRefused && !issuer;

  const [credentials, setCredentials] = useState<Credentials>(
    copyRefused ? "register" : "copy",
  );
  // A registration made for an attempt that then failed is reused on retry, so
  // each retry does not leave another client at the provider.
  const registeredRef = useRef<ProxyRegisteredClient | null>(null);

  const duplicate = useMutation({
    mutationFn: async (): Promise<{
      created: RemoteSessionClient;
      unsupportedAuthMethod: string | null;
    }> => {
      const form: DuplicateOrganizationRemoteSessionClientForm = {
        id: client.id,
      };
      let unsupportedAuthMethod: string | null = null;
      if (offerRegistration && credentials === "register") {
        const registered =
          registeredRef.current ??
          (await proxyRegisterUpstreamClient(authedFetch, {
            registrationEndpoint,
            // RFC 7591 §2: scope is a space-separated string at registration
            // time.
            scope:
              client.scope && client.scope.length > 0
                ? client.scope.join(" ")
                : undefined,
            tokenEndpointAuthMethod: registrationAuthMethod(
              client.tokenEndpointAuthMethod,
            ),
            tunneledMcpServerId: issuer?.tunneledMcpServerId,
            projectSlug: issuerProjectSlug,
          }));
        registeredRef.current = registered;
        const registeredMethod =
          registered.tokenEndpointAuthMethod ?? undefined;
        form.clientId = registered.clientId;
        form.clientSecret = registered.clientSecret || undefined;
        form.tokenEndpointAuthMethod = registrationAuthMethod(registeredMethod);
        if (registeredMethod && !form.tokenEndpointAuthMethod) {
          unsupportedAuthMethod = registeredMethod;
        }
        Object.assign(form, registrationProvenance(registered));
      }
      const created = await sdk.organizationRemoteSessionClients.duplicate({
        duplicateOrganizationRemoteSessionClientForm: form,
      });
      return { created, unsupportedAuthMethod };
    },
    onSuccess: async ({ created, unsupportedAuthMethod }) => {
      registeredRef.current = null;
      await Promise.all([
        invalidateAllOrganizationRemoteSessionClients(queryClient, {
          refetchType: "all",
        }),
        // The provider list shows a client count per provider.
        invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
      ]);
      toast.success("Client duplicated");
      if (unsupportedAuthMethod) {
        toast.warning(
          `The issuer registered the client with an unsupported token endpoint auth method (${unsupportedAuthMethod}); the copy keeps this client's method instead.`,
        );
      }
      onDuplicated(created);
    },
    onError: (error) => {
      if (duplicateRefusal(error)) return;
      handleError(error, { title: "Couldn't duplicate client" });
    },
  });

  const description = metadataDocument
    ? "The copy gets its own client metadata document and client ID, with this client's scopes, audience, and authentication."
    : "The copy gets this client's scopes, audience, authentication method, and key set. MCP servers keep using this client.";

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Duplicate client "${remoteSessionClientDisplayName(client)}"?`}
      description={description}
      confirmLabel="Duplicate"
      pendingLabel="Duplicating…"
      confirmVariant="primary"
      isPending={duplicate.isPending}
      confirmDisabled={blocked !== null || waitingForIssuer}
      error={blocked ?? duplicateRefusal(duplicate.error) ?? undefined}
      onConfirm={() => duplicate.mutate()}
    >
      {offerRegistration && !keySetBound && (
        <RadioRows
          label="Credentials for the copy"
          value={credentials}
          onValueChange={(value) => setCredentials(value as Credentials)}
          disabled={duplicate.isPending}
          rows={[
            {
              value: "copy",
              title: "Copy credentials",
              description: `Reuse this client's ID and secret, so the copy signs in through the same app at ${issuerName}.`,
              disabled: copyRefused,
              badge: copyRefused ? (
                <Text small muted as="span">
                  No longer accepted
                </Text>
              ) : undefined,
            },
            {
              value: "register",
              title: "Register a new client",
              description: `Speakeasy registers a separate client with ${issuerName}, so the copy gets its own ID and secret.`,
            },
          ]}
        />
      )}
    </ConfirmDialog>
  );
}

// DuplicatedClientSheet shows the new copy's settings for editing right after
// it is created, without leaving the page the duplicate started from. Leaving
// with edits the card has not saved asks first.
function DuplicatedClientSheet({
  clientId,
  issuer,
  issuerId,
  onClose,
}: {
  clientId: string;
  issuer: RemoteSessionIssuer | undefined;
  issuerId: string;
  onClose: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const {
    data: client,
    isError,
    refetch,
  } = useOrganizationRemoteSessionClient({
    id: clientId,
  });
  const [dirty, setDirty] = useState(false);
  const [pendingLeave, setPendingLeave] = useState<"close" | "view" | null>(
    null,
  );
  const clientHref = routes.remoteIdentityProviders.clientDetail.href(
    issuerId,
    clientId,
  );

  const leave = (to: "close" | "view") => {
    onClose();
    if (to === "view") void navigate(clientHref);
  };
  const requestLeave = (to: "close" | "view") => {
    if (dirty) {
      setPendingLeave(to);
      return;
    }
    leave(to);
  };

  let body: JSX.Element;
  if (client) {
    body = (
      <ClientCard
        client={client}
        issuer={issuer}
        issuerId={issuerId}
        onDirtyChange={setDirty}
      />
    );
  } else if (isError) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the copy"
        description="The copy was created. Try again, or open it from the client list."
        action={
          <Button variant="secondary" onClick={() => void refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    );
  } else {
    body = <Skeleton className="h-96 w-full" />;
  }

  return (
    <>
      <Sheet
        open
        onOpenChange={(open) => {
          if (!open) requestLeave("close");
        }}
      >
        <SheetContent
          side="right"
          className="flex w-[560px] flex-col sm:max-w-[560px]"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Edit the copy
            </SheetTitle>
            <SheetDescription>
              No MCP server uses this client yet. Change what should differ from
              the original, then save.
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 overflow-y-auto px-6 py-6">{body}</div>

          <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
            <Button variant="tertiary" asChild>
              <Link
                to={clientHref}
                onClick={(event) => {
                  event.preventDefault();
                  requestLeave("view");
                }}
              >
                <Button.Text>View client</Button.Text>
              </Link>
            </Button>
            <Button variant="secondary" onClick={() => requestLeave("close")}>
              <Button.Text>Close</Button.Text>
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={pendingLeave !== null}
        onOpenChange={(open) => {
          if (!open) setPendingLeave(null);
        }}
        title="Discard unsaved changes?"
        description="The copy keeps the settings it was created with. Your unsaved edits are lost."
        confirmLabel="Discard changes"
        onConfirm={() => {
          if (pendingLeave) leave(pendingLeave);
        }}
      />
    </>
  );
}
