import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RBACStatus } from "../models/components/rbacstatus.js";
import { GetRBACStatusRequest, GetRBACStatusSecurity } from "../models/operations/getrbacstatus.js";
export type RbacStatusQueryData = RBACStatus;
export declare function prefetchRbacStatus(queryClient: QueryClient, client$: GramCore, request?: GetRBACStatusRequest | undefined, security?: GetRBACStatusSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRbacStatusQuery(client$: GramCore, request?: GetRBACStatusRequest | undefined, security?: GetRBACStatusSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RbacStatusQueryData>;
};
export declare function queryKeyRbacStatus(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=rbacStatus.core.d.ts.map