import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetProjectOverviewRequest, GetProjectOverviewSecurity } from "../models/operations/getprojectoverview.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetProjectOverviewQuery, GetProjectOverviewQueryData, prefetchGetProjectOverview, queryKeyGetProjectOverview } from "./getProjectOverview.core.js";
export { buildGetProjectOverviewQuery, type GetProjectOverviewQueryData, prefetchGetProjectOverview, queryKeyGetProjectOverview, };
export type GetProjectOverviewQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getProjectOverview telemetry
 *
 * @remarks
 * Get project-level overview including total chats, tool calls, active servers/users, and top lists
 */
export declare function useGetProjectOverview(request: GetProjectOverviewRequest, security?: GetProjectOverviewSecurity | undefined, options?: QueryHookOptions<GetProjectOverviewQueryData, GetProjectOverviewQueryError>): UseQueryResult<GetProjectOverviewQueryData, GetProjectOverviewQueryError>;
/**
 * getProjectOverview telemetry
 *
 * @remarks
 * Get project-level overview including total chats, tool calls, active servers/users, and top lists
 */
export declare function useGetProjectOverviewSuspense(request: GetProjectOverviewRequest, security?: GetProjectOverviewSecurity | undefined, options?: SuspenseQueryHookOptions<GetProjectOverviewQueryData, GetProjectOverviewQueryError>): UseSuspenseQueryResult<GetProjectOverviewQueryData, GetProjectOverviewQueryError>;
export declare function setGetProjectOverviewData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetProjectOverviewQueryData): GetProjectOverviewQueryData | undefined;
export declare function invalidateGetProjectOverview(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetProjectOverview(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getProjectOverview.d.ts.map