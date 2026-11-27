import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useFetcher } from "@/contexts/Fetcher";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import {
  proxyRegisterUpstreamClient,
  registrationProvenance,
} from "@/lib/proxyRegisterUpstreamClient";
import {
  copyableAuthMethod,
  usableAsClientTemplate,
} from "@/lib/remote-identity/model/clientConfiguration";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import {
  deriveRemoteSessionIssuerNameFromUrl,
  readableServiceNameFromUrl,
} from "@/lib/sources";
import {
  detectAuthorizationServer,
  issuerFormFromDraft,
} from "@/pages/sources/remote-mcp/detectAuthorizationServer";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useMemo, useState } from "react";
import {
  availableClientTypes,
  type ClientType,
  dynamicClientRegistrationAvailability,
  narrowTokenEndpointAuthMethod,
  parseScopes,
} from "./issuerFormUtils";
import { useSignInChoices } from "./useSignInChoices";

export type PersonalSignInSource = "scratch" | "existing";

/**
 * The authorization server the MCP server advertises, as the sign-in form
 * uses it: preselected when the project has it, added on save when it does
 * not.
 */
export type DetectedSignIn =
  | { state: "checking" }
  | { state: "none" }
  | {
      state: "existing";
      issuerId: string;
      name: string;
      serviceName: string;
      inUse: boolean;
    }
  | { state: "new"; issuer: string; serviceName: string; inUse: boolean };
type AuthMethod = CreateRemoteSessionClientFormTokenEndpointAuthMethod | "";

/**
 * The sign-in a new personal credential gets: always a registration of its
 * own, configured from scratch or started from a stored one. A stored
 * registration is never attached to a second credential, because a person
 * holds one upstream grant per registration and sharing one lets a sign-in on
 * one server replace the grant another server obtained.
 */
export type PersonalSignInDraft = {
  /** Why create cannot run yet, in words for the administrator. */
  problem: string | null;
  /** Changes whenever the registration create would make changes. */
  signature: string;
  /** Creates the registration. The stored one it starts from is only read. */
  create: () => Promise<RemoteSessionClient>;
  /** Internal state for PersonalSignInFields. */
  fields: PersonalSignInFieldsState;
};

type PersonalSignInFieldsState = {
  source: PersonalSignInSource;
  setSource: (source: PersonalSignInSource) => void;
  signIns: ReturnType<typeof useSignInChoices>;
  templates: ReturnType<typeof useSignInChoices>["choices"];
  templateId: string;
  selectTemplate: (id: string) => void;
  toggleShowAll: () => void;
  starting: RemoteSessionClient | undefined;
  issuerId: string;
  selectIssuer: (id: string) => void;
  issuer: ReturnType<typeof useRemoteSessionIssuer>["data"];
  issuerFailed: boolean;
  /** What the server itself says people sign in with. */
  detected: DetectedSignIn;
  /** Go back to the detected authorization server after picking another. */
  useDetected: () => void;
  clientTypes: ClientType[];
  clientType: ClientType;
  setClientType: (type: ClientType) => void;
  tunneledDcrRestricted: boolean;
  clientId: string;
  setClientId: (value: string) => void;
  clientSecret: string;
  setClientSecret: (value: string) => void;
  authMethod: AuthMethod;
  setAuthMethod: (value: AuthMethod) => void;
  scope: string;
  setScope: (value: string) => void;
  audience: string;
  setAudience: (value: string) => void;
};

export function usePersonalSignInDraft({
  remoteMcpServerId,
  enabled,
}: {
  remoteMcpServerId: string | undefined;
  enabled: boolean;
}): PersonalSignInDraft {
  const sdk = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const isPlatformAdmin = useIsPlatformAdmin();
  const projectSlug = useProjectSlugForRequests();

  const [source, setSourceState] = useState<PersonalSignInSource>("scratch");
  const [templateId, setTemplateId] = useState("");
  const [issuerId, setIssuerId] = useState("");
  // The authorization server this form added from the detected metadata, so
  // a retry after it was added reads as the same sign-in.
  const [addedIssuerId, setAddedIssuerId] = useState("");
  const [clientTypePick, setClientTypePick] = useState<ClientType | null>(null);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [authMethod, setAuthMethod] = useState<AuthMethod>("");
  const [scope, setScope] = useState("");
  const [audience, setAudience] = useState("");

  const signIns = useSignInChoices(
    enabled ? "subject" : undefined,
    remoteMcpServerId,
  );
  // A hosted document is registered again, which its provider must still
  // support; without the provider's record that cannot be known.
  const templates = signIns.choices.filter((choice) =>
    usableAsClientTemplate(
      choice.registration,
      !!choice.issuer?.clientIdMetadataDocumentSupported,
    ),
  );
  // Only the administrator's own pick: one that left the list is never
  // swapped for another.
  const starting =
    source === "existing"
      ? templates.find((choice) => choice.registration.id === templateId)
          ?.registration
      : undefined;
  const startsFromDocument = !!starting?.clientIdMetadataUri;

  // Until someone picks one, the authorization server is the one the MCP
  // server advertises: preselected when the project has it, and added on
  // save when it does not, as setting up a new server does.
  const queryClient = useQueryClient();
  const detection = useQuery({
    queryKey: ["detect-authorization-server", remoteMcpServerId],
    queryFn: ({ signal }) =>
      detectAuthorizationServer(sdk, remoteMcpServerId!, { signal }),
    enabled: enabled && source === "scratch" && !!remoteMcpServerId,
    retry: false,
    throwOnError: false,
  });
  const detectedIssuerId =
    detection.data?.kind === "existing" ? detection.data.issuer.id : "";
  const effectiveIssuerId = issuerId || detectedIssuerId;
  // Never from a result being refreshed: another sheet may have added that
  // authorization server since it was read.
  const pendingIssuer: RemoteSessionIssuerDraft | undefined =
    source === "scratch" &&
    !issuerId &&
    !detection.isFetching &&
    detection.data?.kind === "new"
      ? detection.data.draft
      : undefined;

  const issuerQuery = useRemoteSessionIssuer(
    { id: effectiveIssuerId },
    undefined,
    {
      enabled: enabled && source === "scratch" && !!effectiveIssuerId,
      throwOnError: false,
    },
  );
  const issuer =
    source === "scratch" && issuerQuery.data?.id === effectiveIssuerId
      ? issuerQuery.data
      : undefined;
  const {
    available: dcrAvailable,
    permissionRestricted: tunneledDcrRestricted,
  } = dynamicClientRegistrationAvailability({
    registrationEndpoint:
      issuer?.registrationEndpoint ?? pendingIssuer?.registrationEndpoint,
    tunneled: !!issuer?.tunneledMcpServerId,
    isPlatformAdmin,
  });
  const cimdAvailable = !!(
    issuer?.clientIdMetadataDocumentSupported ??
    pendingIssuer?.clientIdMetadataDocumentSupported
  );
  const clientTypes = useMemo(
    () => availableClientTypes({ dcrAvailable, cimdAvailable }),
    [dcrAvailable, cimdAvailable],
  );
  const clientType: ClientType =
    clientTypePick && clientTypes.includes(clientTypePick)
      ? clientTypePick
      : (clientTypes[0] ?? "manual");

  const clearForm = () => {
    setClientId("");
    setClientSecret("");
    setAuthMethod("");
    setScope("");
    setAudience("");
  };

  const selectTemplate = (id: string) => {
    const picked = templates.find((choice) => choice.registration.id === id);
    if (!picked) return;
    const registration = picked.registration;
    setTemplateId(registration.id);
    setClientId(registration.clientIdMetadataUri ? "" : registration.clientId);
    // Never a stored or masked secret: blank asks the server to copy it.
    setClientSecret("");
    setAuthMethod(
      copyableAuthMethod(registration.tokenEndpointAuthMethod) ?? "",
    );
    setScope(registration.scope?.join(", ") ?? "");
    setAudience(registration.audience ?? "");
  };

  // A stored secret belongs to one registration, so the server is asked to
  // copy it only while the client ID is still the starting one's.
  const sameRegistration =
    !!starting && !startsFromDocument && clientId.trim() === starting.clientId;

  let problem: string | null = null;
  if (source === "existing") {
    if (!starting) problem = "Choose the sign-in to start from.";
    else if (!startsFromDocument && !clientId.trim())
      problem = "Enter the client ID.";
  } else if (!issuer && !pendingIssuer) {
    problem =
      !issuerId && detection.isFetching
        ? "Checking which authorization server this server uses."
        : "Choose the authorization server people sign in with.";
  } else if (clientType === "manual" && !clientId.trim()) {
    problem = "Enter the client ID.";
  }
  // A method that sends a secret needs one: typed, or the stored one the
  // server copies for the same registration.
  const typesCredentials =
    source === "existing"
      ? !!starting && !startsFromDocument
      : clientType === "manual";
  if (
    !problem &&
    typesCredentials &&
    (authMethod === "client_secret_basic" ||
      authMethod === "client_secret_post") &&
    !clientSecret.trim() &&
    !sameRegistration
  ) {
    problem = "Enter the client secret, or choose an auth method without one.";
  }

  const signature = JSON.stringify([
    source,
    starting?.id ?? "",
    // The server this form added still reads as the detected one, so a
    // retry after adding it does not read as a change.
    pendingIssuer || (issuer && issuer.id === addedIssuerId)
      ? `detected:${pendingIssuer?.issuer ?? issuer?.issuer}`
      : (issuer?.id ?? ""),
    source === "scratch" ? clientType : "",
    clientId.trim(),
    clientSecret,
    authMethod,
    scope,
    audience,
  ]);

  const create = async (): Promise<RemoteSessionClient> => {
    const parsedScopes = parseScopes(scope);
    const overrides = {
      scope: parsedScopes.length > 0 ? parsedScopes : undefined,
      audience: audience.trim() || undefined,
    };
    if (source === "existing") {
      if (!starting) throw new Error("Choose the sign-in to start from.");
      if (startsFromDocument) {
        return sdk.remoteSessionClients.createCimd({
          createCimdForm: {
            remoteSessionIssuerId: starting.remoteSessionIssuerId,
            ...overrides,
          },
        });
      }
      return sdk.remoteSessionClients.create({
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: starting.remoteSessionIssuerId,
          clientId: clientId.trim(),
          clientSecret: clientSecret.trim() || undefined,
          // Names the stored registration the server copies the secret and
          // callback from. The secret never passes through the browser.
          templateClientId: sameRegistration ? starting.id : undefined,
          tokenEndpointAuthMethod: authMethod || undefined,
          ...overrides,
        },
      });
    }

    let target = issuer;
    if (!target && pendingIssuer) {
      target = await sdk.remoteSessionIssuers.create({
        createRemoteSessionIssuerForm: issuerFormFromDraft(
          pendingIssuer,
          readableServiceNameFromUrl(pendingIssuer.issuer) ??
            (deriveRemoteSessionIssuerNameFromUrl(pendingIssuer.issuer) ||
              "mcp"),
        ),
      });
      // A retry after a later step fails uses the server just added. It
      // stays if a later step fails: it is reusable, and the next attempt
      // preselects it.
      setAddedIssuerId(target.id);
      setIssuerId(target.id);
      void invalidateAllRemoteSessionIssuers(queryClient);
      void queryClient.invalidateQueries({
        queryKey: ["detect-authorization-server"],
      });
    }
    if (!target) {
      throw new Error("Choose the authorization server people sign in with.");
    }
    if (clientType === "cimd") {
      return sdk.remoteSessionClients.createCimd({
        createCimdForm: { remoteSessionIssuerId: target.id, ...overrides },
      });
    }
    if (clientType === "dcr") {
      const registered = await proxyRegisterUpstreamClient(authedFetch, {
        registrationEndpoint: target.registrationEndpoint ?? "",
        // RFC 7591 §2: scope is a space-separated string at registration.
        scope: parsedScopes.length > 0 ? parsedScopes.join(" ") : undefined,
        tokenEndpointAuthMethod: authMethod || undefined,
        tunneledMcpServerId: target.tunneledMcpServerId,
        projectSlug,
      });
      return sdk.remoteSessionClients.create({
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: target.id,
          clientId: registered.clientId,
          clientSecret: registered.clientSecret || undefined,
          tokenEndpointAuthMethod:
            narrowTokenEndpointAuthMethod(registered.tokenEndpointAuthMethod) ??
            (authMethod || undefined),
          ...overrides,
          ...registrationProvenance(registered),
        },
      });
    }
    return sdk.remoteSessionClients.create({
      createRemoteSessionClientForm: {
        remoteSessionIssuerId: target.id,
        clientId: clientId.trim(),
        clientSecret: clientSecret.trim() || undefined,
        tokenEndpointAuthMethod: authMethod || undefined,
        ...overrides,
      },
    });
  };

  return {
    problem,
    signature,
    create,
    fields: {
      source,
      setSource: (next) => {
        if (next === source) return;
        // A starting sign-in's settings do not pose as typed ones, or the
        // reverse.
        setSourceState(next);
        setTemplateId("");
        clearForm();
        signIns.setShowAll(false);
      },
      signIns,
      templates,
      templateId: starting?.id ?? "",
      selectTemplate,
      // A choice the narrower list leaves out must not stay selected unseen.
      toggleShowAll: () => {
        if (
          signIns.showAll &&
          !signIns.matching.some((c) => c.registration.id === templateId)
        ) {
          setTemplateId("");
          clearForm();
        }
        signIns.setShowAll(!signIns.showAll);
      },
      starting,
      issuerId: effectiveIssuerId,
      selectIssuer: (id) => {
        if (id === effectiveIssuerId) return;
        setIssuerId(id);
        setClientTypePick(null);
        clearForm();
      },
      issuer,
      issuerFailed: issuerQuery.isError,
      useDetected: () => {
        setIssuerId("");
        setClientTypePick(null);
        clearForm();
      },
      detected:
        source !== "scratch" || !remoteMcpServerId
          ? { state: "none" }
          : detection.isPending && detection.fetchStatus !== "idle"
            ? { state: "checking" }
            : detection.data?.kind === "existing"
              ? {
                  state: "existing",
                  issuerId: detection.data.issuer.id,
                  name:
                    detection.data.issuer.name?.trim() ||
                    detection.data.issuer.issuer,
                  serviceName:
                    detection.data.issuer.name?.trim() ||
                    readableServiceNameFromUrl(detection.data.issuer.issuer) ||
                    detection.data.issuer.issuer,
                  inUse: effectiveIssuerId === detection.data.issuer.id,
                }
              : detection.data?.kind === "new"
                ? {
                    state: "new",
                    issuer: detection.data.draft.issuer,
                    serviceName:
                      readableServiceNameFromUrl(detection.data.draft.issuer) ??
                      detection.data.draft.issuer,
                    // In use until someone picks another: still so once this
                    // form has added it, and while detection is refreshed.
                    inUse: !issuerId || issuerId === addedIssuerId,
                  }
                : { state: "none" },
      clientTypes,
      clientType,
      setClientType: setClientTypePick,
      tunneledDcrRestricted,
      clientId,
      setClientId,
      clientSecret,
      setClientSecret,
      authMethod,
      setAuthMethod,
      scope,
      setScope,
      audience,
      setAudience,
    },
  };
}
