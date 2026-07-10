import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetEmployeeDataFlowGraphRequest, GetEmployeeDataFlowGraphSecurity } from "../models/operations/getemployeedataflowgraph.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetEmployeeDataFlowGraphQuery, GetEmployeeDataFlowGraphQueryData, prefetchGetEmployeeDataFlowGraph, queryKeyGetEmployeeDataFlowGraph } from "./getEmployeeDataFlowGraph.core.js";
export { buildGetEmployeeDataFlowGraphQuery, type GetEmployeeDataFlowGraphQueryData, prefetchGetEmployeeDataFlowGraph, queryKeyGetEmployeeDataFlowGraph, };
export type GetEmployeeDataFlowGraphQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getEmployeeDataFlowGraph telemetry
 *
 * @remarks
 * Get an employee's MCP data flow graph across origins, clients, servers, and tools
 */
export declare function useGetEmployeeDataFlowGraph(request: GetEmployeeDataFlowGraphRequest, security?: GetEmployeeDataFlowGraphSecurity | undefined, options?: QueryHookOptions<GetEmployeeDataFlowGraphQueryData, GetEmployeeDataFlowGraphQueryError>): UseQueryResult<GetEmployeeDataFlowGraphQueryData, GetEmployeeDataFlowGraphQueryError>;
/**
 * getEmployeeDataFlowGraph telemetry
 *
 * @remarks
 * Get an employee's MCP data flow graph across origins, clients, servers, and tools
 */
export declare function useGetEmployeeDataFlowGraphSuspense(request: GetEmployeeDataFlowGraphRequest, security?: GetEmployeeDataFlowGraphSecurity | undefined, options?: SuspenseQueryHookOptions<GetEmployeeDataFlowGraphQueryData, GetEmployeeDataFlowGraphQueryError>): UseSuspenseQueryResult<GetEmployeeDataFlowGraphQueryData, GetEmployeeDataFlowGraphQueryError>;
export declare function setGetEmployeeDataFlowGraphData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetEmployeeDataFlowGraphQueryData): GetEmployeeDataFlowGraphQueryData | undefined;
export declare function invalidateGetEmployeeDataFlowGraph(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetEmployeeDataFlowGraph(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getEmployeeDataFlowGraph.d.ts.map