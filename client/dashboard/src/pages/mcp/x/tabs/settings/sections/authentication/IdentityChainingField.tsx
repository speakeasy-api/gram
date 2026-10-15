import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { type BadgeProps } from "@/components/ui/Badge";
import { SettingsSection } from "@/components/detail/settings-section";
import { CopyableValue } from "@/components/setup-steps/StepBlocks";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { cn } from "@/lib/utils";
import { useProtectedResourceMetadata } from "@/lib/remote-identity/queries/useProtectedResourceMetadata";
import { identityProvidersHref } from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes } from "@/routes";
import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation } from "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js";
import {
  useRemoteSessionClientsPrepareEMAMutation,
  type RemoteSessionClientsPrepareEMAMutationVariables,
} from "@gram/client/react-query/remoteSessionClientsPrepareEMA.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { buildRemoteSessionClientsReadEMAMutation } from "@gram/client/react-query/remoteSessionClientsReadEMA.js";
import { useRemoteSessionClientsUnlinkEMAMutation } from "@gram/client/react-query/remoteSessionClientsUnlinkEMA.js";

import { useSpeakeasyAppendCheck } from "./useSpeakeasyAppendCheck";
import {
  trustsExternalIdp,
  type ChainingProviders,
} from "./identityChainingGate";
import {
  chainingBound,
  chainingGrantDeclaration,
  chainingResourceMetadata,
  chainingScopesAllowed,
  chainingUnlinkable,
  defaultChainingScopes,
  displayedChainingState,
  isCimdClient,
  isPublicClient,
  preparationSucceeded,
  sanitizeChainingScopes,
} from "./identityChainingScopes";

type ChainingState = IdentityChainingPreparation["state"];
type BadgeVariant = NonNullable<BadgeProps["variant"]>;

function stateBadge(state: ChainingState): {
  label: string;
  variant: BadgeVariant;
} {
  switch (state) {
    case "ready":
      return { label: "Enabled", variant: "success" };
    case "unlinked":
      return { label: "Not enabled", variant: "neutral" };
    case "in_progress":
      return { label: "In progress", variant: "information" };
    case "published_acceptance_unverified":
      return { label: "Published, unverified", variant: "information" };
    case "configuration_required":
      return { label: "Configuration required", variant: "warning" };
    case "manual_setup_required":
      return { label: "Manual setup required", variant: "warning" };
    case "unknown_grants":
      return { label: "Grants unknown", variant: "warning" };
    case "incomplete_metadata":
      return { label: "Incomplete metadata", variant: "warning" };
    case "indeterminate":
      return { label: "Indeterminate", variant: "warning" };
    case "transient_failure":
      return { label: "Temporary failure", variant: "warning" };
    case "unsupported_profile":
      return { label: "Not supported", variant: "destructive" };
    case "provider_rejection":
      return { label: "Provider rejected", variant: "destructive" };
  }
}

/** A server refusal as a sentence. */
function sentence(message: string): string {
  const text = message.trim();
  if (!text) return text;
  const capitalized = text[0]!.toUpperCase() + text.slice(1);
  return /[.!?]$/.test(capitalized) ? capitalized : `${capitalized}.`;
}

function ClientPicker({
  clients,
  value,
  onChange,
  disabled,
}: {
  clients: RemoteSessionClient[];
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {clients.map((client) => (
          <SelectItem key={client.id} value={client.id}>
            {client.clientId}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Rendered only for an organization issuer that an identity provider or Speakeasy can vouch for. */
export function IdentityChainingField({
  userSessionIssuer,
  providers,
  speakeasyIssuerUrl,
  speakeasyEnabled = false,
  linkedClients,
  resource,
  remoteMcpServerId,
}: {
  userSessionIssuer: UserSessionIssuer;
  providers: ChainingProviders;
  /** Speakeasy's issuer URL, which vendors trust for Speakeasy-signed grants. */
  speakeasyIssuerUrl?: string;
  /** Speakeasy's issuer is turned on and can sign. */
  speakeasyEnabled?: boolean;
  linkedClients: RemoteSessionClient[];
  /** The remote MCP server URL; the executor matches it exactly. */
  resource: string;
  /** Reuses the server's protected resource metadata probe for advertised scopes. */
  remoteMcpServerId?: string;
}): JSX.Element {
  const core = useGramContext();
  const gramProject = useProjectSlugForRequests();
  // The identity provider's own sign-in client is never a chaining target.
  const trustedClientId = userSessionIssuer.trustedRemoteSessionClientId;
  const candidates = linkedClients.filter(
    (client) => !trustedClientId || client.id !== trustedClientId,
  );
  const [selectedId, setSelectedIdState] = useState<string | null>(null);
  // A prepare that returned a blocker; kept until the next action.
  const [prepareOutcome, setPrepareOutcome] =
    useState<IdentityChainingPreparation | null>(null);
  const [grantsConfirmed, setGrantsConfirmed] = useState(false);
  const setSelectedId = (id: string) => {
    setSelectedIdState(id);
    setPrepareOutcome(null);
    setGrantsConfirmed(false);
  };
  const picked =
    candidates.find((client) => client.id === selectedId) ?? candidates[0];

  const target = picked && {
    userSessionIssuerId: userSessionIssuer.id,
    remoteSessionIssuerId: picked.remoteSessionIssuerId,
    resource,
  };

  const queryClient = useQueryClient();
  const readKey = ["identityChaining", "readEMA", gramProject, target];
  const read = useQuery({
    queryKey: readKey,
    enabled: target != null,
    retry: false,
    queryFn: ({ signal }) => {
      if (!target) throw new Error("No client selected");
      return buildRemoteSessionClientsReadEMAMutation(core).mutationFn({
        request: { gramProject, readEMARequestBody: target },
        options: { fetchOptions: { signal } },
      });
    },
  });

  const prm = useProtectedResourceMetadata(
    remoteMcpServerId,
    remoteMcpServerId !== undefined,
  );
  // The binding is keyed by issuer and resource, so it may name another client.
  const boundClient = chainingBound(read.data)
    ? candidates.find(
        (client) =>
          client.id === read.data?.clientId &&
          client.remoteSessionIssuerId === picked?.remoteSessionIssuerId,
      )
    : undefined;
  const selected = selectedId === null ? (boundClient ?? picked) : picked;
  const switchingClient =
    chainingBound(read.data) && read.data?.clientId !== selected?.id;
  const bound = chainingBound(read.data) && !switchingClient;
  const storedScopes = bound && read.data ? read.data.scopes : [];
  const activeScopes = bound
    ? sanitizeChainingScopes(storedScopes)
    : defaultChainingScopes(selected?.scope);
  // The connection's scopes are edited in Identity; chaining has no list of its own.
  const scopes = activeScopes;
  const scopesValid =
    selected != null && chainingScopesAllowed(scopes, selected.scope);
  const [confirmDisable, setConfirmDisable] = useState(false);
  const trustedIssuerQuery = useRemoteSessionIssuer(
    { id: userSessionIssuer.trustedRemoteSessionIssuerId ?? "" },
    undefined,
    {
      enabled:
        providers.external && !!userSessionIssuer.trustedRemoteSessionIssuerId,
      throwOnError: false,
      retry: false,
    },
  );

  const vendorIssuer = useRemoteSessionIssuer(
    { id: picked?.remoteSessionIssuerId ?? "" },
    undefined,
    { enabled: !!picked, throwOnError: false, retry: false },
  );

  const { hasScope } = useRBAC();
  const organization = useOrganization();
  const project = useProject();
  const cimd = !!selected && isCimdClient(selected);
  const publicClient = !!selected && isPublicClient(selected);
  const grantDeclaration = selected
    ? chainingGrantDeclaration(selected.grantTypes, cimd)
    : null;
  const needsGrantConfirm = !!grantDeclaration?.rewrites;
  // An organization-owned client's registration is shared across projects.
  const grantsBlocked =
    needsGrantConfirm &&
    selected?.projectId === "" &&
    !hasScope("org:admin", organization.id);

  const onSettled = () => void read.refetch();

  // A sign-in that trusts no external identity provider can only be vouched
  // for by Speakeasy, so enabling also puts Speakeasy in the vendor's order.
  const speakeasyOnly =
    providers.speakeasy && !trustsExternalIdp(userSessionIssuer);
  const session = useSession();
  const orgRoutes = useOrgRoutes();
  const order = read.data?.emaProviders;
  const speakeasyInOrder =
    !!order &&
    !order.isDefault &&
    order.providers.some((provider) => provider.kind === "speakeasy");
  const needsSpeakeasy = speakeasyOnly && !!order && !speakeasyInOrder;
  const orgAdmin = hasScope("org:admin", organization.id);
  const supportSession =
    session.organizationOverride || !!session.impersonatorEmail;
  const canAddSpeakeasy =
    needsSpeakeasy && speakeasyEnabled && orgAdmin && !supportSession;
  const appendTarget = picked && {
    remoteSessionIssuerId: picked.remoteSessionIssuerId,
    userSessionIssuerId: userSessionIssuer.id,
  };
  const appendCheck = useSpeakeasyAppendCheck(
    appendTarget ?? { remoteSessionIssuerId: "", userSessionIssuerId: "" },
    canAddSpeakeasy && !!appendTarget && !read.isFetching,
  );
  const addSpeakeasy =
    useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation({
      onError: () => undefined,
    });
  const identitySettingsHref = `${orgRoutes.identity.href()}${identityProvidersHref()}`;
  let speakeasyBlock: {
    reason: string;
    link?: { href: string; label: string };
  } | null = null;
  if (speakeasyOnly && !speakeasyEnabled)
    speakeasyBlock = {
      reason: "Turn on Speakeasy’s issuer first.",
      link: {
        href: identitySettingsHref,
        label: "Speakeasy identity provider",
      },
    };
  else if (needsSpeakeasy && !orgAdmin)
    speakeasyBlock = {
      reason:
        "An organization admin needs to add Speakeasy for this vendor first.",
    };
  else if (needsSpeakeasy && supportSession)
    speakeasyBlock = {
      reason:
        "The provider order can’t be changed from a support or impersonated session.",
    };
  else if (needsSpeakeasy && appendCheck.isError)
    speakeasyBlock = { reason: sentence(appendCheck.error.message) };
  const speakeasyChecking =
    canAddSpeakeasy && (appendCheck.isFetching || !appendCheck.isSuccess);
  const prepare = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      if (!preparationSucceeded(result)) {
        setPrepareOutcome(result);
        toast.warning(
          `Identity chaining isn’t on: ${stateBadge(result.state).label}`,
        );
        return;
      }
      queryClient.setQueryData(readKey, result);
      // Declared grants are written onto the client row.
      void invalidateAllRemoteSessionClients(queryClient);
      setGrantsConfirmed(false);
      toast.success("Identity chaining is on");
    },
    onError: () => undefined,
    onSettled,
  });
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      toast.success("Identity chaining is off");
    },
    onError: () => undefined,
    onSettled,
  });

  const enable = () => {
    if (
      !selected ||
      !target ||
      !read.data ||
      !scopesValid ||
      !grantDeclaration ||
      publicClient
    )
      return;
    if (grantsBlocked || (needsGrantConfirm && !grantsConfirmed)) return;
    setPrepareOutcome(null);
    const prepareRequest: RemoteSessionClientsPrepareEMAMutationVariables = {
      request: {
        gramProject,
        prepareEMARequestBody: {
          ...target,
          clientId: selected.id,
          scopes,
          mechanism: cimd ? "cimd" : "manual",
          confirmGrants: grantDeclaration.grants,
          expectedGeneration: read.data.generation,
          resourceMetadata: chainingResourceMetadata(prm.metadata),
        },
      },
    };
    if (!needsSpeakeasy) {
      prepare.mutate(prepareRequest);
      return;
    }
    // Add first: a failed prepare leaves Speakeasy in the order, which is
    // harmless and makes a retry's add a no-op.
    if (!appendTarget || speakeasyBlock) return;
    addSpeakeasy.mutate(
      {
        request: {
          addSpeakeasyEMAProviderRequestBody: {
            ...appendTarget,
            dryRun: false,
          },
        },
      },
      { onSuccess: () => prepare.mutate(prepareRequest) },
    );
  };
  const appendSpeakeasy = () => {
    if (!appendTarget || speakeasyBlock) return;
    addSpeakeasy.mutate(
      {
        request: {
          addSpeakeasyEMAProviderRequestBody: {
            ...appendTarget,
            dryRun: false,
          },
        },
      },
      {
        onSuccess: () => {
          toast.success("Speakeasy added to the provider order");
        },
        onSettled,
      },
    );
  };
  const disable = () => {
    if (!target || !read.data) return;
    setPrepareOutcome(null);
    unlink.mutate(
      {
        request: {
          gramProject,
          unlinkEMARequestBody: {
            ...target,
            expectedGeneration: read.data.generation,
          },
        },
      },
      { onSettled: () => setConfirmDisable(false) },
    );
  };

  const pending =
    read.isFetching ||
    prepare.isPending ||
    unlink.isPending ||
    addSpeakeasy.isPending;
  const ready = bound && read.data?.state === "ready";
  const shownState = read.data ? displayedChainingState(read.data) : null;
  const canUnlink = chainingUnlinkable(read.data);
  const enableBlocked =
    !!speakeasyBlock ||
    (needsSpeakeasy && speakeasyChecking) ||
    publicClient ||
    !scopesValid ||
    grantsBlocked ||
    (needsGrantConfirm && !grantsConfirmed);

  const vendor = vendorName(vendorIssuer.data?.name, resource);
  // Providers this server can use, in the order identity chaining tries them.
  const usable = (order?.providers ?? []).filter(
    (provider) =>
      !provider.unavailableReason &&
      (provider.kind === "speakeasy" || !!provider.remoteSessionIssuerId),
  );
  const issuerNames = useRemoteSessionIssuersByIds(
    usable.flatMap((provider) =>
      provider.kind === "external" && provider.remoteSessionIssuerId
        ? [provider.remoteSessionIssuerId]
        : [],
    ),
  );
  function providerSignIn(provider: EMAProvider): string {
    if (provider.kind === "speakeasy") return "Speakeasy sign-in";
    const name = issuerNames.items.find(
      (issuer) => issuer.id === provider.remoteSessionIssuerId,
    )?.name;
    return name ? `${name} sign-in` : "identity provider sign-in";
  }
  const signIn =
    usable.length > 0
      ? usable.map(providerSignIn).join(", or their ") +
        (usable.length > 1 ? " if that fails" : "")
      : speakeasyOnly
        ? "Speakeasy sign-in"
        : "identity provider sign-in";
  const status = chainingStatus({
    loading: read.isPending,
    ready,
    // A refused Turn on reports why, even while the binding still reads off.
    off:
      !prepareOutcome &&
      (!read.data || shownState === "unlinked" || switchingClient),
    stalled: ready && (needsSpeakeasy || (!!order && usable.length === 0)),
    state: prepareOutcome?.state ?? shownState,
    remediation: prepareOutcome?.remediation ?? read.data?.remediation,
    vendor,
    signIn,
    stalledDetail: needsSpeakeasy
      ? "Speakeasy isn’t in this vendor’s provider order, so identity chaining can’t run."
      : "None of the sign-ins this vendor accepts can be used by this server right now.",
  });
  const blockers = [
    !ready && speakeasyBlock ? speakeasyBlock : null,
    ready && needsSpeakeasy && speakeasyBlock ? speakeasyBlock : null,
    publicClient && selected
      ? {
          reason: `Client ${selected.clientId} is a public client. Identity chaining needs a confidential client that authenticates to the provider’s token endpoint.`,
        }
      : null,
    !publicClient && grantsBlocked
      ? {
          reason:
            "This client belongs to the organization. An organization admin must confirm its grants first.",
        }
      : null,
  ].filter((b): b is NonNullable<typeof b> => b !== null);
  const setupIssuer = speakeasyOnly
    ? speakeasyIssuerUrl
    : trustedIssuerQuery.data?.issuer;

  if (candidates.length === 0) {
    return (
      <SettingsSection.Body>
        <Text muted small>
          {providers.external
            ? "Link a client for this server’s identity provider in Identity above first."
            : "Link a client registered with this server’s authorization server in Identity above first."}
        </Text>
      </SettingsSection.Body>
    );
  }

  return (
    <>
      <SettingsSection.Body>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0 flex-1 space-y-1">
            <span className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={cn("size-2 shrink-0 rounded-full", status.dot)}
              />
              <Text className="font-medium">{status.title}</Text>
            </span>
            {status.detail && (
              <Text muted small className="block">
                {status.detail}
              </Text>
            )}
          </div>
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
          >
            <div className="flex items-center gap-2">
              {ready && needsSpeakeasy && canAddSpeakeasy && (
                <Button
                  size="sm"
                  disabled={pending || speakeasyChecking || !!speakeasyBlock}
                  onClick={appendSpeakeasy}
                >
                  Add Speakeasy
                </Button>
              )}
              {!ready && (
                <Button
                  size="sm"
                  disabled={pending || !read.data || enableBlocked}
                  onClick={enable}
                >
                  {switchingClient ? "Use this client instead" : "Turn on"}
                </Button>
              )}
              {canUnlink && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={pending}
                  onClick={() => setConfirmDisable(true)}
                >
                  Turn off
                </Button>
              )}
            </div>
          </RequireScope>
        </div>
        {blockers.map((blocker) => (
          <Text key={blocker.reason} muted small className="block">
            {blocker.reason}
            {blocker.link && (
              <>
                {" "}
                <Link
                  className="underline underline-offset-4"
                  to={blocker.link.href}
                >
                  {blocker.link.label}
                </Link>
              </>
            )}
          </Text>
        ))}
        {switchingClient && (
          <Text muted small className="block">
            Identity chaining uses client{" "}
            {boundClient?.clientId ?? read.data?.externalClientId}. Using this
            client instead replaces it.
          </Text>
        )}
        {!ready &&
          !publicClient &&
          needsGrantConfirm &&
          !grantsBlocked &&
          grantDeclaration &&
          selected && (
            <RequireScope
              scope="project:write"
              resourceId={project.id}
              level="component"
            >
              <label className="flex items-start gap-2">
                <Checkbox
                  className="mt-0.5"
                  checked={grantsConfirmed}
                  disabled={pending}
                  onCheckedChange={(next) => setGrantsConfirmed(next === true)}
                />
                <Text small>
                  {cimd
                    ? "Publish these grants in the client's metadata document: "
                    : `Declare that client ${selected.clientId} is registered with the provider for these grants, and record them on it: `}
                  {grantDeclaration.grants.join(", ")}
                </Text>
              </label>
            </RequireScope>
          )}
        <ApiErrorAlert
          error={
            read.error ?? addSpeakeasy.error ?? prepare.error ?? unlink.error
          }
        />
      </SettingsSection.Body>

      {!ready && !read.isPending && (
        <div className="border-border space-y-2 border-t px-6 py-5">
          <Text small className="block font-medium">
            Set up {vendor} first
          </Text>
          <Text muted small className="block">
            {speakeasyOnly
              ? `In ${vendor}’s admin settings, turn on enterprise-managed authentication and trust Speakeasy’s issuer. Speakeasy can’t do this step for you.`
              : `In ${vendor}’s admin settings, turn on enterprise-managed authentication and trust your identity provider’s issuer. Speakeasy can’t do this step for you.`}
          </Text>
          {setupIssuer && (
            <div className="max-w-2xl">
              <CopyableValue label="issuer URL" value={setupIssuer} />
            </div>
          )}
        </div>
      )}

      {candidates.length > 1 && (
        <Collapsible className="border-border border-t">
          <CollapsibleTrigger className="group hover:bg-muted/40 flex w-full items-center gap-3 px-6 py-4 text-left">
            <ChevronDown
              aria-hidden="true"
              className="size-4 transition-transform group-data-[state=open]:rotate-180"
            />
            <Text small className="font-medium">
              Client
            </Text>
            <Text muted small className="truncate">
              {selected?.clientId}
            </Text>
          </CollapsibleTrigger>
          <CollapsibleContent className="px-6 pt-1 pb-6">
            <div className="max-w-md">
              <ClientPicker
                clients={candidates}
                value={selected?.id ?? ""}
                onChange={setSelectedId}
                disabled={prepare.isPending || unlink.isPending}
              />
            </div>
          </CollapsibleContent>
        </Collapsible>
      )}

      <Dialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Turn off identity chaining?</Dialog.Title>
            <Dialog.Description>
              People will connect {vendor} themselves the next time they use
              this server.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={unlink.isPending}
              onClick={() => setConfirmDisable(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={unlink.isPending}
              onClick={disable}
            >
              {unlink.isPending ? "Turning off…" : "Turn off"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

const DOT_FOR: Record<NonNullable<BadgeProps["variant"]>, string> = {
  success: "bg-[var(--fill-success-default)]",
  warning: "bg-[var(--fill-warning-default)]",
  destructive: "bg-[var(--fill-destructive-default)]",
  information: "bg-[var(--fill-information-default)]",
  neutral: "bg-muted-foreground/40",
};

function chainingStatus({
  loading,
  ready,
  off,
  stalled,
  state,
  remediation,
  vendor,
  signIn,
  stalledDetail,
}: {
  loading: boolean;
  ready: boolean;
  off: boolean;
  stalled: boolean;
  state: ChainingState | null;
  remediation: string | undefined;
  vendor: string;
  signIn: string;
  stalledDetail: string;
}): { title: string; detail?: string; dot: string } {
  if (loading) return { title: "Checking…", dot: DOT_FOR.neutral };
  if (stalled)
    return {
      title: "On, but not running",
      detail: stalledDetail,
      dot: DOT_FOR.warning,
    };
  if (ready)
    return {
      title: "On",
      detail: `People reach ${vendor} with their ${signIn}.`,
      dot: DOT_FOR.success,
    };
  if (off || !state)
    return {
      title: "Off",
      detail: `People connect ${vendor} themselves the first time they use this server.`,
      dot: DOT_FOR.neutral,
    };
  const badge = stateBadge(state);
  return {
    title: badge.label,
    detail: remediation?.trim() || undefined,
    dot: DOT_FOR[badge.variant] ?? DOT_FOR.neutral,
  };
}

/** The vendor's display name, else the host its MCP server runs on. */
function vendorName(name: string | undefined, resource: string): string {
  if (name?.trim()) return name.trim();
  try {
    return new URL(resource).host;
  } catch {
    return "the vendor";
  }
}
