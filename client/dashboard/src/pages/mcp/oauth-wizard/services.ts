import type { Gram } from "@gram/client";
import { TokenEndpointAuthMethod } from "@gram/client/models/components";
import {
  buildAddExternalOAuthServerMutation,
  buildCreateRemoteSessionClientMutation,
  buildCreateRemoteSessionIssuerMutation,
  buildCreateUserSessionIssuerMutation,
  buildDiscoverRemoteSessionIssuerMutation,
  buildSetToolsetUserSessionIssuerMutation,
} from "@gram/client/react-query";
import { fromPromise } from "xstate";

import { buildUserSessionResourceSlug } from "@/lib/externalMcpUserSessions";
import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";

import {
  authServerOrigin,
  parseScopes,
  pickAuthMethodFromList,
} from "./machine-types";

type SignalArg = { signal: AbortSignal };

const fetchOptions = ({ signal }: SignalArg) => ({
  options: { fetchOptions: { signal } },
});

// 2 weeks — matches the user-session default used elsewhere.
const DEFAULT_SESSION_DURATION_HOURS = 24 * 14;

function narrowTokenEndpointAuthMethod(
  value: string | null | undefined,
): TokenEndpointAuthMethod | undefined {
  if (
    value === TokenEndpointAuthMethod.ClientSecretBasic ||
    value === TokenEndpointAuthMethod.ClientSecretPost
  ) {
    return value;
  }
  return undefined;
}

export type AddExternalOAuthInput = {
  toolsetSlug: string;
  slug: string;
  metadata: Record<string, unknown>;
};

// The custom path provisions a user_session_issuer + remote_session_issuer +
// remote_session_client from the operator-supplied upstream metadata and
// credentials, then links the toolset — the user-session-backed replacement
// for the removed oauth_proxy_server creation.
export type ProvisionUserSessionInput = {
  toolsetSlug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  tokenAuthMethod: string;
  clientId: string;
  clientSecret: string;
};

export type RegisterClientInput = {
  registrationEndpoint: string;
  // Method derived from the (often empty) discovered metadata. Used as the
  // fallback when live discovery is skipped or fails.
  tokenAuthMethod: string;
  // Origin to run live RFC 8414 discovery against to recover the upstream's
  // real token_endpoint_auth_methods_supported. Empty disables discovery.
  issuer: string;
};

export type RegisterClientOutput = {
  clientId: string;
  clientSecret: string;
  tokenAuthMethod: string | null;
};

export type AuthedFetch = (
  endpoint: string,
  opts: RequestInit,
) => Promise<Response>;

export type WizardServices = {
  addExternalOAuth: ReturnType<typeof fromPromise<void, AddExternalOAuthInput>>;
  provisionUserSession: ReturnType<
    typeof fromPromise<void, ProvisionUserSessionInput>
  >;
  registerClient: ReturnType<
    typeof fromPromise<RegisterClientOutput, RegisterClientInput>
  >;
};

export type GramClient = Gram;

export function createWizardServices(
  client: GramClient,
  authedFetch: AuthedFetch,
): WizardServices {
  const addExternalOAuth = fromPromise<void, AddExternalOAuthInput>(
    async ({ input, signal }) => {
      const { mutationFn } = buildAddExternalOAuthServerMutation(client);
      await mutationFn({
        request: {
          slug: input.toolsetSlug,
          addExternalOAuthServerRequestBody: {
            externalOauthServer: {
              slug: input.slug,
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
      let draft: {
        authorizationEndpoint?: string;
        tokenEndpoint?: string;
        registrationEndpoint?: string;
        jwksUri?: string;
        scopesSupported?: string[];
        grantTypesSupported?: string[];
        responseTypesSupported?: string[];
        tokenEndpointAuthMethodsSupported?: string[];
      } = {};
      if (issuerUrl) {
        try {
          draft = await buildDiscoverRemoteSessionIssuerMutation(
            client,
          ).mutationFn({
            request: {
              discoverRemoteSessionIssuerRequestBody: { issuer: issuerUrl },
            },
            ...opts,
          });
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
            scopesSupported: draft.scopesSupported ?? parseScopes(input.scopes),
            grantTypesSupported: draft.grantTypesSupported ?? [
              "authorization_code",
              "refresh_token",
            ],
            responseTypesSupported: draft.responseTypesSupported ?? ["code"],
            tokenEndpointAuthMethodsSupported:
              draft.tokenEndpointAuthMethodsSupported ?? [
                input.tokenAuthMethod,
              ],
          },
        },
        ...opts,
      });

      await buildCreateRemoteSessionClientMutation(client).mutationFn({
        request: {
          createRemoteSessionClientForm: {
            remoteSessionIssuerId: remoteIssuer.id,
            userSessionIssuerId: issuer.id,
            clientId: input.clientId,
            clientSecret: input.clientSecret || undefined,
            tokenEndpointAuthMethod: narrowTokenEndpointAuthMethod(
              input.tokenAuthMethod,
            ),
          },
        },
        ...opts,
      });

      await buildSetToolsetUserSessionIssuerMutation(client).mutationFn({
        request: {
          slug: input.toolsetSlug,
          setUserSessionIssuerRequestBody: { userSessionIssuerId: issuer.id },
        },
        ...opts,
      });
    },
  );

  const registerClient = fromPromise<RegisterClientOutput, RegisterClientInput>(
    async ({ input, signal }) => {
      // The synthesized remote-MCP metadata omits the supported-methods list,
      // so run a live RFC 8414 discovery against the issuer origin to recover
      // it (e.g. Make advertises only client_secret_post, and rejects DCR that
      // omits it or sends client_secret_basic). Best-effort: any failure leaves
      // the metadata-derived fallback method in place.
      let authMethod = input.tokenAuthMethod;
      if (input.issuer) {
        try {
          const draft = await client.remoteSessionIssuers.discover(
            {
              discoverRemoteSessionIssuerRequestBody: { issuer: input.issuer },
            },
            undefined,
            { fetchOptions: { signal } },
          );
          const supported = draft.tokenEndpointAuthMethodsSupported ?? [];
          if (supported.length > 0) {
            authMethod = pickAuthMethodFromList(supported);
          }
        } catch {
          // Keep the fallback method on discovery failure.
        }
      }

      const result = await proxyRegisterUpstreamClient(
        authedFetch,
        {
          registrationEndpoint: input.registrationEndpoint,
          tokenEndpointAuthMethod: authMethod || undefined,
        },
        { signal },
      );
      return {
        clientId: result.clientId,
        clientSecret: result.clientSecret,
        tokenAuthMethod: result.tokenEndpointAuthMethod,
      };
    },
  );

  return {
    addExternalOAuth,
    provisionUserSession,
    registerClient,
  };
}
