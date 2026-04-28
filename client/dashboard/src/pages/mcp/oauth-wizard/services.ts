import {
  buildAddExternalOAuthServerMutation,
  buildAddOAuthProxyServerMutation,
  buildCreateEnvironmentMutation,
  buildDeleteEnvironmentMutation,
  buildUpdateOAuthProxyServerMutation,
  useGramContext,
} from "@gram/client/react-query";
import { fromPromise } from "xstate";

import { parseScopes } from "./machine-types";

type SignalArg = { signal: AbortSignal };

const fetchOptions = ({ signal }: SignalArg) => ({
  options: { fetchOptions: { signal } },
});

export type AddExternalOAuthInput = {
  toolsetSlug: string;
  slug: string;
  metadata: Record<string, unknown>;
};

export type CreateEnvironmentInput = {
  organizationId: string;
  name: string;
  clientId: string;
  clientSecret: string;
};

export type CreateEnvironmentOutput = { envSlug: string };

export type AddOAuthProxyInput = {
  toolsetSlug: string;
  slug: string;
  audience: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  tokenAuthMethod: string;
  environmentSlug: string;
};

export type DeleteEnvironmentInput = { envSlug: string };

export type UpdateOAuthProxyInput = {
  toolsetSlug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  audience: string;
  audienceDirty: boolean;
  tokenAuthMethod: string;
  environmentSlug: string;
};

export type WizardServices = {
  addExternalOAuth: ReturnType<typeof fromPromise<void, AddExternalOAuthInput>>;
  createEnvironment: ReturnType<
    typeof fromPromise<CreateEnvironmentOutput, CreateEnvironmentInput>
  >;
  addOAuthProxy: ReturnType<typeof fromPromise<void, AddOAuthProxyInput>>;
  deleteEnvironment: ReturnType<
    typeof fromPromise<void, DeleteEnvironmentInput>
  >;
  updateOAuthProxy: ReturnType<typeof fromPromise<void, UpdateOAuthProxyInput>>;
};

export type GramClient = ReturnType<typeof useGramContext>;

export function createWizardServices(client: GramClient): WizardServices {
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

  const createEnvironment = fromPromise<
    CreateEnvironmentOutput,
    CreateEnvironmentInput
  >(async ({ input, signal }) => {
    const { mutationFn } = buildCreateEnvironmentMutation(client);
    const env = await mutationFn({
      request: {
        createEnvironmentForm: {
          name: input.name,
          organizationId: input.organizationId,
          entries: [
            { name: "CLIENT_ID", value: input.clientId },
            { name: "CLIENT_SECRET", value: input.clientSecret },
          ],
        },
      },
      ...fetchOptions({ signal }),
    });
    return { envSlug: env.slug };
  });

  const addOAuthProxy = fromPromise<void, AddOAuthProxyInput>(
    async ({ input, signal }) => {
      const { mutationFn } = buildAddOAuthProxyServerMutation(client);
      await mutationFn({
        request: {
          slug: input.toolsetSlug,
          addOAuthProxyServerRequestBody: {
            oauthProxyServer: {
              providerType: "custom",
              slug: input.slug,
              audience: input.audience || undefined,
              authorizationEndpoint: input.authorizationEndpoint,
              tokenEndpoint: input.tokenEndpoint,
              scopesSupported: parseScopes(input.scopes),
              tokenEndpointAuthMethodsSupported: [input.tokenAuthMethod],
              environmentSlug: input.environmentSlug,
            },
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  const deleteEnvironment = fromPromise<void, DeleteEnvironmentInput>(
    async ({ input, signal }) => {
      const { mutationFn } = buildDeleteEnvironmentMutation(client);
      await mutationFn({
        request: { slug: input.envSlug },
        ...fetchOptions({ signal }),
      });
    },
  );

  const updateOAuthProxy = fromPromise<void, UpdateOAuthProxyInput>(
    async ({ input, signal }) => {
      const { mutationFn } = buildUpdateOAuthProxyServerMutation(client);
      await mutationFn({
        request: {
          slug: input.toolsetSlug,
          updateOAuthProxyServerRequestBody: {
            oauthProxyServer: {
              audience: input.audienceDirty ? input.audience : undefined,
              authorizationEndpoint: input.authorizationEndpoint,
              tokenEndpoint: input.tokenEndpoint,
              scopesSupported: parseScopes(input.scopes),
              tokenEndpointAuthMethodsSupported: [input.tokenAuthMethod],
              environmentSlug: input.environmentSlug || undefined,
            },
          },
        },
        ...fetchOptions({ signal }),
      });
    },
  );

  return {
    addExternalOAuth,
    createEnvironment,
    addOAuthProxy,
    deleteEnvironment,
    updateOAuthProxy,
  };
}

export function nextEnvironmentName(
  toolsetName: string,
  existingNames: string[],
): string {
  const base = `${toolsetName} OAuth`;
  const set = new Set(existingNames);
  if (!set.has(base)) return base;
  let suffix = 1;
  while (set.has(`${base} ${suffix}`)) suffix++;
  return `${base} ${suffix}`;
}
