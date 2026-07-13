import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetRiskPolicyChallengeResponseBody } from "../models/components/getriskpolicychallengeresponsebody.js";
import { GetRiskPolicyChallengeRequest, GetRiskPolicyChallengeSecurity } from "../models/operations/getriskpolicychallenge.js";
export type RiskGetPolicyChallengeQueryData = GetRiskPolicyChallengeResponseBody;
export declare function prefetchRiskGetPolicyChallenge(queryClient: QueryClient, client$: GramCore, request: GetRiskPolicyChallengeRequest, security?: GetRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskGetPolicyChallengeQuery(client$: GramCore, request: GetRiskPolicyChallengeRequest, security?: GetRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskGetPolicyChallengeQueryData>;
};
export declare function queryKeyRiskGetPolicyChallenge(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskGetPolicyChallenge.core.d.ts.map