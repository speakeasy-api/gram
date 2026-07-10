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
  GetMcpServerRequest,
  GetMcpServerSecurity,
} from "../models/operations/getmcpserver.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetMcpServerQuery,
  GetMcpServerQueryData,
  prefetchGetMcpServer,
  queryKeyGetMcpServer,
} from "./getMcpServer.core.js";
export {
  buildGetMcpServerQuery,
  type GetMcpServerQueryData,
  prefetchGetMcpServer,
  queryKeyGetMcpServer,
};
export type GetMcpServerQueryError =
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
 * getMcpServer mcpServers
 *
 * @remarks
 * Get an MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function useGetMcpServer(
  request?: GetMcpServerRequest | undefined,
  security?: GetMcpServerSecurity | undefined,
  options?: QueryHookOptions<GetMcpServerQueryData, GetMcpServerQueryError>,
): UseQueryResult<GetMcpServerQueryData, GetMcpServerQueryError>;
/**
 * getMcpServer mcpServers
 *
 * @remarks
 * Get an MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function useGetMcpServerSuspense(
  request?: GetMcpServerRequest | undefined,
  security?: GetMcpServerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetMcpServerQueryData,
    GetMcpServerQueryError
  >,
): UseSuspenseQueryResult<GetMcpServerQueryData, GetMcpServerQueryError>;
export declare function setGetMcpServerData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id?: string | undefined;
      slug?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetMcpServerQueryData,
): GetMcpServerQueryData | undefined;
export declare function invalidateGetMcpServer(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetMcpServer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getMcpServer.d.ts.map
