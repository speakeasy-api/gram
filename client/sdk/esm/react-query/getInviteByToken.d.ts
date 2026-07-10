import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetInviteByTokenQuery, GetInviteByTokenQueryData, prefetchGetInviteByToken, queryKeyGetInviteByToken } from "./getInviteByToken.core.js";
export { buildGetInviteByTokenQuery, type GetInviteByTokenQueryData, prefetchGetInviteByToken, queryKeyGetInviteByToken, };
export type GetInviteByTokenQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getInviteByToken organizations
 *
 * @remarks
 * Resolve a WorkOS invitation from its token (e.g. accept-flow).
 */
export declare function useGetInviteByToken(request: operations.GetInviteByTokenRequest, options?: QueryHookOptions<GetInviteByTokenQueryData, GetInviteByTokenQueryError>): UseQueryResult<GetInviteByTokenQueryData, GetInviteByTokenQueryError>;
/**
 * getInviteByToken organizations
 *
 * @remarks
 * Resolve a WorkOS invitation from its token (e.g. accept-flow).
 */
export declare function useGetInviteByTokenSuspense(request: operations.GetInviteByTokenRequest, options?: SuspenseQueryHookOptions<GetInviteByTokenQueryData, GetInviteByTokenQueryError>): UseSuspenseQueryResult<GetInviteByTokenQueryData, GetInviteByTokenQueryError>;
export declare function setGetInviteByTokenData(client: QueryClient, queryKeyBase: [parameters: {
    token: string;
}], data: GetInviteByTokenQueryData): GetInviteByTokenQueryData | undefined;
export declare function invalidateGetInviteByToken(client: QueryClient, queryKeyBase: TupleToPrefixes<[parameters: {
    token: string;
}]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetInviteByToken(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getInviteByToken.d.ts.map