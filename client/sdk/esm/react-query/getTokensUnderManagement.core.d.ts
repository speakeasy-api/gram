import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TokensUnderManagement } from "../models/components/tokensundermanagement.js";
import { GetTokensUnderManagementRequest, GetTokensUnderManagementSecurity } from "../models/operations/gettokensundermanagement.js";
export type GetTokensUnderManagementQueryData = TokensUnderManagement;
export declare function prefetchGetTokensUnderManagement(queryClient: QueryClient, client$: GramCore, request?: GetTokensUnderManagementRequest | undefined, security?: GetTokensUnderManagementSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetTokensUnderManagementQuery(client$: GramCore, request?: GetTokensUnderManagementRequest | undefined, security?: GetTokensUnderManagementSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetTokensUnderManagementQueryData>;
};
export declare function queryKeyGetTokensUnderManagement(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getTokensUnderManagement.core.d.ts.map