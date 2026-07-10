import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import { ListExternalCredentialsRequest, ListExternalCredentialsSecurity, Provider } from "../models/operations/listexternalcredentials.js";
export type ListExternalCredentialsQueryData = ListExternalCredentialsResult;
export declare function prefetchListExternalCredentials(queryClient: QueryClient, client$: GramCore, request?: ListExternalCredentialsRequest | undefined, security?: ListExternalCredentialsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListExternalCredentialsQuery(client$: GramCore, request?: ListExternalCredentialsRequest | undefined, security?: ListExternalCredentialsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListExternalCredentialsQueryData>;
};
export declare function queryKeyListExternalCredentials(parameters: {
    provider?: Provider | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listExternalCredentials.core.d.ts.map