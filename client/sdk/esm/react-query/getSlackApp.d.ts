import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetSlackAppQuery, GetSlackAppQueryData, prefetchGetSlackApp, queryKeyGetSlackApp } from "./getSlackApp.core.js";
export { buildGetSlackAppQuery, type GetSlackAppQueryData, prefetchGetSlackApp, queryKeyGetSlackApp, };
export type GetSlackAppQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getSlackApp slack
 *
 * @remarks
 * Get details of a specific Slack app.
 */
export declare function useGetSlackApp(request: operations.GetSlackAppRequest, security?: operations.GetSlackAppSecurity | undefined, options?: QueryHookOptions<GetSlackAppQueryData, GetSlackAppQueryError>): UseQueryResult<GetSlackAppQueryData, GetSlackAppQueryError>;
/**
 * getSlackApp slack
 *
 * @remarks
 * Get details of a specific Slack app.
 */
export declare function useGetSlackAppSuspense(request: operations.GetSlackAppRequest, security?: operations.GetSlackAppSecurity | undefined, options?: SuspenseQueryHookOptions<GetSlackAppQueryData, GetSlackAppQueryError>): UseSuspenseQueryResult<GetSlackAppQueryData, GetSlackAppQueryError>;
export declare function setGetSlackAppData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetSlackAppQueryData): GetSlackAppQueryData | undefined;
export declare function invalidateGetSlackApp(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetSlackApp(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getSlackApp.d.ts.map