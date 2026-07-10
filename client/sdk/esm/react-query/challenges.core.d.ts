import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChallengesResult } from "../models/components/listchallengesresult.js";
import { ListChallengesRequest, ListChallengesSecurity, QueryParamOutcome } from "../models/operations/listchallenges.js";
export type ChallengesQueryData = ListChallengesResult;
export declare function prefetchChallenges(queryClient: QueryClient, client$: GramCore, request?: ListChallengesRequest | undefined, security?: ListChallengesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildChallengesQuery(client$: GramCore, request?: ListChallengesRequest | undefined, security?: ListChallengesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ChallengesQueryData>;
};
export declare function queryKeyChallenges(parameters: {
    outcome?: QueryParamOutcome | undefined;
    principalUrn?: string | undefined;
    scope?: string | undefined;
    projectId?: string | undefined;
    resolved?: boolean | undefined;
    ids?: Array<string> | undefined;
    limit?: number | undefined;
    offset?: number | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=challenges.core.d.ts.map