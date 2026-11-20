import { buildAddExternalOAuthServerMutation } from "@gram/client/react-query/addExternalOAuthServer.js";
import { buildCreateRemoteSessionClientMutation } from "@gram/client/react-query/createRemoteSessionClient.js";
import { buildCreateRemoteSessionIssuerMutation } from "@gram/client/react-query/createRemoteSessionIssuer.js";
import { buildCreateUserSessionIssuerMutation } from "@gram/client/react-query/createUserSessionIssuer.js";
import { buildFetchRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/fetchRemoteSessionIssuerMetadata.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import type { Gram } from "@gram/client";
import { fromPromise } from "xstate";

import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { linkToolsetUserSessionIssuer } from "@/lib/linkUserSessionIssuer";

import { authServerOrigin, parseScopes } from "./machine-types";

type SignalArg = { signal: AbortSignal };

const fetchOptions = ({ signal }: SignalArg) => ({
  options: { fetchOptions: { signal } },
});

// 2 weeks — matches the user-session default used elsewhere.
const DEFAULT_SESSION_DURATION_HOURS = 24 * 14;

function narrowTokenEndpointAuthMethod(
  value: string | null | undefined,
): CreateRemoteSessionClientFormTokenEndpointAuthMethod | undefined {
  if (
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretBasic ||
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretPost ||
    value === CreateRemoteSessionClientFormTokenEndpointAuthMethod.None
  ) {
    return value;
  }
  return undefined;
}

export type AddExternalOAuthInput = {
  toolsetSlug: string;
  authorizationServerIssuer?: string;
  metadata?: Record<string, unknown>;
};

export type DiscoverExternalOAuthInput = { issuer: string };
export type DiscoverExternalOAuthOutput = RemoteSessionIssuerDraft;

// The custom path provisions a user_session_issuer + remote_session_issuer +
// remote_session_client from the operator-supplied upstream metadata and
// credentials, then links the toolset — the user-session-backed replacement
// for the removed oauth_proxy_server creation.
export type ProvisionUserSessionInput = {
  toolsetId: string;
  toolsetSlug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  tokenAuthMethod: string;
  clientId: string;
  clientSecret: string;
  audience: string;
};

export type WizardServices = {
  addExternalOAuth: ReturnType<typeof fromPromise<void, AddExternalOAuthInput>>;
  discoverExternalOAuth: ReturnType<
    typeof fromPromise<DiscoverExternalOAuthOutput, DiscoverExternalOAuthInput>
  >;
  provisionUserSession: ReturnType<
    typeof fromPromise<void, ProvisionUserSessionInput>
  >;
};

export type GramClient = Gram;

export function createWizardServices(client: GramClient): WizardServices {
  const discoverExternalOAuth = fromPromise<
    DiscoverExternalOAuthOutput,
    DiscoverExternalOAuthInput
  >(async ({ input, signal }) => {
    const { mutationFn } =
      buildFetchRemoteSessionIssuerMetadataMutation(client);
    return mutationFn({
      request: { fetchIssuerMetadataRequestBody: { issuer: input.issuer } },
      ...fetchOptions({ signal }),
    });
  });

  const addExternalOAuth = fromPromise<void, AddExternalOAuthInput>(
    async ({ input, signal }) => {
      const { mutationFn } = buildAddExternalOAuthServerMutation(client);
      await mutationFn({
        request: {
          slug: input.toolsetSlug,
          addExternalOAuthServerRequestBody: {
            externalOauthServer: {
              authorizationServerIssuer: input.authorizationServerIssuer,
              metadata: input.metadata,
            },
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  const provisionUserSession = fromPromise<void, ProvisionUserSessionInput>(
    async ({ input, signal }) => {
      const opts = fetchOptions({ signal });
      const slug = buildUserSessionResourceSlug(input.toolsetSlug);

      const issuer = await buildCreateUserSessionIssuerMutation(
        client,
      ).mutationFn({
        request: {
          createUserSessionIssuerForm: {
            slug,
            authnChallengeMode: "interactive",
            sessionDurationHours: DEFAULT_SESSION_DURATION_HOURS,
          },
        },
        ...opts,
      });

      // Best-effort RFC 8414 discovery to enrich the remote issuer; falls back
      // to the operator-supplied endpoints.
      const issuerUrl =
        authServerOrigin(input.authorizationEndpoint, input.tokenEndpoint) ||
        input.authorizationEndpoint;
      let draft: Partial<RemoteSessionIssuerDraft> = {};
      let discovered = false;
      if (issuerUrl) {
        try {
          draft = await buildFetchRemoteSessionIssuerMetadataMutation(
            client,
          ).mutationFn({
            request: {
              fetchIssuerMetadataRequestBody: { issuer: issuerUrl },
            },
            ...opts,
          });
          discovered = true;
        } catch {
          // Keep the operator-supplied endpoints on discovery failure.
        }
      }

      const remoteIssuer = await buildCreateRemoteSessionIssuerMutation(
        client,
      ).mutationFn({
        request: {
          createRemoteSessionIssuerForm: {
            slug,
            issuer: issuerUrl,
            authorizationEndpoint:
              draft.authorizationEndpoint ?? input.authorizationEndpoint,
            tokenEndpoint: draft.tokenEndpoint ?? input.tokenEndpoint,
            registrationEndpoint: draft.registrationEndpoint,
            jwksUri: draft.jwksUri,
            // Operator-entered scopes stay explicit; the other lists restate the document.
            scopesSupported: draft.scopesSupported ?? parseScopes(input.scopes),
            grantTypesSupported:
              draft.grantTypesSupported ??
              (discovered ? [] : ["authorization_code", "refresh_token"]),
            responseTypesSupported:
              draft.responseTypesSupported ?? (discovered ? [] : ["code"]),
            tokenEndpointAuthMethodsSupported:
              draft.tokenEndpointAuthMethodsSupported ??
              (discovered ? [] : [input.tokenAuthMethod]),
            // Discovery-only capabilities; all undefined when discovery failed.
            authorizationGrantProfilesSupported: discovered
              ? (draft.authorizationGrantProfilesSupported ?? [])
              : undefined,
            codeChallengeMethodsSupported:
              draft.codeChallengeMethodsSupported ?? undefined,
            clientIdMetadataDocumentSupported:
              draft.clientIdMetadataDocumentSupported,
            serviceDocumentation: draft.serviceDocumentation,
            userinfoEndpoint: draft.userinfoEndpoint,
            introspectionEndpoint: draft.introspectionEndpoint,
            introspectionEndpointAuthMethodsSupported:
              draft.introspectionEndpointAuthMethodsSupported ?? undefined,
            idTokenSigningAlgValuesSupported:
              draft.idTokenSigningAlgValuesSupported ?? undefined,
            claimsSupported: draft.claimsSupported ?? undefined,
            backchannelLogoutSupported: draft.backchannelLogoutSupported,
            authorizationResponseIssParameterSupported:
              draft.authorizationResponseIssParameterSupported,
          },
        },
        ...opts,
      });

      await buildCreateRemoteSessionClientMutation(client).mutationFn({
        request: {
          createRemoteSessionClientForm: {
            remoteSessionIssuerId: remoteIssuer.id,
            userSessionIssuerIds: [issuer.id],
            clientId: input.clientId,
            clientSecret: input.clientSecret || undefined,
            tokenEndpointAuthMethod: narrowTokenEndpointAuthMethod(
              input.tokenAuthMethod,
            ),
            audience: input.audience || undefined,
          },
        },
        ...opts,
      });

      await linkToolsetUserSessionIssuer(
        client,
        { id: input.toolsetId },
        issuer.id,
        opts.options,
      );
    },
  );

  return {
    addExternalOAuth,
    discoverExternalOAuth,
    provisionUserSession,
  };
}
