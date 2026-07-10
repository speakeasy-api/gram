import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRemoteSessionClientRequest, GetRemoteSessionClientSecurity } from "../models/operations/getremotesessionclient.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRemoteSessionClientQuery, prefetchRemoteSessionClient, queryKeyRemoteSessionClient, RemoteSessionClientQueryData } from "./remoteSessionClient.core.js";
export { buildRemoteSessionClientQuery, prefetchRemoteSessionClient, queryKeyRemoteSessionClient, type RemoteSessionClientQueryData, };
export type RemoteSessionClientQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Get a remote_session_client by id.
 */
export declare function useRemoteSessionClient(request: GetRemoteSessionClientRequest, security?: GetRemoteSessionClientSecurity | undefined, options?: QueryHookOptions<RemoteSessionClientQueryData, RemoteSessionClientQueryError>): UseQueryResult<RemoteSessionClientQueryData, RemoteSessionClientQueryError>;
/**
 * getRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Get a remote_session_client by id.
 */
export declare function useRemoteSessionClientSuspense(request: GetRemoteSessionClientRequest, security?: GetRemoteSessionClientSecurity | undefined, options?: SuspenseQueryHookOptions<RemoteSessionClientQueryData, RemoteSessionClientQueryError>): UseSuspenseQueryResult<RemoteSessionClientQueryData, RemoteSessionClientQueryError>;
export declare function setRemoteSessionClientData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RemoteSessionClientQueryData): RemoteSessionClientQueryData | undefined;
export declare function invalidateRemoteSessionClient(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRemoteSessionClient(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=remoteSessionClient.d.ts.map