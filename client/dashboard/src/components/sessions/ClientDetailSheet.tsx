import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { toast } from "sonner";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { useRefreshUserSessionClientCIMDMutation } from "@gram/client/react-query/refreshUserSessionClientCIMD.js";
import {
  invalidateUserSessionClient,
  setUserSessionClientData,
  useUserSessionClient,
} from "@gram/client/react-query/userSessionClient.js";
import { invalidateAllUserSessionClients } from "@gram/client/react-query/userSessionClients.js";

import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { safeExternalHttpUrl } from "@/lib/safe-external-url";
import {
  CREDENTIAL_KIND_PRESENTATION,
  declaredAuthMethodValue,
} from "@/lib/user-session-client-credential";
import {
  clientDocumentOrigin,
  userSessionClientSource,
} from "@/lib/user-session-client-source";
import { ClientCredentialBadge } from "./ClientCredentialBadge";
import { ClientSourceBadge } from "./ClientSourceBadge";

/**
 * Everything Gram knows about one registered client: the shared registration
 * facts, and — for a CIMD-resolved client — the metadata document's cache
 * state (source URL, last read, expiry, validator) with a manual refresh.
 * DCR rows get the base detail without the CIMD panel.
 */
export function ClientDetailSheet({
  clientId,
  client,
  projectSlug,
  open,
  onOpenChange,
}: {
  /**
   * The registration to show. Enough on its own: only one of the surfaces that
   * opens this sheet holds a whole record, so the id is what they all share.
   */
  clientId: string;
  /**
   * The listing row, when the caller has one, rendered immediately while the
   * per-client query runs so opening the sheet never shows an empty panel.
   */
  client?: UserSessionClient;
  /**
   * Project the lookup is scoped to. Required from a surface whose route
   * carries no project slug, where the SDK would otherwise stamp the request
   * with the literal "default" and the lookup would miss; a route that names
   * its project can leave this unset.
   */
  projectSlug?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const detailQuery = useUserSessionClient(
    { id: clientId, gramProject: projectSlug },
    undefined,
    { enabled: open },
  );
  const detail = detailQuery.data ?? client;

  if (!detail) {
    return (
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>Registration</SheetTitle>
            <SheetDescription>
              {detailQuery.isError
                ? "This registration could not be loaded."
                : "Loading…"}
            </SheetDescription>
          </SheetHeader>
          {detailQuery.isError ? null : (
            <div className="flex flex-col gap-4 px-4 pb-6">
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          )}
        </SheetContent>
      </Sheet>
    );
  }

  const origin = clientDocumentOrigin(detail);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <div className="flex items-center gap-2">
            <SheetTitle className="truncate" title={detail.clientName}>
              {detail.clientName}
            </SheetTitle>
            <ClientSourceBadge client={detail} />
            <ClientCredentialBadge
              kind={detail.credentialKind}
              declaredMethod={detail.tokenEndpointAuthMethod}
            />
          </div>
          {/* client_name is client-chosen; the origin is the part of a CIMD
              client's identity it cannot forge, so it stays in view here just
              as it does on the listing row. */}
          <SheetDescription className="break-all">
            {origin ?? detail.clientId}
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-col gap-6 px-4 pb-6">
          <div className="flex flex-col gap-4">
            <DetailField label="Client ID">
              <Text small className="font-mono break-all">
                {detail.clientId}
              </Text>
            </DetailField>
            <DetailField label="Registered">
              <Text small>{format(detail.clientIdIssuedAt, "PP p")}</Text>
            </DetailField>
            {/* Stated for every kind, unlike the listing, which badges only the
                two worth interrupting a scan for. This is where "public" and
                "secret" become distinguishable, and where the raw protocol
                value is written out rather than left to a tooltip. */}
            <DetailField label="Authentication">
              <Text small>
                {CREDENTIAL_KIND_PRESENTATION[detail.credentialKind].label}
              </Text>
              <Text small muted className="font-mono">
                {declaredAuthMethodValue(detail.tokenEndpointAuthMethod)}
              </Text>
            </DetailField>
            <DetailField label="Active sessions">
              <Text small>{detail.activeSessionCount}</Text>
            </DetailField>
            <DetailField label="Redirect URIs">
              <RedirectUriList uris={detail.redirectUris} />
            </DetailField>
          </div>

          {userSessionClientSource(detail) === "cimd" && (
            <CimdMetadataPanel client={detail} />
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

/**
 * The metadata document's cache state plus the manual refresh. Rendered only
 * for CIMD-resolved rows; every field here is null on a DCR registration.
 */
function CimdMetadataPanel({
  client,
}: {
  client: UserSessionClient;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const project = useProject();
  // Refresh is a write mutation the backend gates on project:write for THIS
  // project; an unscoped hasScope is existential across every project the
  // user holds grants in. Mirrors the listing's Revoke gating.
  const canRefresh = hasScope("project:write", project.id);

  const refresh = useRefreshUserSessionClientCIMDMutation({
    onSuccess: async (data) => {
      // The endpoint returns the freshly re-read view; seed it so the sheet
      // updates without waiting on a refetch, then invalidate the listing,
      // whose rows carry the same fields.
      setUserSessionClientData(queryClient, [{ id: client.id }], data);
      await invalidateAllUserSessionClients(queryClient, {
        refetchType: "all",
      });
      toast.success("Client metadata refreshed");
    },
    onError: async (error) => {
      toast.error(refreshErrorMessage(error));
      // A rejected refresh may still have mutated the row: the purge commits
      // before the fetch, deliberately, so a failed re-read leaves the cache
      // cleared. Refetch so the panel shows the server's actual state (no
      // expiry, no validator) instead of the pre-purge copy. Pre-purge
      // rejections (cooldown, DCR, missing) refetch unchanged data, which is
      // harmless.
      await Promise.all([
        invalidateUserSessionClient(queryClient, [{ id: client.id }], {
          refetchType: "all",
        }),
        invalidateAllUserSessionClients(queryClient, {
          refetchType: "all",
        }),
      ]);
    },
  });

  return (
    <div className="border-border flex flex-col gap-4 border-t pt-4">
      <Text variant="subheading" className="text-sm">
        Metadata document
      </Text>
      <Text small muted>
        This client is identified by a metadata document it hosts. Speakeasy
        caches the document and enforces the values extracted from it;
        refreshing discards the cached copy and re-reads the document in full.
      </Text>
      <div className="flex flex-col gap-4">
        <DetailField label="Source URL">
          <SourceUrlValue raw={client.clientIdMetadataUri} />
        </DetailField>
        <DetailField label="Last fetched">
          {/* The last successful read: a 304 revalidation counts, so this can
              be newer than the last time a body was downloaded. */}
          <Text small>
            {formatCacheTimestamp(client.clientIdMetadataFetchedAt)}
          </Text>
        </DetailField>
        <DetailField label="Cache expires">
          <Text small>
            {formatCacheTimestamp(client.clientIdMetadataCacheExpiresAt)}
          </Text>
        </DetailField>
        <DetailField label="ETag">
          <ETagValue etag={client.clientIdMetadataEtag} />
        </DetailField>
      </div>
      {canRefresh && (
        <Button
          variant="secondary"
          size="sm"
          className="self-start"
          disabled={refresh.isPending}
          onClick={() => refresh.mutate({ request: { id: client.id } })}
        >
          {refresh.isPending ? "Refreshing…" : "Refresh metadata"}
        </Button>
      )}
    </div>
  );
}

function DetailField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <div className="text-eyebrow">{label}</div>
      {children}
    </div>
  );
}

function RedirectUriList({ uris }: { uris: string[] }): JSX.Element {
  if (uris.length === 0) {
    return (
      <Text small muted>
        None registered
      </Text>
    );
  }
  return (
    <div className="flex flex-col gap-1">
      {uris.map((uri) => (
        <Text key={uri} small className="font-mono break-all">
          {uri}
        </Text>
      ))}
    </div>
  );
}

/**
 * The document URL, linkified only when it parses as absolute http(s) — an
 * unparseable stored value still renders as text so an operator can see what
 * is wrong with it. Mirrors the issuer documentation-URL convention.
 */
function SourceUrlValue({
  raw,
}: {
  raw: string | null | undefined;
}): JSX.Element {
  const safeUrl = safeExternalHttpUrl(raw);
  if (!safeUrl) {
    return (
      <Text small className="font-mono break-all">
        {raw ?? "—"}
      </Text>
    );
  }
  return (
    <a
      href={safeUrl}
      target="_blank"
      rel="noopener noreferrer"
      className="hover:text-primary text-sm break-all hover:underline"
    >
      {raw}
    </a>
  );
}

function ETagValue({ etag }: { etag: string | undefined }): JSX.Element {
  if (!etag) {
    return (
      <Text small muted>
        None — the next read will be unconditional
      </Text>
    );
  }
  return (
    <Text small className="font-mono break-all">
      {etag}
    </Text>
  );
}

function formatCacheTimestamp(value: Date | undefined): string {
  return value ? format(value, "PP p") : "—";
}

function refreshErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return `Refresh failed: ${error.message}`;
  }
  return "Refresh failed";
}
