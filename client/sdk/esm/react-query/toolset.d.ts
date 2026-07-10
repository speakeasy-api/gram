import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetToolsetRequest, GetToolsetSecurity } from "../models/operations/gettoolset.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildToolsetQuery, prefetchToolset, queryKeyToolset, ToolsetQueryData } from "./toolset.core.js";
export { buildToolsetQuery, prefetchToolset, queryKeyToolset, type ToolsetQueryData, };
export type ToolsetQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getToolset toolsets
 *
 * @remarks
 * Get detailed information about a toolset including full HTTP tool definitions
 */
export declare function useToolset(request: GetToolsetRequest, security?: GetToolsetSecurity | undefined, options?: QueryHookOptions<ToolsetQueryData, ToolsetQueryError>): UseQueryResult<ToolsetQueryData, ToolsetQueryError>;
/**
 * getToolset toolsets
 *
 * @remarks
 * Get detailed information about a toolset including full HTTP tool definitions
 */
export declare function useToolsetSuspense(request: GetToolsetRequest, security?: GetToolsetSecurity | undefined, options?: SuspenseQueryHookOptions<ToolsetQueryData, ToolsetQueryError>): UseSuspenseQueryResult<ToolsetQueryData, ToolsetQueryError>;
export declare function setToolsetData(client: QueryClient, queryKeyBase: [
    parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ToolsetQueryData): ToolsetQueryData | undefined;
export declare function invalidateToolset(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllToolset(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=toolset.d.ts.map