import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ValidateKeyResult } from "../models/components/validatekeyresult.js";
import { ValidateAPIKeyRequest, ValidateAPIKeySecurity } from "../models/operations/validateapikey.js";
export type ValidateAPIKeyQueryData = ValidateKeyResult;
export declare function prefetchValidateAPIKey(queryClient: QueryClient, client$: GramCore, request?: ValidateAPIKeyRequest | undefined, security?: ValidateAPIKeySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildValidateAPIKeyQuery(client$: GramCore, request?: ValidateAPIKeyRequest | undefined, security?: ValidateAPIKeySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ValidateAPIKeyQueryData>;
};
export declare function queryKeyValidateAPIKey(parameters: {
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=validateAPIKey.core.d.ts.map