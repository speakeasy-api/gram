import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListBuiltinExclusionsRequest, ListBuiltinExclusionsSecurity } from "../models/operations/listbuiltinexclusions.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildBuiltinExclusionsQuery, BuiltinExclusionsQueryData, prefetchBuiltinExclusions, queryKeyBuiltinExclusions } from "./builtinExclusions.core.js";
export { buildBuiltinExclusionsQuery, type BuiltinExclusionsQueryData, prefetchBuiltinExclusions, queryKeyBuiltinExclusions, };
export type BuiltinExclusionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listBuiltinExclusions risk
 *
 * @remarks
 * List the built-in exclusion library (known-safe values suppressed before they reach exclusions), grouped by category.
 */
export declare function useBuiltinExclusions(request?: ListBuiltinExclusionsRequest | undefined, security?: ListBuiltinExclusionsSecurity | undefined, options?: QueryHookOptions<BuiltinExclusionsQueryData, BuiltinExclusionsQueryError>): UseQueryResult<BuiltinExclusionsQueryData, BuiltinExclusionsQueryError>;
/**
 * listBuiltinExclusions risk
 *
 * @remarks
 * List the built-in exclusion library (known-safe values suppressed before they reach exclusions), grouped by category.
 */
export declare function useBuiltinExclusionsSuspense(request?: ListBuiltinExclusionsRequest | undefined, security?: ListBuiltinExclusionsSecurity | undefined, options?: SuspenseQueryHookOptions<BuiltinExclusionsQueryData, BuiltinExclusionsQueryError>): UseSuspenseQueryResult<BuiltinExclusionsQueryData, BuiltinExclusionsQueryError>;
export declare function setBuiltinExclusionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: BuiltinExclusionsQueryData): BuiltinExclusionsQueryData | undefined;
export declare function invalidateBuiltinExclusions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllBuiltinExclusions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=builtinExclusions.d.ts.map