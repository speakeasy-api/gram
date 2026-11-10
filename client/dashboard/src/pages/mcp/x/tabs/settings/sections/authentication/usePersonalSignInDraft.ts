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

  const issuerQuery = useRemoteSessionIssuer({ id: issuerId }, undefined, {
    enabled: enabled && source === "scratch" && !!issuerId,
    throwOnError: false,
  });
  const issuer =
    source === "scratch" && issuerQuery.data?.id === issuerId
      ? issuerQuery.data
      : undefined;
  const {
    available: dcrAvailable,
    permissionRestricted: tunneledDcrRestricted,
  } = dynamicClientRegistrationAvailability({
    registrationEndpoint: issuer?.registrationEndpoint,
    tunneled: !!issuer?.tunneledMcpServerId,
    isPlatformAdmin,
  });
  const cimdAvailable = !!issuer?.clientIdMetadataDocumentSupported;
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
  } else if (!issuer) {
    problem = "Choose the authorization server people sign in with.";
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
    issuer?.id ?? "",
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

    if (!issuer) {
      throw new Error("Choose the authorization server people sign in with.");
    }
    if (clientType === "cimd") {
      return sdk.remoteSessionClients.createCimd({
        createCimdForm: { remoteSessionIssuerId: issuer.id, ...overrides },
      });
    }
    if (clientType === "dcr") {
      const registered = await proxyRegisterUpstreamClient(authedFetch, {
        registrationEndpoint: issuer.registrationEndpoint ?? "",
        // RFC 7591 §2: scope is a space-separated string at registration.
        scope: parsedScopes.length > 0 ? parsedScopes.join(" ") : undefined,
        tokenEndpointAuthMethod: authMethod || undefined,
        tunneledMcpServerId: issuer.tunneledMcpServerId,
        projectSlug,
      });
      return sdk.remoteSessionClients.create({
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: issuer.id,
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
        remoteSessionIssuerId: issuer.id,
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
      issuerId,
      selectIssuer: (id) => {
        if (id === issuerId) return;
        setIssuerId(id);
        setClientTypePick(null);
        clearForm();
      },
      issuer,
      issuerFailed: issuerQuery.isError,
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
