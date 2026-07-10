import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
import { GetSourceEnvironmentRequest, GetSourceEnvironmentSecurity, QueryParamSourceKind } from "../models/operations/getsourceenvironment.js";
export type GetSourceEnvironmentQueryData = Environment;
export declare function prefetchGetSourceEnvironment(queryClient: QueryClient, client$: GramCore, request: GetSourceEnvironmentRequest, security?: GetSourceEnvironmentSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetSourceEnvironmentQuery(client$: GramCore, request: GetSourceEnvironmentRequest, security?: GetSourceEnvironmentSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetSourceEnvironmentQueryData>;
};
export declare function queryKeyGetSourceEnvironment(parameters: {
    sourceKind: QueryParamSourceKind;
    sourceSlug: string;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getSourceEnvironment.core.d.ts.map