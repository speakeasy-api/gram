import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { VerifyOnboardingHooksSetupResult } from "../models/components/verifyonboardinghookssetupresult.js";
import { VerifyOnboardingHooksSetupRequest, VerifyOnboardingHooksSetupSecurity } from "../models/operations/verifyonboardinghookssetup.js";
export type VerifyOnboardingHooksSetupQueryData = VerifyOnboardingHooksSetupResult;
export declare function prefetchVerifyOnboardingHooksSetup(queryClient: QueryClient, client$: GramCore, request?: VerifyOnboardingHooksSetupRequest | undefined, security?: VerifyOnboardingHooksSetupSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildVerifyOnboardingHooksSetupQuery(client$: GramCore, request?: VerifyOnboardingHooksSetupRequest | undefined, security?: VerifyOnboardingHooksSetupSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<VerifyOnboardingHooksSetupQueryData>;
};
export declare function queryKeyVerifyOnboardingHooksSetup(parameters: {
    sinceUnixNano?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=verifyOnboardingHooksSetup.core.d.ts.map