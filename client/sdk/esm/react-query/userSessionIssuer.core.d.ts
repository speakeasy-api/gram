import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import { GetUserSessionIssuerRequest, GetUserSessionIssuerSecurity } from "../models/operations/getusersessionissuer.js";
export type UserSessionIssuerQueryData = UserSessionIssuer;
export declare function prefetchUserSessionIssuer(queryClient: QueryClient, client$: GramCore, request?: GetUserSessionIssuerRequest | undefined, security?: GetUserSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildUserSessionIssuerQuery(client$: GramCore, request?: GetUserSessionIssuerRequest | undefined, security?: GetUserSessionIssuerSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<UserSessionIssuerQueryData>;
};
export declare function queryKeyUserSessionIssuer(parameters: {
    id?: string | undefined;
    slug?: string | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionIssuer.core.d.ts.map