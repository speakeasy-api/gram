import type { QueryClient } from "@tanstack/react-query";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import {
  invalidateAllIdentityProviderConnection,
  useIdentityProviderConnection,
} from "@gram/client/react-query/identityProviderConnection.js";
import { invalidateAllIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { invalidateAllOktaResourceConnections } from "@gram/client/react-query/oktaResourceConnections.js";

export const SESSION_SECURITY = { sessionHeaderGramSession: "" } as const;

type OktaConnectionQuery = ReturnType<typeof useIdentityProviderConnection>;

/** Errors render inline, so the query never throws or retries. */
export function useOktaConnection(): {
  query: OktaConnectionQuery;
  connection: OktaIdentityProviderConnection | undefined;
} {
  const query = useIdentityProviderConnection(undefined, SESSION_SECURITY, {
    throwOnError: false,
    retry: false,
  });
  return { query, connection: query.data?.connection };
}

/** Hooks that render their error inline opt out of the global "Request failed" toast. */
export function inlineError(): undefined {
  return undefined;
}

export function invalidateIdentityProviderQueries(
  client: QueryClient,
): Promise<void> {
  return Promise.all([
    invalidateAllIdentityProviderConnection(client),
    invalidateAllIdentityProviderConnectionApplications(client),
    invalidateAllOktaResourceConnections(client),
  ]).then(() => undefined);
}
