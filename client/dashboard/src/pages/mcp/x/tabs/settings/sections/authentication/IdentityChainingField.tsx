import { useState, type ReactNode } from "react";
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
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useProtectedResourceMetadata } from "@/lib/remote-identity/queries/useProtectedResourceMetadata";
import {
  acceptanceLabel,
  providerView,
  serviceName,
  trustInstruction,
  type ProviderView,
} from "@/pages/org/identity-provider/services/emaServiceView";
import {
  emaServiceHref,
  identityProvidersHref,
  oktaViewHref,
  speakeasyProviderHref,
} from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes } from "@/routes";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  useRemoteSessionClientsPrepareEMAMutation,
  type RemoteSessionClientsPrepareEMAMutationVariables,
} from "@gram/client/react-query/remoteSessionClientsPrepareEMA.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { buildRemoteSessionClientsReadEMAMutation } from "@gram/client/react-query/remoteSessionClientsReadEMA.js";
import { useRemoteSessionClientsUnlinkEMAMutation } from "@gram/client/react-query/remoteSessionClientsUnlinkEMA.js";

import { MCP_SESSIONS_SECTION_ID } from "../RemoteMcpSessionsSection";
import { type ChainingProviders } from "./identityChainingGate";
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
type Tone = NonNullable<BadgeProps["variant"]>;

function stateBadge(state: ChainingState): { label: string; variant: Tone } {
  switch (state) {
    case "ready":
      return { label: "On", variant: "success" };
    case "unlinked":
      return { label: "Off", variant: "neutral" };
    case "in_progress":
      return { label: "Setting up", variant: "information" };
    case "published_acceptance_unverified":
      return { label: "Published, not verified", variant: "information" };
    case "configuration_required":
      return { label: "Needs configuration", variant: "warning" };
    case "manual_setup_required":
      return { label: "Needs manual setup", variant: "warning" };
    case "unknown_grants":
      return { label: "Grants unknown", variant: "warning" };
    case "incomplete_metadata":
      return { label: "Incomplete metadata", variant: "warning" };
    case "indeterminate":
      return { label: "Couldn’t tell", variant: "warning" };
    case "transient_failure":
      return { label: "Temporary failure", variant: "warning" };
    case "unsupported_profile":
      return { label: "Not supported", variant: "destructive" };
    case "provider_rejection":
      return { label: "Provider rejected", variant: "destructive" };
  }
}

const DOT_FOR: Record<Tone, string> = {
  success: "bg-[var(--fill-success-default)]",
  warning: "bg-[var(--fill-warning-default)]",
  destructive: "bg-[var(--fill-destructive-default)]",
  information: "bg-[var(--fill-information-default)]",
  neutral: "bg-muted-foreground/40",
};

function StatusDot({ tone }: { tone: Tone }): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn("size-2 shrink-0 rounded-full", DOT_FOR[tone])}
    />
  );
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
      <SelectTrigger className="max-w-md">
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

/**
 * One link in the chain that gets a person from their sign-in to the
 * service: who it is, whether it holds, and where to fix it.
 */
function ChainLink({
  label,
  tone,
  children,
}: {
  label: string;
  tone: Tone;
  children: ReactNode;
}): JSX.Element {
  return (
    <li className="grid items-start gap-x-6 gap-y-2 px-6 py-4 sm:grid-cols-[170px_minmax(0,1fr)]">
      <div className="flex items-center gap-2 sm:pt-0.5">
        <StatusDot tone={tone} />
        <span className="text-eyebrow">{label}</span>
      </div>
      <div className="min-w-0 space-y-2">{children}</div>
    </li>
  );
}

function SettingsLink({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Link
      className="text-muted-foreground text-sm underline underline-offset-4"
      to={to}
    >
      {children}
    </Link>
  );
}

/** Rendered only for an organization issuer that an identity provider or Speakeasy can vouch for. */
export function IdentityChainingField({
  userSessionIssuer,
  providers,
  speakeasyIssuerUrl,
  linkedClients,
  resource,
  remoteMcpServerId,
}: {
  userSessionIssuer: UserSessionIssuer;
  providers: ChainingProviders;
  /** Speakeasy's issuer URL, which vendors trust for Speakeasy-signed grants. */
  speakeasyIssuerUrl?: string;
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
  // The connection's scopes are edited in Identity; chaining has no list of its own.
  const scopes = bound
    ? sanitizeChainingScopes(storedScopes)
    : defaultChainingScopes(selected?.scope);
  const scopesValid =
    selected != null && chainingScopesAllowed(scopes, selected.scope);
  const [confirmDisable, setConfirmDisable] = useState(false);

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

  const orgRoutes = useOrgRoutes();
  const identityHref = (search: string) =>
    `${orgRoutes.identity.href()}${search}`;
  const prepare = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      if (!preparationSucceeded(result)) {
        setPrepareOutcome(result);
        toast.warning(
          `Enterprise Managed Authorization isn’t on: ${stateBadge(result.state).label}`,
        );
        return;
      }
      queryClient.setQueryData(readKey, result);
      // Declared grants are written onto the client row.
      void invalidateAllRemoteSessionClients(queryClient);
      setGrantsConfirmed(false);
      toast.success("Enterprise Managed Authorization is on");
    },
    onError: () => undefined,
    onSettled,
  });
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      toast.success("Enterprise Managed Authorization is off");
    },
    onError: () => undefined,
    onSettled,
  });

  // Who can vouch for people on this server follows from its sign-in and the
  // organization's Speakeasy issuer; nothing is chosen here.
  const order = read.data?.emaProviders;
  const listed = (order?.providers ?? []).filter(
    (provider) =>
      provider.kind === "speakeasy" || !!provider.remoteSessionIssuerId,
  );
  const usable = listed.filter((provider) => !provider.unavailableReason);
  const trustedIssuerId = userSessionIssuer.trustedRemoteSessionIssuerId;
  const issuers = useRemoteSessionIssuersByIds([
    ...(trustedIssuerId ? [trustedIssuerId] : []),
    ...listed.flatMap((provider) =>
      provider.kind === "external" &&
      provider.remoteSessionIssuerId &&
      provider.remoteSessionIssuerId !== trustedIssuerId
        ? [provider.remoteSessionIssuerId]
        : [],
    ),
  ]);
  const issuerById = (id: string | undefined) =>
    issuers.items.find((issuer) => issuer.id === id);
  const vendor = serviceName(vendorIssuer.data?.name, resource);
  const signIn = {
    name: trustedIssuerId
      ? issuerName(issuerById(trustedIssuerId))
      : "Speakeasy’s own sign-in",
    slug: userSessionIssuer.slug,
  };
  // An identity provider this sign-in doesn't use is just noise here; the
  // sign-in line already says why. Speakeasy stays listed: it can be set up.
  const vouchers = listed
    .map((provider) =>
      providerView(provider, {
        issuer: issuerById(provider.remoteSessionIssuerId),
        speakeasyIssuerUrl,
      }),
    )
    .filter((row) => !row.unusedBySignIn);
  const usableVouchers = vouchers.filter((row) => row.usable);
  const trusts = usableVouchers.filter(
    (row): row is ProviderView & { issuerUrl: string } => !!row.issuerUrl,
  );
  const noneUsable = !!order && usable.length === 0;
  const lastAccepted = usable
    .map((provider) => provider.lastAcceptedAt)
    .filter((at): at is Date => at != null)
    .sort((a, b) => b.getTime() - a.getTime())[0];

  const enable = () => {
    if (
      !selected ||
      !target ||
      !read.data ||
      !scopesValid ||
      !grantDeclaration ||
      publicClient ||
      noneUsable
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
    prepare.mutate(prepareRequest);
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

  const pending = read.isFetching || prepare.isPending || unlink.isPending;
  const ready = bound && read.data?.state === "ready";
  const shownState = read.data ? displayedChainingState(read.data) : null;
  const canUnlink = chainingUnlinkable(read.data);
  const enableBlocked =
    noneUsable ||
    publicClient ||
    !scopesValid ||
    grantsBlocked ||
    (needsGrantConfirm && !grantsConfirmed);

  const status = chainingStatus({
    loading: read.isPending,
    ready,
    // A refused Turn on reports why, even while the binding still reads off.
    off:
      !prepareOutcome &&
      (!read.data || shownState === "unlinked" || switchingClient),
    stalled: ready && noneUsable,
    lastAccepted,
    state: prepareOutcome?.state ?? shownState,
    remediation: prepareOutcome?.remediation ?? read.data?.remediation,
    vendor,
  });

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

  const clientTone: Tone =
    publicClient || grantsBlocked
      ? "warning"
      : needsGrantConfirm && !ready
        ? "information"
        : "success";

  return (
    <>
      <SettingsSection.Body>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0 flex-1 space-y-1">
            <span className="flex items-center gap-2">
              <StatusDot tone={status.tone} />
              <Text className="font-medium">{status.title}</Text>
              {status.aside && (
                <Text muted small className="ml-auto">
                  {status.aside}
                </Text>
              )}
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
        <ApiErrorAlert error={read.error ?? prepare.error ?? unlink.error} />
      </SettingsSection.Body>

      <ol
        aria-label="How people get in"
        className="border-border divide-border divide-y border-t"
      >
        <ChainLink label="Sign-in" tone="success">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <Text small>
              {signIn.name}
              <Text muted small as="span">
                {" "}
                ({signIn.slug})
              </Text>
            </Text>
            <SettingsLink to={`#${MCP_SESSIONS_SECTION_ID}`}>
              Change in Sessions
            </SettingsLink>
          </div>
        </ChainLink>

        <ChainLink
          label="Vouched for by"
          tone={usableVouchers.length > 0 ? "success" : "warning"}
        >
          {!order ? (
            <Text muted small>
              Checking…
            </Text>
          ) : vouchers.length === 0 ? (
            <Text small>
              Nobody yet.{" "}
              <Link
                className="underline underline-offset-4"
                to={identityHref(identityProvidersHref())}
              >
                Set up Speakeasy as an identity provider
              </Link>
              , or have people sign in through an identity provider you’ve
              connected.
            </Text>
          ) : (
            <ul aria-label="Identity providers" className="space-y-1">
              {vouchers.map((row) => {
                const position = usableVouchers.indexOf(row);
                return (
                  <li
                    key={row.key}
                    className="flex flex-wrap items-baseline gap-x-2"
                  >
                    <Text
                      small
                      className={cn(
                        "font-medium",
                        !row.usable && "text-muted-foreground",
                      )}
                    >
                      {row.name}
                    </Text>
                    <Text muted small>
                      {row.usable
                        ? row.kind === "speakeasy"
                          ? "for everyone who signs in to this server"
                          : `for people who signed in with ${row.name}`
                        : row.unavailable}
                      {row.usable &&
                        usableVouchers.length > 1 &&
                        (position === 0 ? " · tried first" : " · then")}
                    </Text>
                    {row.fix && (
                      <SettingsLink
                        to={identityHref(
                          row.fix === "speakeasy"
                            ? speakeasyProviderHref()
                            : oktaViewHref("setup"),
                        )}
                      >
                        {row.fix === "speakeasy"
                          ? "Speakeasy identity provider"
                          : "Okta"}
                      </SettingsLink>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          {!ready && noneUsable && (
            <Text muted small className="block">
              No identity provider can vouch for people on this server yet.
            </Text>
          )}
        </ChainLink>

        <ChainLink
          label={`${vendor} trusts`}
          tone={
            trusts.length === 0
              ? "neutral"
              : trusts.some((row) => row.lastAcceptedAt)
                ? "success"
                : ready
                  ? "warning"
                  : "neutral"
          }
        >
          {!order ? (
            <Text muted small>
              Checking…
            </Text>
          ) : trusts.length === 0 ? (
            <Text muted small>
              Nothing to set up at {vendor} until an identity provider can
              vouch.
            </Text>
          ) : (
            <>
              {trusts.some((row) => !row.lastAcceptedAt) && (
                <Text muted small className="block">
                  {trustInstruction(vendor)}
                </Text>
              )}
              <ul aria-label={`${vendor} trusts`} className="space-y-3">
                {trusts.map((row) => (
                  <li key={row.key} className="space-y-2">
                    <div className="flex flex-wrap items-baseline gap-x-2">
                      <Text small className="font-medium">
                        {row.name}
                      </Text>
                      <Text muted small>
                        {acceptanceLabel(row.lastAcceptedAt)}
                      </Text>
                    </div>
                    {row.lastAcceptedAt ? (
                      <Collapsible>
                        <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
                          <ChevronDown
                            aria-hidden="true"
                            className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
                          />
                          Issuer URL
                        </CollapsibleTrigger>
                        <CollapsibleContent className="pt-2">
                          <div className="max-w-2xl">
                            <CopyableValue
                              label={`${row.name}’s issuer URL`}
                              value={row.issuerUrl}
                            />
                          </div>
                        </CollapsibleContent>
                      </Collapsible>
                    ) : (
                      <div className="max-w-2xl">
                        <CopyableValue
                          label={`${row.name}’s issuer URL`}
                          value={row.issuerUrl}
                        />
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}
          {order && (
            <SettingsLink to={identityHref(emaServiceHref(order.vendorIssuer))}>
              Every server that reaches {vendor}
            </SettingsLink>
          )}
        </ChainLink>

        <ChainLink label="Client" tone={clientTone}>
          {candidates.length > 1 ? (
            <ClientPicker
              clients={candidates}
              value={selected?.id ?? ""}
              onChange={setSelectedId}
              disabled={prepare.isPending || unlink.isPending}
            />
          ) : (
            <Text small className="truncate">
              {selected?.clientId}
            </Text>
          )}
          {switchingClient && (
            <Text muted small className="block">
              Enterprise Managed Authorization uses client{" "}
              {boundClient?.clientId ?? read.data?.externalClientId}. Using this
              client instead replaces it.
            </Text>
          )}
          {publicClient && selected && (
            <Text muted small className="block">
              Client {selected.clientId} is a public client. Enterprise Managed
              Authorization needs a confidential client that authenticates to
              the provider’s token endpoint.
            </Text>
          )}
          {!publicClient && grantsBlocked && (
            <Text muted small className="block">
              This client belongs to the organization. An organization admin
              must confirm its grants first.
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
                    onCheckedChange={(next) =>
                      setGrantsConfirmed(next === true)
                    }
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
        </ChainLink>
      </ol>

      <Dialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>
              Turn off Enterprise Managed Authorization?
            </Dialog.Title>
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

function issuerName(issuer: RemoteSessionIssuer | undefined): string {
  return issuer?.name?.trim() || "Identity provider";
}

function chainingStatus({
  loading,
  ready,
  off,
  stalled,
  lastAccepted,
  state,
  remediation,
  vendor,
}: {
  loading: boolean;
  ready: boolean;
  off: boolean;
  stalled: boolean;
  lastAccepted: Date | undefined;
  state: ChainingState | null;
  remediation: string | undefined;
  vendor: string;
}): { title: string; detail?: string; aside?: string; tone: Tone } {
  if (loading) return { title: "Checking…", tone: "neutral" };
  if (stalled)
    return {
      title: "On, but not running",
      detail:
        "No identity provider can vouch for people on this server right now.",
      tone: "warning",
    };
  if (ready && lastAccepted)
    return {
      title: "Working",
      detail: `People reach ${vendor} through their sign-in.`,
      aside: `${vendor} accepted a sign-in ${formatRelativeTime(lastAccepted)}`,
      tone: "success",
    };
  if (ready)
    return {
      title: "On, not proven yet",
      detail: `Speakeasy’s side is set up. It starts working once ${vendor} trusts an identity provider below and someone connects through this server. Try it from your MCP client.`,
      tone: "warning",
    };
  if (off || !state)
    return {
      title: "Off",
      detail: `People connect ${vendor} themselves the first time they use this server.`,
      tone: "neutral",
    };
  const badge = stateBadge(state);
  return {
    title: badge.label,
    detail: remediation?.trim() || undefined,
    tone: badge.variant,
  };
}
