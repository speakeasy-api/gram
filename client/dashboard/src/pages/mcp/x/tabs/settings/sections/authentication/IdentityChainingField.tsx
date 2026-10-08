import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge, type BadgeProps } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
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
import { VendorEmaTrustNotice } from "@/components/vendor-ema-trust-notice";
import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useProtectedResourceMetadata } from "@/lib/remote-identity/queries/useProtectedResourceMetadata";
import type { IdentityChainingPreparation } from "@gram/client/models/components/identitychainingpreparation.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useRemoteSessionClientsPrepareEMAMutation } from "@gram/client/react-query/remoteSessionClientsPrepareEMA.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { buildRemoteSessionClientsReadEMAMutation } from "@gram/client/react-query/remoteSessionClientsReadEMA.js";
import { useRemoteSessionClientsUnlinkEMAMutation } from "@gram/client/react-query/remoteSessionClientsUnlinkEMA.js";

import { AuthRow } from "./AuthRow";
import { IdentityChainingScopesField } from "./IdentityChainingScopesField";
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
  sameScopes,
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

function ClientPicker({
  clients,
  value,
  onChange,
}: {
  clients: RemoteSessionClient[];
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange}>
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

/** Rendered only for an organization issuer that trusts an identity provider sign-in client. */
export function IdentityChainingField({
  userSessionIssuer,
  linkedClients,
  resource,
  permissionResourceId,
  remoteMcpServerId,
}: {
  userSessionIssuer: UserSessionIssuer;
  linkedClients: RemoteSessionClient[];
  /** The remote MCP server URL; the executor matches it exactly. */
  resource: string;
  permissionResourceId: string;
  /** Reuses the server's protected resource metadata probe for advertised scopes. */
  remoteMcpServerId?: string;
}): JSX.Element {
  const core = useGramContext();
  const gramProject = useProjectSlugForRequests();
  const candidates = linkedClients.filter(
    (client) => client.id !== userSessionIssuer.trustedRemoteSessionClientId,
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
  const selected =
    candidates.find((client) => client.id === selectedId) ?? candidates[0];

  const target = selected && {
    userSessionIssuerId: userSessionIssuer.id,
    remoteSessionIssuerId: selected.remoteSessionIssuerId,
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
  const [scopeDraft, setScopeDraft] = useState<{
    clientId: string;
    scopes: string[];
  } | null>(null);
  const bound = chainingBound(read.data);
  const storedScopes = bound && read.data ? read.data.scopes : [];
  const activeScopes = bound
    ? sanitizeChainingScopes(storedScopes)
    : defaultChainingScopes(selected?.scope);
  const scopes =
    scopeDraft && scopeDraft.clientId === selected?.id
      ? scopeDraft.scopes
      : activeScopes;
  const scopesValid =
    selected != null && chainingScopesAllowed(scopes, selected.scope);
  const scopesChanged =
    bound &&
    !sameScopes(
      sanitizeChainingScopes(scopes),
      sanitizeChainingScopes(storedScopes),
    );
  const [confirmDisable, setConfirmDisable] = useState(false);
  const trustedIssuerQuery = useRemoteSessionIssuer(
    { id: userSessionIssuer.trustedRemoteSessionIssuerId ?? "" },
    undefined,
    {
      enabled: !!userSessionIssuer.trustedRemoteSessionIssuerId,
      throwOnError: false,
      retry: false,
    },
  );

  const { hasScope } = useRBAC();
  const organization = useOrganization();
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
  const prepare = useRemoteSessionClientsPrepareEMAMutation({
    onSuccess: (result) => {
      if (!preparationSucceeded(result)) {
        setPrepareOutcome(result);
        toast.warning(
          `Identity chaining not enabled: ${stateBadge(result.state).label}`,
        );
        return;
      }
      queryClient.setQueryData(readKey, result);
      // Declared grants are written onto the client row.
      void invalidateAllRemoteSessionClients(queryClient);
      setScopeDraft(null);
      setGrantsConfirmed(false);
      toast.success(`Identity chaining: ${stateBadge(result.state).label}`);
    },
    onError: () => undefined,
    onSettled,
  });
  const unlink = useRemoteSessionClientsUnlinkEMAMutation({
    onSuccess: () => {
      setScopeDraft(null);
      toast.success("Identity chaining disabled");
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
    prepare.mutate({
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
    });
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
  const badge = shownState ? stateBadge(shownState) : null;
  const canUnlink = chainingUnlinkable(read.data);
  const enableBlocked =
    publicClient ||
    !scopesValid ||
    grantsBlocked ||
    (needsGrantConfirm && !grantsConfirmed);

  return (
    <>
      <AuthRow
        label="Identity chaining"
        hint="Exchange the person’s identity provider sign-in for a token to this server, without a separate consent."
      >
        {candidates.length === 0 ? (
          <Text muted small>
            Link a client for this server’s identity provider first.
          </Text>
        ) : (
          <>
            {candidates.length > 1 && (
              <ClientPicker
                clients={candidates}
                value={selected?.id ?? ""}
                onChange={setSelectedId}
              />
            )}
            {selected && (
              <IdentityChainingScopesField
                client={selected}
                resourceScopes={prm.metadata?.scopesSupported}
                value={scopes}
                onChange={(next) => {
                  setPrepareOutcome(null);
                  setScopeDraft({ clientId: selected.id, scopes: next });
                }}
                disabled={pending || !read.data}
              />
            )}
            <div className="flex flex-wrap items-center gap-3">
              {badge && (
                <Badge variant={badge.variant} size="sm">
                  {badge.label}
                </Badge>
              )}
              {read.isPending && (
                <Text muted small>
                  Checking…
                </Text>
              )}
              <RequireScope
                scope="mcp:write"
                resourceId={permissionResourceId}
                level="component"
              >
                {ready ? (
                  scopesChanged && (
                    <Button
                      size="sm"
                      disabled={pending || enableBlocked}
                      onClick={enable}
                    >
                      Update scopes
                    </Button>
                  )
                ) : (
                  <Button
                    size="sm"
                    disabled={pending || !read.data || enableBlocked}
                    onClick={enable}
                  >
                    Enable identity chaining
                  </Button>
                )}
                {canUnlink && (
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={pending}
                    onClick={() => setConfirmDisable(true)}
                  >
                    Disable
                  </Button>
                )}
              </RequireScope>
            </div>
            {publicClient && selected && (
              <Text muted small className="block">
                Client {selected.clientId} is a public client. Identity chaining
                needs a confidential client that authenticates to the provider’s
                token endpoint.
              </Text>
            )}
            {!publicClient &&
              needsGrantConfirm &&
              grantDeclaration &&
              selected && (
                <RequireScope
                  scope="mcp:write"
                  resourceId={permissionResourceId}
                  level="component"
                >
                  {grantsBlocked ? (
                    <Text muted small className="block">
                      This client belongs to the organization. An organization
                      admin must confirm its grants before enabling identity
                      chaining.
                    </Text>
                  ) : (
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
                  )}
                </RequireScope>
              )}
            {!ready && (
              <VendorEmaTrustNotice
                issuerUrl={trustedIssuerQuery.data?.issuer}
              />
            )}
            {prepareOutcome?.remediation ? (
              <Alert variant="warning" dismissible={false}>
                {prepareOutcome.remediation}
              </Alert>
            ) : (
              read.data?.remediation &&
              !ready &&
              shownState !== "unlinked" && (
                <Text muted small className="block">
                  {read.data.remediation}
                </Text>
              )
            )}
            <ApiErrorAlert
              error={read.error ?? prepare.error ?? unlink.error}
            />
          </>
        )}
      </AuthRow>
      <Dialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Disable identity chaining?</Dialog.Title>
            <Dialog.Description>
              Speakeasy stops exchanging sign-ins for tokens to this server.
              Every person using it falls back to interactive sign-in and
              authorizes the server themselves.
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
              {unlink.isPending ? "Disabling…" : "Disable"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
