import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import { ListAwsIamCredentialsRequest, ListAwsIamCredentialsSecurity } from "../models/operations/listawsiamcredentials.js";
export type ListAwsIamCredentialsQueryData = ListExternalCredentialsResult;
export declare function prefetchListAwsIamCredentials(queryClient: QueryClient, client$: GramCore, request?: ListAwsIamCredentialsRequest | undefined, security?: ListAwsIamCredentialsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListAwsIamCredentialsQuery(client$: GramCore, request?: ListAwsIamCredentialsRequest | undefined, security?: ListAwsIamCredentialsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListAwsIamCredentialsQueryData>;
};
export declare function queryKeyListAwsIamCredentials(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAwsIamCredentials.core.d.ts.map