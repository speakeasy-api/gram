import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { organizationUserSessionIssuersGetDeletePreflight } from "@gram/client/funcs/organizationUserSessionIssuersGetDeletePreflight.js";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  invalidateAllListJsonWebKeys,
  useListJsonWebKeys,
} from "@gram/client/react-query/listJsonWebKeys.js";
import {
  invalidateAllListJsonWebKeySets,
  useListJsonWebKeySets,
} from "@gram/client/react-query/listJsonWebKeySets.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import {
  invalidateAllOrganizationRemoteSessionClients,
  useOrganizationRemoteSessionClientsInfinite,
} from "@gram/client/react-query/organizationRemoteSessionClients.js";
import {
  invalidateAllOrganizationUserSessionIssuers,
  useOrganizationUserSessionIssuersInfinite,
} from "@gram/client/react-query/organizationUserSessionIssuers.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { unwrapAsync } from "@gram/client/types/fp.js";

import { SESSION_SECURITY } from "../../identityProviderQueries";
import { ExternalKeySelect } from "../../../encryption-keys/jwks/ExternalKeySelect";
import {
  isConnectionVerified,
  type LiveConnection,
} from "../../connectionView";
import { CopyableValue, Fact, FactList } from "./OktaConnectionDetails";
import { OktaSignInIssuers } from "./OktaSignInIssuers";
import {
  activePublicJwk,
  clientJwksUrl,
  findSignInClient,
  freeSignInIssuerSlug,
  isSignInClientReady,
  managedExternalKeyIds,
  managedKeySetIds,
  setUpOktaSignIn,
  signInIssuerRows,
  staleSignInClientIds,
  type KeySetChoice,
  type OktaSignInSetupInput,
  needsTrustConfirmation,
  type SignInIssuerRow,
  type TrustSignInConfirmation,
} from "./oktaSignIn";
import { TrustSignInConfirmDialog } from "./TrustSignInConfirmDialog";

export const OKTA_SIGN_IN_SECTION_ID = "okta-sign-in";
const CREATE_KEY_SET = "__create__";

function prerequisiteHint(connection: LiveConnection): string | null {
  if (!isConnectionVerified(connection)) {
    return "Available once the connection is verified.";
  }
  if (!connection.agentId) {
    return "Available once the AI agent is recorded.";
  }
  if (!connection.remoteSessionIssuerId) {
    return "Available once Speakeasy finishes provisioning this connection.";
  }
  return null;
}

function keySetChoiceFor(
  choice: string,
  externalKeyId: string,
): KeySetChoice | undefined {
  if (choice !== CREATE_KEY_SET) return { kind: "existing", setId: choice };
  return externalKeyId ? { kind: "create", externalKeyId } : undefined;
}

function footerHint(
  trustingCount: number,
  newIssuerSlug: string,
  orgUrl: string,
): string {
  if (trustingCount === 0) {
    return `Creates the ${newIssuerSlug} sign-in issuer and links it to Okta. To use an existing sign-in issuer, trust Okta sign-in on it in the table.`;
  }
  return `Okta (${orgUrl}) signs people in through the linked app.`;
}

/** Fetches every page; a failed page stops the walk and surfaces as `error`. */
function useFetchAllPages(query: {
  isPending: boolean;
  error: Error | null;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
  fetchNextPage: () => Promise<unknown>;
}): { isPending: boolean; error: Error | null } {
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = query;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
      void fetchNextPage();
    }
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);
  return {
    isPending: query.isPending || (hasNextPage && !isFetchNextPageError),
    error: query.error,
  };
}

/** Every organization sign-in issuer, across all pages. */
function useAllOrganizationSignInIssuers(): {
  issuers: UserSessionIssuer[];
  isPending: boolean;
  error: Error | null;
} {
  const query = useOrganizationUserSessionIssuersInfinite(
    undefined,
    undefined,
    { throwOnError: false, retry: false },
  );
  const status = useFetchAllPages(query);
  const issuers = useMemo(
    () =>
      (query.data?.pages.flatMap((page) => page.result.items) ?? []).filter(
        (issuer) => issuer.projectId === "",
      ),
    [query.data],
  );
  return { issuers, ...status };
}

/** Every client of the connection's remote session issuer, across all pages. */
function useAllRemoteSessionClients(issuerId: string): {
  clients: RemoteSessionClient[];
  isPending: boolean;
  isSuccess: boolean;
  error: Error | null;
} {
  const query = useOrganizationRemoteSessionClientsInfinite(
    { issuerId },
    undefined,
    { throwOnError: false, retry: false },
  );
  const status = useFetchAllPages(query);
  const clients = useMemo(
    () =>
      query.data?.pages.flatMap((page) =>
        page.result.items.map((item) => item.client),
      ) ?? [],
    [query.data],
  );
  return {
    clients,
    ...status,
    isSuccess: !status.isPending && status.error == null,
  };
}

function KeySetPicker({
  sets,
  choice,
  onChoiceChange,
  externalKeyId,
  onExternalKeyIdChange,
  managedKeyIds,
}: {
  sets: JSONWebKeySet[];
  choice: string;
  onChoiceChange: (value: string) => void;
  externalKeyId: string;
  onExternalKeyIdChange: (value: string) => void;
  managedKeyIds: ReadonlySet<string>;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label>Signing key set</Label>
        <Select value={choice} onValueChange={onChoiceChange}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {sets.map((set) => (
              <SelectItem key={set.id} value={set.id}>
                {set.name}
              </SelectItem>
            ))}
            <SelectItem value={CREATE_KEY_SET}>
              Create a new set from a KMS key
            </SelectItem>
          </SelectContent>
        </Select>
        <Text small muted>
          Speakeasy signs its client assertions to Okta with this set. A new set
          needs a Google Cloud KMS key; AWS KMS keys cannot back a signing key
          set.
        </Text>
      </div>
      {choice === CREATE_KEY_SET && (
        <ExternalKeySelect
          value={externalKeyId}
          onChange={onExternalKeyIdChange}
          unavailableKeyIds={managedKeyIds}
          unavailableReason="managed by the Okta connection"
        />
      )}
    </div>
  );
}

function SignInPublicKey({ setId }: { setId: string }): JSX.Element {
  const keysQuery = useListJsonWebKeys({ setId }, SESSION_SECURITY, {
    throwOnError: false,
    retry: false,
  });
  const jwk = activePublicJwk(keysQuery.data?.keys ?? []);
  if (keysQuery.isPending) {
    return (
      <Text muted small>
        Loading the public key…
      </Text>
    );
  }
  if (keysQuery.error) return <ApiErrorAlert error={keysQuery.error} />;
  if (!jwk) {
    return (
      <Text small warning>
        The signing key set has no active key. Run Set up Okta sign-in to
        publish one.
      </Text>
    );
  }
  const json = JSON.stringify(jwk, null, 2);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <Label>Agent public key (JWK)</Label>
        <CopyButton text={json} size="xs" tooltip="Copy public key" />
      </div>
      <pre className="bg-muted overflow-x-auto rounded-md p-3 font-mono text-xs [overflow-wrap:anywhere] whitespace-pre-wrap">
        {json}
      </pre>
      <Text small muted>
        In Okta, paste this key in the agent&apos;s Credentials and click
        Activate. Okta stores the key itself, not a key URL: rotating this key
        set or publishing a new key in it means registering the new public key
        in Okta again.
      </Text>
    </div>
  );
}

function SignInFacts({
  client,
  agentId,
}: {
  client: RemoteSessionClient | undefined;
  agentId: string;
}): JSX.Element {
  return (
    <FactList>
      <Fact label="Sign-in client" mono>
        <CopyableValue value={agentId} tooltip="Copy client ID" />
        {!client && (
          <Badge variant="neutral" size="sm">
            Not set up
          </Badge>
        )}
      </Fact>
      {client?.federatedCallbackUrl && (
        <Fact label="Sign-in redirect URI" mono>
          <CopyableValue
            value={client.federatedCallbackUrl}
            tooltip="Copy sign-in redirect URI"
          />
        </Fact>
      )}
      {client && (
        <Fact label="Key URL (JWKS URI)" mono>
          <CopyableValue
            value={clientJwksUrl(client)}
            tooltip="Copy key URL (JWKS URI)"
          />
        </Fact>
      )}
      {client && (
        <Fact label="Client authentication">
          {client.tokenEndpointAuthMethod ?? "client_secret_basic"}
          {client.jsonWebKeySetId == null && " · no signing key set"}
        </Fact>
      )}
    </FactList>
  );
}

function OktaSignInBody({
  connection,
  agentId,
  remoteSessionIssuerId,
}: {
  connection: LiveConnection;
  agentId: string;
  remoteSessionIssuerId: string;
}): JSX.Element {
  const core = useGramContext();
  const queryClient = useQueryClient();
  const clientsQuery = useAllRemoteSessionClients(remoteSessionIssuerId);
  const issuersQuery = useAllOrganizationSignInIssuers();
  const organization = useOrganization();
  const { data: features } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );
  const notEntitled = features?.customerManagedEncryptionKeysEnabled === false;
  const clients = clientsQuery.clients;
  const { client, duplicates } = findSignInClient(
    clients,
    agentId,
    connection.clientId,
  );
  const needsKeySet = client?.jsonWebKeySetId == null;
  const setsQuery = useListJsonWebKeySets(undefined, undefined, {
    enabled:
      clientsQuery.isSuccess && needsKeySet && !notEntitled && duplicates === 0,
    throwOnError: false,
    retry: false,
  });
  const managedSetIds = managedKeySetIds(clients, agentId);
  const allSets = setsQuery.data?.sets ?? [];
  const sets = allSets.filter((set) => !managedSetIds.has(set.id));
  const managedKeyIds = managedExternalKeyIds(allSets, managedSetIds);
  const issuers = issuersQuery.issuers;
  const rows = signInIssuerRows(
    issuers,
    remoteSessionIssuerId,
    client,
    staleSignInClientIds(clients, agentId, connection.clientId),
  );
  const trusting = rows.filter((row) => row.trustsSignIn);
  const stale = rows.filter((row) => row.trustsStale);
  const newIssuerSlug = freeSignInIssuerSlug(issuers);

  const [keySetChoice, setKeySetChoice] = useState<string | null>(null);
  const [externalKeyId, setExternalKeyId] = useState("");
  const [confirmation, setConfirmation] =
    useState<TrustSignInConfirmation | null>(null);
  const [checkingTrust, setCheckingTrust] = useState(false);
  const effectiveKeySetChoice = keySetChoice ?? sets[0]?.id ?? CREATE_KEY_SET;

  // Guards the render before ExternalKeySelect clears a now-managed pick.
  const usableExternalKeyId = managedKeyIds.has(externalKeyId)
    ? ""
    : externalKeyId;
  const keySet = needsKeySet
    ? keySetChoiceFor(effectiveKeySetChoice, usableExternalKeyId)
    : undefined;

  const setup = useMutation({
    mutationFn: (input: OktaSignInSetupInput) => setUpOktaSignIn(core, input),
    onSuccess: ({ issuer }) => {
      setConfirmation(null);
      toast.success(`Okta sign-in set up on ${issuer.slug}`);
    },
    onSettled: () =>
      Promise.all([
        invalidateAllOrganizationRemoteSessionClients(queryClient),
        invalidateAllOrganizationRemoteSessionClient(queryClient),
        invalidateAllOrganizationUserSessionIssuers(queryClient),
        invalidateAllListJsonWebKeySets(queryClient),
        invalidateAllListJsonWebKeys(queryClient),
      ]),
  });
  const runSetup = (userSessionIssuer: UserSessionIssuer | undefined) =>
    setup.mutate({
      remoteSessionIssuerId,
      agentId,
      existingClient: client,
      keySet,
      userSessionIssuer,
      newIssuerSlug,
      onKeySetCreated: setKeySetChoice,
    });

  const trustRow = async (row: SignInIssuerRow) => {
    setCheckingTrust(true);
    let owners: Pick<TrustSignInConfirmation, "servers" | "toolsets"> = {
      servers: [],
      toolsets: [],
    };
    let lookupFailed = false;
    try {
      const preflight = await unwrapAsync(
        organizationUserSessionIssuersGetDeletePreflight(core, {
          id: row.issuer.id,
        }),
      );
      owners = { servers: preflight.mcpServers, toolsets: preflight.toolsets };
    } catch {
      lookupFailed = true;
    } finally {
      setCheckingTrust(false);
    }
    const pending = {
      issuer: row.issuer,
      trustsOther: row.trustsOther,
      ...owners,
      lookupFailed,
    };
    if (needsTrustConfirmation(pending)) {
      setup.reset();
      setConfirmation(pending);
    } else runSetup(row.issuer);
  };

  const loading = clientsQuery.isPending || issuersQuery.isPending;
  const configured =
    client != null && isSignInClientReady(client) && trusting.length > 0;
  // Without the clients or issuers, setup would duplicate the client or issuer.
  const blockingError = clientsQuery.error ?? issuersQuery.error;
  const loadError = blockingError ?? setsQuery.error;
  const ready = !loading && blockingError == null;
  const setupBlocked =
    !ready ||
    setup.isPending ||
    checkingTrust ||
    notEntitled ||
    duplicates > 0 ||
    (needsKeySet && (setsQuery.isLoading || !keySet));

  return (
    <>
      <SettingsSection.Body>
        {loading ? (
          <Text muted small>
            Loading sign-in settings…
          </Text>
        ) : (
          ready && <SignInFacts client={client} agentId={agentId} />
        )}
        {ready && duplicates > 0 && (
          <Alert variant="error" dismissible={false}>
            {`${duplicates} organization sign-in clients use the agent ID ${agentId}, so Speakeasy uses none of them. Delete the extras under Remote identity providers, then set up Okta sign-in again.`}
          </Alert>
        )}
        {ready && notEntitled && (
          <Alert variant="warning" dismissible={false}>
            Okta sign-in signs with a key in your Google Cloud KMS, which needs
            customer-managed encryption keys. They are not enabled for this
            organization; contact Speakeasy to enable them.
          </Alert>
        )}
        {ready && client?.jsonWebKeySetId != null && !notEntitled && (
          <SignInPublicKey setId={client.jsonWebKeySetId} />
        )}
        {ready && needsKeySet && !notEntitled && duplicates === 0 && (
          <KeySetPicker
            sets={sets}
            choice={effectiveKeySetChoice}
            onChoiceChange={setKeySetChoice}
            externalKeyId={externalKeyId}
            onExternalKeyIdChange={setExternalKeyId}
            managedKeyIds={managedKeyIds}
          />
        )}
        {ready && stale.length > 0 && (
          <Alert variant="warning" dismissible={false}>
            {stale.map((row) => row.issuer.slug).join(", ")}{" "}
            {stale.length === 1 ? "still trusts" : "still trust"} the sign-in
            client of a previous agent ID. Trust Okta sign-in on{" "}
            {stale.length === 1 ? "it" : "them"} to move to the current agent.
          </Alert>
        )}
        {ready && (
          <OktaSignInIssuers
            rows={rows}
            issuers={issuers}
            remoteSessionIssuerId={remoteSessionIssuerId}
            client={client}
            trustDisabled={setupBlocked}
            onTrust={(row) => void trustRow(row)}
          />
        )}
        <ApiErrorAlert error={loadError} />
        {!confirmation && <ApiErrorAlert error={setup.error} />}
      </SettingsSection.Body>
      <SettingsSection.Footer>
        <SettingsSection.FooterHint>
          {footerHint(trusting.length, newIssuerSlug, connection.orgUrl)}
        </SettingsSection.FooterHint>
        <SettingsSection.FooterActions>
          <RequireScope scope="org:admin" level="component">
            <Button
              variant={configured ? "secondary" : "primary"}
              disabled={setupBlocked}
              onClick={() => runSetup(trusting[0]?.issuer)}
            >
              {setup.isPending ? "Setting up…" : "Set up Okta sign-in"}
            </Button>
          </RequireScope>
        </SettingsSection.FooterActions>
      </SettingsSection.Footer>
      <TrustSignInConfirmDialog
        confirmation={confirmation}
        pending={setup.isPending}
        error={setup.error}
        onCancel={() => setConfirmation(null)}
        onConfirm={runSetup}
      />
    </>
  );
}

export function OktaSignInSection({
  connection,
}: {
  connection: LiveConnection;
}): JSX.Element {
  const hint = prerequisiteHint(connection);
  const { agentId, remoteSessionIssuerId } = connection;
  return (
    <SettingsSection id={OKTA_SIGN_IN_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Okta sign-in</SettingsSection.Title>
        <SettingsSection.Description>
          The AI agent’s linked app becomes the app people use to sign in to
          Speakeasy. In Okta, paste the public key below in the agent’s
          Credentials and click Activate, then add the sign-in redirect URI to
          the linked app.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {hint || !agentId || !remoteSessionIssuerId ? (
          <>
            <SettingsSection.Body>
              <Text muted small>
                {hint}
              </Text>
            </SettingsSection.Body>
            <SettingsSection.Footer>
              <SettingsSection.FooterActions>
                <Button disabled>Set up Okta sign-in</Button>
              </SettingsSection.FooterActions>
            </SettingsSection.Footer>
          </>
        ) : (
          <OktaSignInBody
            connection={connection}
            agentId={agentId}
            remoteSessionIssuerId={remoteSessionIssuerId}
          />
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
