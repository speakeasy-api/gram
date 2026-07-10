import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CheckMCPSlugAvailabilityRequest, CheckMCPSlugAvailabilitySecurity } from "../models/operations/checkmcpslugavailability.js";
export type CheckMCPSlugAvailabilityQueryData = boolean;
export declare function prefetchCheckMCPSlugAvailability(queryClient: QueryClient, client$: GramCore, request: CheckMCPSlugAvailabilityRequest, security?: CheckMCPSlugAvailabilitySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildCheckMCPSlugAvailabilityQuery(client$: GramCore, request: CheckMCPSlugAvailabilityRequest, security?: CheckMCPSlugAvailabilitySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<CheckMCPSlugAvailabilityQueryData>;
};
export declare function queryKeyCheckMCPSlugAvailability(parameters: {
    slug: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=checkMCPSlugAvailability.core.d.ts.map