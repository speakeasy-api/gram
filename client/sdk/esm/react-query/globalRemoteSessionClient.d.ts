import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetGlobalRemoteSessionClientRequest, GetGlobalRemoteSessionClientSecurity } from "../models/operations/getglobalremotesessionclient.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGlobalRemoteSessionClientQuery, GlobalRemoteSessionClientQueryData, prefetchGlobalRemoteSessionClient, queryKeyGlobalRemoteSessionClient } from "./globalRemoteSessionClient.core.js";
export { buildGlobalRemoteSessionClientQuery, type GlobalRemoteSessionClientQueryData, prefetchGlobalRemoteSessionClient, queryKeyGlobalRemoteSessionClient, };
export type GlobalRemoteSessionClientQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getGlobalClient adminRemoteSessions
 *
 * @remarks
 * Get a global remote_session_client by id. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClient(request: GetGlobalRemoteSessionClientRequest, security?: GetGlobalRemoteSessionClientSecurity | undefined, options?: QueryHookOptions<GlobalRemoteSessionClientQueryData, GlobalRemoteSessionClientQueryError>): UseQueryResult<GlobalRemoteSessionClientQueryData, GlobalRemoteSessionClientQueryError>;
/**
 * getGlobalClient adminRemoteSessions
 *
 * @remarks
 * Get a global remote_session_client by id. Requires platform admin.
 */
export declare function useGlobalRemoteSessionClientSuspense(request: GetGlobalRemoteSessionClientRequest, security?: GetGlobalRemoteSessionClientSecurity | undefined, options?: SuspenseQueryHookOptions<GlobalRemoteSessionClientQueryData, GlobalRemoteSessionClientQueryError>): UseSuspenseQueryResult<GlobalRemoteSessionClientQueryData, GlobalRemoteSessionClientQueryError>;
export declare function setGlobalRemoteSessionClientData(client: QueryClient, queryKeyBase: [parameters: {
    id: string;
    gramSession?: string | undefined;
}], data: GlobalRemoteSessionClientQueryData): GlobalRemoteSessionClientQueryData | undefined;
export declare function invalidateGlobalRemoteSessionClient(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGlobalRemoteSessionClient(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=globalRemoteSessionClient.d.ts.map