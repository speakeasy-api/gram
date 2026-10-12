import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import {
  keySetChoiceReady,
  oauthFormatOf,
  type AssertionAudience,
  type KeySetChoice,
  type SecretMethod,
  type ServiceAccountOAuthFormat,
  type SigningKeySource,
} from "../model/serviceAccount";
import { useResolveKeySet } from "../queries/useKeySets";
import { useAllRemoteSessionClients } from "../queries/useAllRemoteSessionClients";

export type ServiceAccountClientDraft = {
  issuers: RemoteSessionIssuer[];
  issuersLoading: boolean;
  issuer: RemoteSessionIssuer | undefined;
  issuerUsable: boolean;
  pickIssuer: (id: string) => void;
  clientId: string;
  setClientId: (value: string) => void;
  clientSecret: string;
  setClientSecret: (value: string) => void;
  secretMethod: SecretMethod;
  setSecretMethod: (value: SecretMethod) => void;
  scopes: string[];
  setScopes: (value: string[]) => void;
  audience: string;
  setAudience: (value: string) => void;
  assertionAudience: AssertionAudience;
  setAssertionAudience: (value: AssertionAudience) => void;
  keySource: SigningKeySource | null;
  setKeySource: (value: SigningKeySource | null) => void;
  keySet: KeySetChoice | null;
  setKeySet: (value: KeySetChoice | null) => void;
  suggestedKeySetId: string | null;
  /** The sibling clients a suggestion comes from are still loading. */
  suggestionLoading: boolean;
  /** A signing key client made by "Create client" and not yet bound. */
  created: RemoteSessionClient | null;
  creating: boolean;
  signingReady: boolean;
  createSigningClient: () => Promise<boolean>;
  saving: boolean;
  boundClient: RemoteSessionClient | undefined;
  showingBound: (format: ServiceAccountOAuthFormat) => boolean;
  replace: () => void;
  cancelReplace: () => void;
  replacing: boolean;
  canSave: (format: ServiceAccountOAuthFormat) => boolean;
  save: (format: ServiceAccountOAuthFormat) => Promise<boolean>;
  reset: () => void;
};

/**
 * The OAuth half of the Service Account choice: a client that holds one
 * credential for itself (credential_owner self), obtained with the client
 * credentials grant and shared by every caller.
 *
 * A client secret client is created and bound in the panel's one Save. A
 * signing key client is created first, from its own step, because the
 * operator has to register its key URL with the provider before Save can
 * mean anything; Save then binds it.
 */
export function useServiceAccountClientDraft({
  mcpServerId,
  boundClient,
}: {
  mcpServerId: string;
  /** The service account client this server uses now, if any. */
  boundClient: RemoteSessionClient | undefined;
}): ServiceAccountClientDraft {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const resolveKeySet = useResolveKeySet();

  // Every provider this project can see. A service account only needs the
  // token endpoint, so a provider without one is listed but refused.
  const issuersQuery = useRemoteSessionIssuers({}, undefined, {
    throwOnError: false,
  });
  const issuers: RemoteSessionIssuer[] = useMemo(
    () => issuersQuery.data?.result.items ?? [],
    [issuersQuery.data],
  );

  const [issuerPick, setIssuerPick] = useState<string | null>(null);
  const issuerId = issuerPick ?? boundClient?.remoteSessionIssuerId ?? null;
  const issuer = issuers.find((candidate) => candidate.id === issuerId);

  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [secretMethod, setSecretMethod] = useState<SecretMethod>(
    "client_secret_basic",
  );
  const [scopes, setScopes] = useState<string[]>([]);
  const [audience, setAudience] = useState("");
  const [assertionAudience, setAssertionAudience] =
    useState<AssertionAudience>("issuer");
  const [keySource, setKeySource] = useState<SigningKeySource | null>(null);
  const [keySet, setKeySet] = useState<KeySetChoice | null>(null);
  // A signing key client exists from "Create client" until Save binds it.
  const [created, setCreated] = useState<RemoteSessionClient | null>(null);
  const [creating, setCreating] = useState(false);
  const [saving, setSaving] = useState(false);
  // The operator chose to replace the bound client rather than keep it.
  const [replacing, setReplacing] = useState(false);

  // Suggest the key set another client at this provider already signs with,
  // fetched only once a key set is being picked. A failed page stops the wait
  // rather than holding the default back for good.
  const siblingsQuery = useAllRemoteSessionClients(
    { remoteSessionIssuerId: issuerId ?? undefined },
    { enabled: !!issuerId && keySource === "kms" },
  );
  const suggestedKeySetId =
    siblingsQuery.items.find((sibling) => sibling.jsonWebKeySetId != null)
      ?.jsonWebKeySetId ?? null;
  const suggestionLoading = siblingsQuery.isLoading && !siblingsQuery.isError;

  const reset = useCallback(() => {
    setIssuerPick(null);
    setClientId("");
    setClientSecret("");
    setSecretMethod("client_secret_basic");
    setScopes([]);
    setAudience("");
    setAssertionAudience("issuer");
    setKeySource(null);
    setKeySet(null);
    setCreated(null);
    setReplacing(false);
  }, []);

  const pickIssuer = (id: string) => {
    setIssuerPick(id);
    setKeySet(null);
    setCreated(null);
  };

  const issuerUsable = !!issuer?.tokenEndpoint;
  /** Shows the bound client rather than a form for a new one. */
  const showingBound = (format: ServiceAccountOAuthFormat) =>
    !!boundClient && !replacing && oauthFormatOf(boundClient) === format;

  const secretReady =
    issuerUsable && clientId.trim() !== "" && clientSecret.trim() !== "";
  const signingReady =
    issuerUsable &&
    clientId.trim() !== "" &&
    keySource === "kms" &&
    keySetChoiceReady(keySet);

  /** Something for Save to commit in this format. */
  const canSave = (format: ServiceAccountOAuthFormat): boolean => {
    if (showingBound(format)) return false;
    return format === "client-secret" ? secretReady : created !== null;
  };

  const createSelfClient = async (
    fields: Partial<
      Parameters<
        typeof client.remoteSessionClients.create
      >[0]["createRemoteSessionClientForm"]
    >,
  ): Promise<RemoteSessionClient> => {
    if (!issuer) throw new Error("Choose an identity provider");
    return await client.remoteSessionClients.create({
      createRemoteSessionClientForm: {
        remoteSessionIssuerId: issuer.id,
        clientId: clientId.trim(),
        credentialOwner: "self",
        scope: scopes.length > 0 ? scopes : undefined,
        audience: audience.trim() || undefined,
        ...fields,
      },
    });
  };

  /** Step two of the signing key setup: the client, and its key set if new. */
  const createSigningClient = async (): Promise<boolean> => {
    if (!signingReady || !keySet) return false;
    setCreating(true);
    try {
      const jsonWebKeySetId = await resolveKeySet(keySet);
      // Pinned before the client, so a retry after a failed create reuses the
      // set rather than making another.
      setKeySet({ kind: "existing", id: jsonWebKeySetId });
      const made = await createSelfClient({
        tokenEndpointAuthMethod: "private_key_jwt",
        tokenEndpointAuthAudienceFormat: assertionAudience,
        jsonWebKeySetId,
      });
      setCreated(made);
      await invalidateAllRemoteSessionClients(queryClient);
      return true;
    } finally {
      setCreating(false);
    }
  };

  /** Binds the client to this server, creating a client secret one first. */
  const save = async (format: ServiceAccountOAuthFormat): Promise<boolean> => {
    if (!canSave(format) || !issuer) return false;
    setSaving(true);
    try {
      const target =
        format === "client-secret"
          ? await createSelfClient({
              tokenEndpointAuthMethod: secretMethod,
              clientSecret: clientSecret.trim(),
            })
          : created!;
      const result =
        await client.remoteSessions.commitServerIdentityConfiguration({
          commitServerIdentityConfigurationForm: {
            mcpServerId,
            providerId: issuer.id,
            clientMode: "existing",
            existingClientId: target.id,
          },
        });
      if (result.failure) {
        throw new Error(
          result.failure.providerMessage ??
            `Couldn't connect this server to ${issuer.name}.`,
        );
      }
      await invalidateAllRemoteSessionClients(queryClient);
      reset();
      return true;
    } finally {
      setSaving(false);
    }
  };

  return {
    issuers,
    issuersLoading: issuersQuery.isLoading,
    issuer,
    issuerUsable,
    pickIssuer,
    clientId,
    setClientId,
    clientSecret,
    setClientSecret,
    secretMethod,
    setSecretMethod,
    scopes,
    setScopes,
    audience,
    setAudience,
    assertionAudience,
    setAssertionAudience,
    keySource,
    setKeySource,
    keySet,
    setKeySet,
    suggestedKeySetId,
    suggestionLoading,
    created,
    creating,
    signingReady,
    createSigningClient,
    saving,
    boundClient,
    showingBound,
    replace: () => {
      reset();
      setReplacing(true);
    },
    cancelReplace: reset,
    replacing,
    canSave,
    save,
    reset,
  };
}
