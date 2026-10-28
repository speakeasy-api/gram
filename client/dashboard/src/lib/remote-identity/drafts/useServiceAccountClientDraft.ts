import { useSdkClient } from "@/contexts/Sdk";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import { isExpectedAPIError } from "@/lib/errors";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllServerClientAssociations } from "@gram/client/react-query/serverClientAssociations.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import { useAllRemoteSessionIssuers } from "../queries/useAllRemoteSessionIssuers";
import { useRemoteSessionIssuersByIds } from "../queries/useRemoteSessionIssuersByIds";

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
  /**
   * What Save would commit, as plain data, for binding to a server that does
   * not exist yet. Null until there is something to commit.
   */
  spec: (format: ServiceAccountOAuthFormat) => ServiceAccountClientSpec | null;
  reset: () => void;
};

/**
 * A service account client to bind once its server exists: the fields for a
 * client secret client to create, or the signing key client already made.
 */
export type ServiceAccountClientSpec =
  | {
      format: "client-secret";
      issuerId: string;
      issuerName: string;
      clientId: string;
      clientSecret: string;
      secretMethod: SecretMethod;
      scopes: string[];
      audience: string;
    }
  | {
      format: "signing-key";
      issuerId: string;
      issuerName: string;
      clientRowId: string;
    };

/**
 * Binds a service account client to a newly created server, creating a client
 * secret client first. Throws when the provider or the commit refuses it; a
 * client secret client made here is deleted on a refusal.
 */
export async function bindServiceAccountClient(
  client: Gram,
  spec: ServiceAccountClientSpec,
  mcpServerId: string,
  options?: RequestOptions,
): Promise<void> {
  const madeHere =
    spec.format === "client-secret"
      ? await client.remoteSessionClients.create(
          {
            createRemoteSessionClientForm: {
              remoteSessionIssuerId: spec.issuerId,
              clientId: spec.clientId,
              credentialOwner: "self",
              scope: spec.scopes.length > 0 ? spec.scopes : undefined,
              audience: spec.audience || undefined,
              tokenEndpointAuthMethod: spec.secretMethod,
              clientSecret: spec.clientSecret,
            },
          },
          undefined,
          options,
        )
      : null;
  const discard = () => {
    if (!madeHere) return;
    void client.remoteSessionClients
      .delete({ id: madeHere.id })
      .catch((error: unknown) =>
        console.error("Couldn't delete an unbound client", error),
      );
  };
  let result;
  try {
    result = await client.remoteSessions.commitServerIdentityConfiguration(
      {
        commitServerIdentityConfigurationForm: {
          mcpServerId,
          providerId: spec.issuerId,
          clientMode: "existing",
          existingClientId:
            madeHere?.id ??
            (spec.format === "signing-key" ? spec.clientRowId : ""),
        },
      },
      undefined,
      options,
    );
  } catch (error) {
    if (isExpectedAPIError(error)) discard();
    throw error;
  }
  if (result.failure) {
    discard();
    throw new Error(
      result.failure.providerMessage ??
        `Couldn't connect this server to ${spec.issuerName}.`,
    );
  }
}

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
  enabled = true,
  deleteCreatedOnUnmount = true,
}: {
  /** The server Save binds to. Absent while the server is being created. */
  mcpServerId?: string;
  /** The Service Account choice is on screen, so its providers are needed. */
  enabled?: boolean;
  /** The service account client this server uses now, if any. */
  boundClient: RemoteSessionClient | undefined;
  /**
   * Delete a signing key client made by "Create client" when the draft
   * unmounts unbound. The creation dialog turns this off: its draft unmounts
   * before the server exists, and the install binds the client afterwards.
   */
  deleteCreatedOnUnmount?: boolean;
}): ServiceAccountClientDraft {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const resolveKeySet = useResolveKeySet();

  // Every provider this project can see. A service account only needs the
  // token endpoint, so a provider without one is listed but refused. The bound
  // client's provider is looked up by id too, since the listing spans the
  // project, its organization and the platform catalog and may not reach it.
  const listed = useAllRemoteSessionIssuers({ enabled });
  const boundIssuer = useRemoteSessionIssuersByIds(
    boundClient ? [boundClient.remoteSessionIssuerId] : [],
  );
  const issuers: RemoteSessionIssuer[] = useMemo(() => {
    const unlisted = boundIssuer.items.filter(
      (bound) => !listed.items.some((item) => item.id === bound.id),
    );
    return [...listed.items, ...unlisted];
  }, [listed.items, boundIssuer.items]);

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
  // A client secret client whose commit ended without an answer, so it may be
  // bound already. A retry with the same fields commits it again rather than
  // making another; it is never deleted, since it may be in use.
  const unsettled = useRef<{
    client: RemoteSessionClient;
    fields: string;
  } | null>(null);
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

  // Everything typed for a provider's registration, which a different
  // provider would not recognize.
  const clearRegistration = useCallback(() => {
    setClientId("");
    setClientSecret("");
    setSecretMethod("client_secret_basic");
    setScopes([]);
    setAudience("");
    setAssertionAudience("issuer");
    setKeySource(null);
    setKeySet(null);
    setCreated(null);
  }, []);

  // Best effort and in the background: a leftover client is clutter, not a
  // failure the operator has to act on.
  const deleteClient = useCallback(
    (id: string) =>
      client.remoteSessionClients
        .delete({ id })
        .then(() => invalidateAllRemoteSessionClients(queryClient))
        .catch((error: unknown) =>
          console.error(
            "Couldn't delete an unbound service account client",
            error,
          ),
        ),
    [client, queryClient],
  );

  // A created client outlives the draft only by mistake: leaving the page
  // before Save would strand it. The browser asks before a reload or close,
  // and an unmount deletes it. The delete waits a tick so a remount, such as
  // StrictMode's dev rehearsal, can call it off and keep the client. Save
  // clears the ref while its commit is out, since that may bind the client.
  const createdRef = useRef(created);
  const pendingDelete = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Set once the unmount is real, so a create still in flight cleans up.
  const disposed = useRef(false);
  const deleteOnUnmount = useRef(deleteCreatedOnUnmount);
  deleteOnUnmount.current = deleteCreatedOnUnmount;
  useEffect(() => {
    createdRef.current = created;
    if (!created) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [created]);
  useEffect(() => {
    if (pendingDelete.current) clearTimeout(pendingDelete.current);
    pendingDelete.current = null;
    return () => {
      pendingDelete.current = setTimeout(() => {
        pendingDelete.current = null;
        disposed.current = true;
        if (createdRef.current && deleteOnUnmount.current) {
          void deleteClient(createdRef.current.id);
        }
      }, 0);
    };
  }, [deleteClient]);

  // Hands an unbound client to the unmount cleanup, or deletes it now when
  // that cleanup has already run, as after a create or a refused commit that
  // outlives the page. Reports whether the draft still holds it.
  const holdForCleanup = (unbound: RemoteSessionClient | null): boolean => {
    if (disposed.current) {
      if (unbound) void deleteClient(unbound.id);
      return false;
    }
    createdRef.current = unbound;
    return true;
  };

  // A signing key client made by "Create client" exists before Save binds it,
  // so dropping it from the draft deletes it too.
  const discardCreated = () => {
    if (created) void deleteClient(created.id);
  };

  const clearDraft = useCallback(() => {
    setIssuerPick(null);
    clearRegistration();
    setReplacing(false);
  }, [clearRegistration]);

  const reset = () => {
    discardCreated();
    clearDraft();
  };

  const pickIssuer = (id: string) => {
    discardCreated();
    setIssuerPick(id);
    clearRegistration();
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
      if (!holdForCleanup(made)) return false;
      setCreated(made);
      await invalidateAllRemoteSessionClients(queryClient);
      return true;
    } finally {
      setCreating(false);
    }
  };

  /** Binds the client to this server, creating a client secret one first. */
  const save = async (format: ServiceAccountOAuthFormat): Promise<boolean> => {
    if (!canSave(format) || !issuer || !mcpServerId) return false;
    setSaving(true);
    const fields = JSON.stringify([
      issuer.id,
      clientId.trim(),
      clientSecret.trim(),
      secretMethod,
      scopes,
      audience.trim(),
    ]);
    try {
      // A client secret client is made by this Save, so a refused commit
      // deletes it rather than leaving a copy behind for every retry. Only a
      // refusal: a lost response or a server fault may have bound it already.
      const madeHere =
        format !== "client-secret"
          ? null
          : unsettled.current?.fields === fields
            ? unsettled.current.client
            : await createSelfClient({
                tokenEndpointAuthMethod: secretMethod,
                clientSecret: clientSecret.trim(),
              });
      unsettled.current = null;
      const discardMadeHere = () => {
        if (madeHere) void deleteClient(madeHere.id);
      };
      // Bound or not is unknown until the commit answers, so an unmount in
      // between must leave a signing key client alone.
      if (!madeHere) createdRef.current = null;
      let result;
      try {
        result = await client.remoteSessions.commitServerIdentityConfiguration({
          commitServerIdentityConfigurationForm: {
            mcpServerId,
            providerId: issuer.id,
            clientMode: "existing",
            existingClientId: (madeHere ?? created!).id,
          },
        });
      } catch (error) {
        if (isExpectedAPIError(error)) {
          discardMadeHere();
          holdForCleanup(created);
        } else if (madeHere) {
          unsettled.current = { client: madeHere, fields };
        }
        throw error;
      }
      if (result.failure) {
        discardMadeHere();
        holdForCleanup(created);
        throw new Error(
          result.failure.providerMessage ??
            `Couldn't connect this server to ${issuer.name}.`,
        );
      }
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllServerClientAssociations(queryClient),
      ]);
      // A signing key client made earlier is unbound once a client secret
      // one takes its place; the bound one is cleared without being deleted.
      if (madeHere) discardCreated();
      clearDraft();
      return true;
    } finally {
      setSaving(false);
    }
  };

  return {
    issuers,
    issuersLoading: listed.isLoading || boundIssuer.isLoading,
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
    spec: (format) => {
      if (!canSave(format) || !issuer) return null;
      if (format === "signing-key") {
        return {
          format,
          issuerId: issuer.id,
          issuerName: issuer.name ?? "the identity provider",
          clientRowId: created!.id,
        };
      }
      return {
        format,
        issuerId: issuer.id,
        issuerName: issuer.name ?? "the identity provider",
        clientId: clientId.trim(),
        clientSecret: clientSecret.trim(),
        secretMethod,
        scopes,
        audience: audience.trim(),
      };
    },
    reset,
  };
}
