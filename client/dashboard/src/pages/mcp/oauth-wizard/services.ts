import {
  buildAddExternalOAuthServerMutation,
  buildAddOAuthProxyServerMutation,
  buildCreateEnvironmentMutation,
  buildDeleteEnvironmentMutation,
  buildListEnvironmentsQuery,
  useGramContext,
} from "@gram/client/react-query";
import type { QueryClient } from "@tanstack/react-query";
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
  toolsetName: string;
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

export type WizardServices = {
  addExternalOAuth: ReturnType<typeof fromPromise<void, AddExternalOAuthInput>>;
  createEnvironment: ReturnType<
    typeof fromPromise<CreateEnvironmentOutput, CreateEnvironmentInput>
  >;
  addOAuthProxy: ReturnType<typeof fromPromise<void, AddOAuthProxyInput>>;
  deleteEnvironment: ReturnType<
    typeof fromPromise<void, DeleteEnvironmentInput>
  >;
};

export type GramClient = ReturnType<typeof useGramContext>;

export function createWizardServices(
  client: GramClient,
  queryClient: QueryClient,
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

  const createEnvironment = fromPromise<
    CreateEnvironmentOutput,
    CreateEnvironmentInput
  >(async ({ input, signal }) => {
    // Read fresh at submit time so the name-collision check doesn't race a
    // still-loading useListEnvironments() at modal open.
    const envs = await queryClient.fetchQuery(
      buildListEnvironmentsQuery(client),
    );
    const name = nextEnvironmentName(
      input.toolsetName,
      envs.environments.map((e) => e.name),
    );

    const { mutationFn } = buildCreateEnvironmentMutation(client);
    const env = await mutationFn({
      request: {
        createEnvironmentForm: {
          name,
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

  return {
    addExternalOAuth,
    createEnvironment,
    addOAuthProxy,
    deleteEnvironment,
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
