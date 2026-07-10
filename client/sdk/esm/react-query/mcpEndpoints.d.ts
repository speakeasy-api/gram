import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  ListMcpEndpointsRequest,
  ListMcpEndpointsSecurity,
} from "../models/operations/listmcpendpoints.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildMcpEndpointsQuery,
  McpEndpointsQueryData,
  prefetchMcpEndpoints,
  queryKeyMcpEndpoints,
} from "./mcpEndpoints.core.js";
export {
  buildMcpEndpointsQuery,
  type McpEndpointsQueryData,
  prefetchMcpEndpoints,
  queryKeyMcpEndpoints,
};
export type McpEndpointsQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * listMcpEndpoints mcpEndpoints
 *
 * @remarks
 * List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.
 */
export declare function useMcpEndpoints(
  request?: ListMcpEndpointsRequest | undefined,
  security?: ListMcpEndpointsSecurity | undefined,
  options?: QueryHookOptions<McpEndpointsQueryData, McpEndpointsQueryError>,
): UseQueryResult<McpEndpointsQueryData, McpEndpointsQueryError>;
/**
 * listMcpEndpoints mcpEndpoints
 *
 * @remarks
 * List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.
 */
export declare function useMcpEndpointsSuspense(
  request?: ListMcpEndpointsRequest | undefined,
  security?: ListMcpEndpointsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    McpEndpointsQueryData,
    McpEndpointsQueryError
  >,
): UseSuspenseQueryResult<McpEndpointsQueryData, McpEndpointsQueryError>;
export declare function setMcpEndpointsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      mcpServerId?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: McpEndpointsQueryData,
): McpEndpointsQueryData | undefined;
export declare function invalidateMcpEndpoints(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        mcpServerId?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllMcpEndpoints(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=mcpEndpoints.d.ts.map
