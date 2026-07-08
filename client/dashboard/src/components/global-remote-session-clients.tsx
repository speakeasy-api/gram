// Super-admin workflow for managing *global* remote session providers — a
// remote_session_issuer (RSI) paired with its remote_session_client (RSC), both
// with no owning project AND no owning organization (project_id IS NULL AND
// organization_id IS NULL), available platform-wide. Backed by the
// adminRemoteSessions service (platform-admin only). A "provider" in this UI is
// one issuer; the 1:1 create flow registers exactly one client under it, but an
// issuer can legitimately carry more than one client and the detail view reads
// (and displays) all of them rather than hiding data.
//
// GlobalRSCsModal is the stateful container: it owns selection, the editable
// draft, and the create/update/delete orchestration. The render is composed
// from presentational sub-components (ProviderList, IssuerFields, ClientFields,
// ExtraClients, ProviderMeta, ProviderFooter) that take a draft + callbacks and
// hold no state of their own.
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useCreateGlobalRemoteSessionClientMutation } from "@gram/client/react-query/createGlobalRemoteSessionClient.js";
import { useCreateGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/createGlobalRemoteSessionIssuer.js";
import { useDeleteGlobalRemoteSessionClientMutation } from "@gram/client/react-query/deleteGlobalRemoteSessionClient.js";
import { useDeleteGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/deleteGlobalRemoteSessionIssuer.js";
import {
  invalidateAllGlobalRemoteSessionClients,
  useGlobalRemoteSessionClients,
} from "@gram/client/react-query/globalRemoteSessionClients.js";
import {
  invalidateAllGlobalRemoteSessionIssuers,
  useGlobalRemoteSessionIssuers,
} from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { useUpdateGlobalRemoteSessionClientMutation } from "@gram/client/react-query/updateGlobalRemoteSessionClient.js";
import { useUpdateGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/updateGlobalRemoteSessionIssuer.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

type AuthMethod = CreateRemoteSessionClientFormTokenEndpointAuthMethod;
const { ClientSecretBasic, ClientSecretPost, None } =
  CreateRemoteSessionClientFormTokenEndpointAuthMethod;

const AUTH_METHODS: AuthMethod[] = [ClientSecretBasic, ClientSecretPost, None];

// Comma-separated text <-> string[]. Mirrors the OAuth wizard's parseScopes.
function parseList(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function formatDate(d: Date | null | undefined): string {
  if (!d) return "—";
  return d.toISOString().slice(0, 10);
}

function issuerLabel(i: RemoteSessionIssuer): string {
  return (
    (i.name ?? "").trim() || i.slug.trim() || i.issuer.trim() || "Untitled"
  );
}

function errMessage(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "Something went wrong";
}

type Draft = {
  // Issuer (RSI)
  issuerName: string;
  slug: string;
  issuer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  scopesSupportedText: string;
  oidc: boolean;
  passthrough: boolean;
  // Client (RSC)
  clientId: string;
  secret: string;
  authMethod: AuthMethod;
  scopeText: string;
  audience: string;
};

const EMPTY_DRAFT: Draft = {
  issuerName: "",
  slug: "",
  issuer: "",
  authorizationEndpoint: "",
  tokenEndpoint: "",
  registrationEndpoint: "",
  jwksUri: "",
  scopesSupportedText: "",
  oidc: false,
  passthrough: false,
  clientId: "",
  secret: "",
  authMethod: ClientSecretBasic,
  scopeText: "",
  audience: "",
};

// Build a Draft from a loaded issuer + its primary (first) client. Client fields
// stay empty when the issuer has no client yet (orphan from a failed 1:1 create);
// saving then registers a client instead of updating one.
function draftFrom(
  issuer: RemoteSessionIssuer,
  client: RemoteSessionClient | null,
): Draft {
  return {
    issuerName: issuer.name ?? "",
    slug: issuer.slug,
    issuer: issuer.issuer,
    authorizationEndpoint: issuer.authorizationEndpoint ?? "",
    tokenEndpoint: issuer.tokenEndpoint ?? "",
    registrationEndpoint: issuer.registrationEndpoint ?? "",
    jwksUri: issuer.jwksUri ?? "",
    scopesSupportedText: (issuer.scopesSupported ?? []).join(", "),
    oidc: issuer.oidc,
    passthrough: issuer.passthrough,
    clientId: client?.clientId ?? "",
    secret: "", // write-only: never prefilled
    authMethod:
      (client?.tokenEndpointAuthMethod as AuthMethod) ?? ClientSecretBasic,
    scopeText: (client?.scope ?? []).join(", "),
    audience: client?.audience ?? "",
  };
}

// The management modal is mounted separately at the toolbar root (so toolbar
// collapse can't unmount it) — see PlatformAdminToolbarInner. Its launcher
// button lives in PlatformAdminGlobalPanel (platform-admin-panel.tsx).
export function GlobalRSCsModal({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();

  const [selectedIssuerId, setSelectedIssuerId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [query, setQuery] = useState("");

  const issuersQuery = useGlobalRemoteSessionIssuers(undefined, undefined, {
    enabled: open,
  });
  const issuers = issuersQuery.data?.result.items ?? [];

  const clientsQuery = useGlobalRemoteSessionClients(
    { remoteSessionIssuerId: selectedIssuerId ?? "" },
    undefined,
    { enabled: open && selectedIssuerId != null },
  );
  const clients = clientsQuery.data?.result.items ?? [];
  const primaryClient = clients[0] ?? null;

  const selectedIssuer = issuers.find((i) => i.id === selectedIssuerId) ?? null;
  const showForm = creating || selectedIssuer != null;

  // Re-derive the draft when the selection (issuer or its primary client) changes.
  // Keyed on the ids only, so unrelated refetches don't clobber in-progress edits;
  // when a freshly-selected issuer's clients load, primaryClient.id flips from
  // undefined to a real id and the client fields fill in.
  useEffect(() => {
    if (creating || !selectedIssuer) return;
    setDraft(draftFrom(selectedIssuer, primaryClient));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIssuerId, primaryClient?.id, creating]);

  const createIssuer = useCreateGlobalRemoteSessionIssuerMutation();
  const updateIssuer = useUpdateGlobalRemoteSessionIssuerMutation();
  const deleteIssuer = useDeleteGlobalRemoteSessionIssuerMutation();
  const createClient = useCreateGlobalRemoteSessionClientMutation();
  const updateClient = useUpdateGlobalRemoteSessionClientMutation();
  const deleteClient = useDeleteGlobalRemoteSessionClientMutation();

  const saving =
    createIssuer.isPending ||
    updateIssuer.isPending ||
    createClient.isPending ||
    updateClient.isPending;
  const deleting = deleteClient.isPending || deleteIssuer.isPending;

  const invalidateBoth = () =>
    Promise.all([
      invalidateAllGlobalRemoteSessionIssuers(queryClient),
      invalidateAllGlobalRemoteSessionClients(queryClient),
    ]);

  const q = query.trim().toLowerCase();
  const filteredIssuers = q
    ? issuers.filter(
        (i) =>
          issuerLabel(i).toLowerCase().includes(q) ||
          i.slug.toLowerCase().includes(q) ||
          i.issuer.toLowerCase().includes(q),
      )
    : issuers;

  const selectIssuer = (i: RemoteSessionIssuer) => {
    setCreating(false);
    setSelectedIssuerId(i.id);
    setDraft(draftFrom(i, null));
    setConfirmingDelete(false);
  };

  const startCreate = () => {
    setCreating(true);
    setSelectedIssuerId(null);
    setDraft(EMPTY_DRAFT);
    setConfirmingDelete(false);
  };

  const patchDraft = (patch: Partial<Draft>) =>
    setDraft((d) => ({ ...d, ...patch }));

  // Client ID is immutable once a client exists (the update form has no client_id
  // field), so it's only required when creating an issuer or adding a client to an
  // orphan issuer.
  const needsClientId = creating || primaryClient == null;
  const canSave =
    draft.slug.trim().length > 0 &&
    draft.issuer.trim().length > 0 &&
    (!needsClientId || draft.clientId.trim().length > 0);

  const issuerFields = () => ({
    name: draft.issuerName.trim(),
    slug: draft.slug.trim(),
    issuer: draft.issuer.trim(),
    authorizationEndpoint: draft.authorizationEndpoint.trim(),
    tokenEndpoint: draft.tokenEndpoint.trim(),
    registrationEndpoint: draft.registrationEndpoint.trim(),
    jwksUri: draft.jwksUri.trim(),
    scopesSupported: parseList(draft.scopesSupportedText),
    oidc: draft.oidc,
    passthrough: draft.passthrough,
  });

  // Client fields for a *new* client. Empty optionals are omitted, not sent as
  // explicit empties: "" audience fails the API's non-empty pattern, and an
  // omitted scope is the documented way to fall back to the issuer's
  // scopes_supported.
  const newClientFields = (remoteSessionIssuerId: string) => {
    const secret = draft.secret.trim();
    const scope = parseList(draft.scopeText);
    const audience = draft.audience.trim();
    return {
      remoteSessionIssuerId,
      clientId: draft.clientId.trim(),
      clientSecret: secret.length > 0 ? secret : undefined,
      tokenEndpointAuthMethod: draft.authMethod,
      scope: scope.length > 0 ? scope : undefined,
      audience: audience.length > 0 ? audience : undefined,
    };
  };

  const save = async () => {
    if (!canSave || saving) return;

    if (creating) {
      let issuer: RemoteSessionIssuer;
      try {
        issuer = await createIssuer.mutateAsync({
          request: { createRemoteSessionIssuerForm: issuerFields() },
        });
      } catch (e) {
        toast.error(errMessage(e));
        return;
      }
      try {
        await createClient.mutateAsync({
          request: {
            createGlobalRemoteSessionClientForm: newClientFields(issuer.id),
          },
        });
      } catch (e) {
        // Issuer landed but the client didn't — leave the orphan selected so the
        // operator can add a client or delete it.
        await invalidateAllGlobalRemoteSessionIssuers(queryClient);
        setCreating(false);
        setSelectedIssuerId(issuer.id);
        toast.error(
          `Issuer created, but client failed: ${errMessage(e)}. Add a client or delete the issuer.`,
        );
        return;
      }
      await invalidateBoth();
      setCreating(false);
      setSelectedIssuerId(issuer.id);
      toast.success("Provider created");
      return;
    }

    if (!selectedIssuer) return;
    try {
      await updateIssuer.mutateAsync({
        request: {
          updateRemoteSessionIssuerForm: {
            id: selectedIssuer.id,
            ...issuerFields(),
          },
        },
      });

      if (primaryClient) {
        const secret = draft.secret.trim();
        const audience = draft.audience.trim();
        await updateClient.mutateAsync({
          request: {
            updateRemoteSessionClientForm: {
              id: primaryClient.id,
              // Empty secret = keep current; non-empty = rotate.
              clientSecret: secret.length > 0 ? secret : undefined,
              tokenEndpointAuthMethod: draft.authMethod,
              // [] clears explicit scopes (dance falls back to the issuer's
              // scopes_supported), so always send the parsed list.
              scope: parseList(draft.scopeText),
              // "" fails the API's non-empty audience pattern, so a blanked
              // field is omitted, which keeps the stored value. Clearing an
              // audience isn't reachable through the API today.
              audience: audience.length > 0 ? audience : undefined,
            },
          },
        });
      } else {
        await createClient.mutateAsync({
          request: {
            createGlobalRemoteSessionClientForm: newClientFields(
              selectedIssuer.id,
            ),
          },
        });
      }

      await invalidateBoth();
      patchDraft({ secret: "" });
      toast.success("Provider saved");
    } catch (e) {
      toast.error(errMessage(e));
    }
  };

  const deleteSelected = async () => {
    if (!selectedIssuer || deleting) return;
    try {
      // Issuer delete 409s while a live client references it, so clear the
      // clients first.
      for (const c of clients) {
        await deleteClient.mutateAsync({ request: { id: c.id } });
      }
      await deleteIssuer.mutateAsync({ request: { id: selectedIssuer.id } });
      await invalidateBoth();
      setSelectedIssuerId(null);
      setCreating(false);
      setConfirmingDelete(false);
      toast.success("Provider deleted");
    } catch (e) {
      toast.error(errMessage(e));
    }
  };

  const extraClients = clients.slice(1);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content
        // High z so the modal sits above the floating dev toolbar (z-[99999]);
        // the portal attr stops the toolbar's outside-click handler collapsing
        // it while the operator interacts with the modal.
        data-rbac-dev-toolbar-portal="true"
        className="z-[100000] flex h-[80vh] min-w-5xl flex-col gap-0 overflow-hidden p-0"
      >
        <Dialog.Header className="border-b px-6 py-4 text-left">
          <Dialog.Title>Global Remote Session Providers</Dialog.Title>
          <Dialog.Description>
            Issuer + client pairs with no owning project or organization,
            available platform-wide.
          </Dialog.Description>
        </Dialog.Header>

        <div className="flex min-h-0 flex-1">
          <ProviderList
            query={query}
            onQueryChange={setQuery}
            creating={creating}
            onCreate={startCreate}
            isLoading={issuersQuery.isLoading}
            isError={issuersQuery.isError}
            totalCount={issuers.length}
            items={filteredIssuers}
            selectedIssuerId={selectedIssuerId}
            onSelect={selectIssuer}
          />

          {/* Right: detail / form (issuer on top, client below) */}
          <div className="flex min-w-0 flex-1 flex-col">
            {!showForm ? (
              <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
                Select a provider or create a new one.
              </div>
            ) : (
              <>
                <div className="flex-1 space-y-6 overflow-y-auto px-6 py-5">
                  <IssuerFields draft={draft} onPatch={patchDraft} />
                  <ClientFields
                    draft={draft}
                    onPatch={patchDraft}
                    needsClientId={needsClientId}
                  />
                  {!creating && extraClients.length > 0 && (
                    <ExtraClients clients={extraClients} />
                  )}
                  {!creating && selectedIssuer && (
                    <ProviderMeta
                      issuer={selectedIssuer}
                      primaryClient={primaryClient}
                    />
                  )}
                </div>

                <ProviderFooter
                  creating={creating}
                  hasSelection={selectedIssuer != null}
                  confirmingDelete={confirmingDelete}
                  onStartConfirm={() => setConfirmingDelete(true)}
                  onCancelConfirm={() => setConfirmingDelete(false)}
                  deleting={deleting}
                  onDelete={() => void deleteSelected()}
                  canSave={canSave}
                  saving={saving}
                  onSave={() => void save()}
                />
              </>
            )}
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

// Left pane: create button, search, and the selectable list of global providers
// (issuers). Purely presentational — selection and filtering live in the modal.
function ProviderList({
  query,
  onQueryChange,
  creating,
  onCreate,
  isLoading,
  isError,
  totalCount,
  items,
  selectedIssuerId,
  onSelect,
}: {
  query: string;
  onQueryChange: (v: string) => void;
  creating: boolean;
  onCreate: () => void;
  isLoading: boolean;
  isError: boolean;
  totalCount: number;
  items: RemoteSessionIssuer[];
  selectedIssuerId: string | null;
  onSelect: (i: RemoteSessionIssuer) => void;
}) {
  return (
    <div className="flex w-72 shrink-0 flex-col border-r">
      <div className="border-b p-2">
        <button
          type="button"
          onClick={onCreate}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm font-medium transition-colors ${
            creating
              ? "bg-foreground text-background"
              : "border-border hover:bg-muted/50 border"
          }`}
        >
          <Plus className="h-4 w-4" />
          New provider
        </button>
      </div>
      <div className="border-b p-2">
        <Input
          value={query}
          onChange={onQueryChange}
          placeholder="Search providers…"
        />
      </div>
      <div className="flex-1 overflow-y-auto p-2">
        {isLoading && (
          <p className="text-muted-foreground px-2 py-6 text-center text-xs">
            Loading…
          </p>
        )}
        {isError && (
          <p className="text-destructive px-2 py-6 text-center text-xs">
            Failed to load providers.
          </p>
        )}
        {!isLoading && !isError && totalCount === 0 && (
          <p className="text-muted-foreground px-2 py-6 text-center text-xs">
            No global providers.
          </p>
        )}
        {totalCount > 0 && items.length === 0 && (
          <p className="text-muted-foreground px-2 py-6 text-center text-xs">
            No matches.
          </p>
        )}
        {items.map((i) => {
          const active = !creating && i.id === selectedIssuerId;
          return (
            <button
              key={i.id}
              type="button"
              onClick={() => onSelect(i)}
              className={`mb-1 flex w-full flex-col items-start gap-0.5 rounded-md px-2 py-2 text-left transition-colors ${
                active ? "bg-muted" : "hover:bg-muted/50"
              }`}
            >
              <span className="text-foreground text-sm font-medium">
                {issuerLabel(i)}
              </span>
              <span className="text-muted-foreground truncate font-mono text-[11px]">
                {i.slug}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

// Issuer (RSI) portion of the provider form.
function IssuerFields({
  draft,
  onPatch,
}: {
  draft: Draft;
  onPatch: (patch: Partial<Draft>) => void;
}) {
  return (
    <div className="space-y-4">
      <SectionHeading>Issuer</SectionHeading>

      <Field label="Display name">
        <Input
          value={draft.issuerName}
          onChange={(v) => onPatch({ issuerName: v })}
          placeholder="e.g. HubSpot"
        />
      </Field>

      <Field label="Slug">
        <Input
          value={draft.slug}
          onChange={(v) => onPatch({ slug: v })}
          placeholder="e.g. hubspot"
        />
      </Field>

      <Field label="Issuer URL">
        <Input
          value={draft.issuer}
          onChange={(v) => onPatch({ issuer: v })}
          placeholder="https://issuer.example.com"
        />
      </Field>

      <Field label="Authorization endpoint">
        <Input
          value={draft.authorizationEndpoint}
          onChange={(v) => onPatch({ authorizationEndpoint: v })}
          placeholder="https://…/authorize"
        />
      </Field>

      <Field label="Token endpoint">
        <Input
          value={draft.tokenEndpoint}
          onChange={(v) => onPatch({ tokenEndpoint: v })}
          placeholder="https://…/token"
        />
      </Field>

      <Field label="Registration endpoint">
        <Input
          value={draft.registrationEndpoint}
          onChange={(v) => onPatch({ registrationEndpoint: v })}
          placeholder="optional (RFC 7591 DCR)"
        />
      </Field>

      <Field label="JWKS URI">
        <Input
          value={draft.jwksUri}
          onChange={(v) => onPatch({ jwksUri: v })}
          placeholder="https://…/jwks"
        />
      </Field>

      <Field label="Scopes supported">
        <Input
          value={draft.scopesSupportedText}
          onChange={(v) => onPatch({ scopesSupportedText: v })}
          placeholder="comma-separated"
        />
      </Field>

      <div className="flex items-center gap-6">
        <label className="flex items-center gap-2">
          <Checkbox
            checked={draft.oidc}
            onCheckedChange={(c) => onPatch({ oidc: c === true })}
          />
          <span className="text-sm">OIDC</span>
        </label>
        <label className="flex items-center gap-2">
          <Checkbox
            checked={draft.passthrough}
            onCheckedChange={(c) => onPatch({ passthrough: c === true })}
          />
          <span className="text-sm">Passthrough</span>
        </label>
      </div>
    </div>
  );
}

// Client (RSC) portion of the provider form. clientId is locked once a client
// exists; secret is write-only.
function ClientFields({
  draft,
  onPatch,
  needsClientId,
}: {
  draft: Draft;
  onPatch: (patch: Partial<Draft>) => void;
  needsClientId: boolean;
}) {
  return (
    <div className="space-y-4 border-t pt-6">
      <SectionHeading>Client</SectionHeading>

      <Field label="Client ID">
        <Input
          value={draft.clientId}
          onChange={(v) => onPatch({ clientId: v })}
          disabled={!needsClientId}
          placeholder="client_id from the issuer"
        />
        {!needsClientId && (
          <p className="text-muted-foreground text-[11px]">
            Client ID can't be changed after creation.
          </p>
        )}
      </Field>

      <Field label="Client secret">
        <Input
          type="password"
          value={draft.secret}
          onChange={(v) => onPatch({ secret: v })}
          placeholder={
            needsClientId ? "optional" : "leave blank to keep current"
          }
        />
        {!needsClientId && (
          <p className="text-muted-foreground text-[11px]">
            Write-only. Leave blank to keep the current secret; enter a value to
            rotate it.
          </p>
        )}
      </Field>

      <Field label="Token endpoint auth method">
        <Select
          value={draft.authMethod}
          onValueChange={(v) => onPatch({ authMethod: v as AuthMethod })}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {AUTH_METHODS.map((m) => (
              <SelectItem key={m} value={m}>
                {m}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field label="Scopes">
        <Input
          value={draft.scopeText}
          onChange={(v) => onPatch({ scopeText: v })}
          placeholder="comma-separated, e.g. crm.read, crm.write"
        />
      </Field>

      <Field label="Audience">
        <Input
          value={draft.audience}
          onChange={(v) => onPatch({ audience: v })}
          placeholder="optional"
        />
      </Field>
    </div>
  );
}

// Additional clients (read-only) — an issuer may carry more than one; the form
// edits the first, the rest are shown so data isn't hidden.
function ExtraClients({ clients }: { clients: RemoteSessionClient[] }) {
  return (
    <div className="space-y-2 border-t pt-6">
      <SectionHeading>Other clients ({clients.length})</SectionHeading>
      {clients.map((c) => (
        <div
          key={c.id}
          className="text-muted-foreground flex items-center justify-between gap-4 text-[11px]"
        >
          <span className="truncate font-mono">{c.clientId}</span>
          <span>{c.tokenEndpointAuthMethod ?? "—"}</span>
        </div>
      ))}
    </div>
  );
}

// Read-only identifiers/timestamps for the selected provider.
function ProviderMeta({
  issuer,
  primaryClient,
}: {
  issuer: RemoteSessionIssuer;
  primaryClient: RemoteSessionClient | null;
}) {
  return (
    <div className="text-muted-foreground space-y-1 border-t pt-4 text-[11px]">
      <div className="flex justify-between gap-4">
        <span>Issuer ID</span>
        <span className="font-mono">{issuer.id}</span>
      </div>
      {primaryClient && (
        <div className="flex justify-between gap-4">
          <span>Client ID issued</span>
          <span>{formatDate(primaryClient.clientIdIssuedAt)}</span>
        </div>
      )}
      <div className="flex justify-between gap-4">
        <span>Created</span>
        <span>{formatDate(issuer.createdAt)}</span>
      </div>
    </div>
  );
}

// Footer: delete (with inline confirm) on the left, save/create on the right.
function ProviderFooter({
  creating,
  hasSelection,
  confirmingDelete,
  onStartConfirm,
  onCancelConfirm,
  deleting,
  onDelete,
  canSave,
  saving,
  onSave,
}: {
  creating: boolean;
  hasSelection: boolean;
  confirmingDelete: boolean;
  onStartConfirm: () => void;
  onCancelConfirm: () => void;
  deleting: boolean;
  onDelete: () => void;
  canSave: boolean;
  saving: boolean;
  onSave: () => void;
}) {
  return (
    <Dialog.Footer className="items-center border-t px-6 py-4 sm:justify-between">
      <div>
        {!creating && hasSelection && (
          <>
            {confirmingDelete ? (
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground text-xs">
                  Delete this provider?
                </span>
                <Button
                  variant="tertiary"
                  onClick={onCancelConfirm}
                  disabled={deleting}
                >
                  <Button.Text>Cancel</Button.Text>
                </Button>
                <Button
                  variant="destructive-primary"
                  onClick={onDelete}
                  disabled={deleting}
                >
                  <Button.Text>{deleting ? "Deleting…" : "Delete"}</Button.Text>
                </Button>
              </div>
            ) : (
              <Button variant="destructive-secondary" onClick={onStartConfirm}>
                <Button.Text>Delete</Button.Text>
              </Button>
            )}
          </>
        )}
      </div>
      <Button variant="primary" disabled={!canSave || saving} onClick={onSave}>
        <Button.Text>
          {saving
            ? creating
              ? "Creating…"
              : "Saving…"
            : creating
              ? "Create"
              : "Save"}
        </Button.Text>
      </Button>
    </Dialog.Footer>
  );
}

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-muted-foreground text-[10px] font-semibold tracking-widest uppercase">
      {children}
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  );
}
