import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import {
  ListGcpIamCredentialsRequest,
  ListGcpIamCredentialsSecurity,
} from "../models/operations/listgcpiamcredentials.js";
export type ListGcpIamCredentialsQueryData = ListExternalCredentialsResult;
export declare function prefetchListGcpIamCredentials(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListGcpIamCredentialsRequest | undefined,
  security?: ListGcpIamCredentialsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListGcpIamCredentialsQuery(
  client$: GramCore,
  request?: ListGcpIamCredentialsRequest | undefined,
  security?: ListGcpIamCredentialsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListGcpIamCredentialsQueryData>;
};
export declare function queryKeyListGcpIamCredentials(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listGcpIamCredentials.core.d.ts.map
