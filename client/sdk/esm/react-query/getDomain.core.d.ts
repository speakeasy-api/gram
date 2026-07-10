import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CustomDomain } from "../models/components/customdomain.js";
import { GetDomainRequest, GetDomainSecurity } from "../models/operations/getdomain.js";
export type GetDomainQueryData = CustomDomain;
export declare function prefetchGetDomain(queryClient: QueryClient, client$: GramCore, request?: GetDomainRequest | undefined, security?: GetDomainSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetDomainQuery(client$: GramCore, request?: GetDomainRequest | undefined, security?: GetDomainSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetDomainQueryData>;
};
export declare function queryKeyGetDomain(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getDomain.core.d.ts.map