import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OnboardingStatusResult } from "../models/components/onboardingstatusresult.js";
import { GetOnboardingStatusRequest, GetOnboardingStatusSecurity } from "../models/operations/getonboardingstatus.js";
export type OnboardingStatusQueryData = OnboardingStatusResult;
export declare function prefetchOnboardingStatus(queryClient: QueryClient, client$: GramCore, request?: GetOnboardingStatusRequest | undefined, security?: GetOnboardingStatusSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOnboardingStatusQuery(client$: GramCore, request?: GetOnboardingStatusRequest | undefined, security?: GetOnboardingStatusSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OnboardingStatusQueryData>;
};
export declare function queryKeyOnboardingStatus(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=onboardingStatus.core.d.ts.map