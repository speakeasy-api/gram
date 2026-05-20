import type { Gram } from "@gram/client";
import {
  CreateRemoteSessionClientFormTokenEndpointAuthMethod,
  type RemoteSessionClient,
  type RemoteSessionIssuer,
  type UserSessionIssuer,
} from "@gram/client/models/components";
import {
  buildCloneClientFromOAuthProxyProviderMutation,
  buildCreateRemoteSessionClientMutation,
  buildCreateRemoteSessionIssuerMutation,
  buildCreateUserSessionIssuerMutation,
  buildDiscoverRemoteSessionIssuerMutation,
  buildSetToolsetUserSessionIssuerMutation,
} from "@gram/client/react-query";
import { fromPromise } from "xstate";

import {
  type AuthedFetch,
  proxyRegisterUpstreamClient,
} from "@/lib/proxyRegisterUpstreamClient";

import type {
  CreateRemoteSessionClientInput,
  CreateRemoteSessionIssuerInput,
  CreateUserSessionIssuerInput,
  LinkToolsetUserSessionIssuerInput,
  ResolveRemoteSessionClientInput,
  ResolveRemoteSessionIssuerInput,
  ResolveUserSessionIssuerInput,
} from "./machine-types";

type SignalArg = { signal: AbortSignal };

const fetchOptions = ({ signal }: SignalArg) => ({
  options: { fetchOptions: { signal } },
});

function narrowTokenEndpointAuthMethod(
  value: string | null | undefined,
): CreateRemoteSessionClientFormTokenEndpointAuthMethod | undefined {
  if (
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretBasic ||
    value ===
      CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretPost
  ) {
    return value;
  }
  return undefined;
}

export type GramClient = Gram;

export function createMigrationServices(
  client: GramClient,
  authedFetch: AuthedFetch,
) {
  const resolveUserSessionIssuer = fromPromise<
    UserSessionIssuer | null,
    ResolveUserSessionIssuerInput
  >(
    async ({
      input,
      signal,
    }: {
      input: ResolveUserSessionIssuerInput;
    } & SignalArg) => {
      try {
        return await client.userSessionIssuers.get(
          { slug: input.slug },
          undefined,
          fetchOptions({ signal }).options,
        );
      } catch (error) {
        if (isNotFound(error)) return null;
        throw error;
      }
    },
  );

  const resolveRemoteSessionIssuer = fromPromise<
    RemoteSessionIssuer | null,
    ResolveRemoteSessionIssuerInput
  >(
    async ({
      input,
      signal,
    }: {
      input: ResolveRemoteSessionIssuerInput;
    } & SignalArg) => {
      try {
        return await client.remoteSessionIssuers.get(
          { slug: input.slug },
          undefined,
          fetchOptions({ signal }).options,
        );
      } catch (error) {
        if (isNotFound(error)) return null;
        throw error;
      }
    },
  );

  const resolveRemoteSessionClient = fromPromise<
    RemoteSessionClient | null,
    ResolveRemoteSessionClientInput
  >(
    async ({
      input,
      signal,
    }: {
      input: ResolveRemoteSessionClientInput;
    } & SignalArg) => {
      const clients = await client.remoteSessionClients.list(
        {
          remoteSessionIssuerId: input.remoteSessionIssuerId,
          userSessionIssuerId: input.userSessionIssuerId,
          limit: 1,
        },
        undefined,
        fetchOptions({ signal }).options,
      );

      return clients.result.items[0] ?? null;
    },
  );

  const createUserSessionIssuer = fromPromise(
    async ({
      input,
      signal,
    }: {
      input: CreateUserSessionIssuerInput;
    } & SignalArg) => {
      const { mutationFn } = buildCreateUserSessionIssuerMutation(client);
      return await mutationFn({
        request: {
          createUserSessionIssuerForm: {
            slug: input.slug,
            authnChallengeMode: "interactive",
            sessionDurationHours: input.sessionDurationHours,
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  const createRemoteSessionIssuer = fromPromise(
    async ({
      input,
      signal,
    }: {
      input: CreateRemoteSessionIssuerInput;
    } & SignalArg) => {
      const discoverMutation = buildDiscoverRemoteSessionIssuerMutation(client);
      const createMutation = buildCreateRemoteSessionIssuerMutation(client);

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

      try {
        draft = await discoverMutation.mutationFn({
          request: {
            discoverRemoteSessionIssuerRequestBody: {
              issuer: input.issuerUrl,
            },
          },
          ...fetchOptions({ signal }),
        });
      } catch (error) {
        console.info(
          "Remote session issuer discovery failed; falling back to OAuth proxy provider metadata.",
          { issuer: input.issuerUrl, error },
        );
      }

      return await createMutation.mutationFn({
        request: {
          createRemoteSessionIssuerForm: {
            slug: input.slug,
            issuer: input.issuerUrl,
            authorizationEndpoint:
              draft.authorizationEndpoint ??
              input.proxyProvider.authorizationEndpoint,
            tokenEndpoint:
              draft.tokenEndpoint ?? input.proxyProvider.tokenEndpoint,
            registrationEndpoint: draft.registrationEndpoint,
            jwksUri: draft.jwksUri,
            scopesSupported:
              draft.scopesSupported ??
              input.proxyProvider.scopesSupported ??
              [],
            grantTypesSupported:
              draft.grantTypesSupported ??
              input.proxyProvider.grantTypesSupported ??
              [],
            responseTypesSupported: draft.responseTypesSupported ?? [],
            tokenEndpointAuthMethodsSupported:
              draft.tokenEndpointAuthMethodsSupported ??
              input.proxyProvider.tokenEndpointAuthMethodsSupported ??
              [],
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  const createRemoteSessionClient = fromPromise(
    async ({
      input,
      signal,
    }: {
      input: CreateRemoteSessionClientInput;
    } & SignalArg) => {
      switch (input.strategy) {
        case "clone": {
          if (!input.cloneCallbackConfirmed) {
            throw new Error(
              "confirm the upstream redirect URI is registered before cloning",
            );
          }

          const { mutationFn } =
            buildCloneClientFromOAuthProxyProviderMutation(client);
          return await mutationFn({
            request: {
              cloneClientFromOAuthProxyProviderForm: {
                oauthProxyProviderId: input.proxyProviderId,
                remoteSessionIssuerId: input.remoteSessionIssuer.id,
                userSessionIssuerId: input.userSessionIssuerId,
              },
            },
            ...fetchOptions({ signal }),
          });
        }

        case "register": {
          if (!input.remoteSessionIssuer.registrationEndpoint) {
            throw new Error(
              "issuer has no registration_endpoint; register is unavailable",
            );
          }

          const proxyRegistered = await proxyRegisterUpstreamClient(
            authedFetch,
            {
              registrationEndpoint:
                input.remoteSessionIssuer.registrationEndpoint,
            },
            { signal },
          );

          const { mutationFn } = buildCreateRemoteSessionClientMutation(client);
          return await mutationFn({
            request: {
              createRemoteSessionClientForm: {
                remoteSessionIssuerId: input.remoteSessionIssuer.id,
                userSessionIssuerId: input.userSessionIssuerId,
                clientId: proxyRegistered.clientId,
                clientSecret: proxyRegistered.clientSecret || undefined,
                tokenEndpointAuthMethod: narrowTokenEndpointAuthMethod(
                  proxyRegistered.tokenEndpointAuthMethod,
                ),
              },
            },
            ...fetchOptions({ signal }),
          });
        }

        case "manual": {
          if (!input.manualClientId.trim()) {
            throw new Error("client_id is required");
          }

          const { mutationFn } = buildCreateRemoteSessionClientMutation(client);
          return await mutationFn({
            request: {
              createRemoteSessionClientForm: {
                remoteSessionIssuerId: input.remoteSessionIssuer.id,
                userSessionIssuerId: input.userSessionIssuerId,
                clientId: input.manualClientId.trim(),
                clientSecret: input.manualClientSecret || undefined,
              },
            },
            ...fetchOptions({ signal }),
          });
        }
      }
    },
  );

  const linkToolsetUserSessionIssuer = fromPromise(
    async ({
      input,
      signal,
    }: {
      input: LinkToolsetUserSessionIssuerInput;
    } & SignalArg) => {
      const { mutationFn } = buildSetToolsetUserSessionIssuerMutation(client);
      await mutationFn({
        request: {
          slug: input.toolsetSlug,
          setUserSessionIssuerRequestBody: {
            userSessionIssuerId: input.userSessionIssuerId,
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  return {
    resolveUserSessionIssuer,
    resolveRemoteSessionIssuer,
    resolveRemoteSessionClient,
    createUserSessionIssuer,
    createRemoteSessionIssuer,
    createRemoteSessionClient,
    linkToolsetUserSessionIssuer,
  };
}

function isNotFound(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return "statusCode" in error && error.statusCode === 404;
}
