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
  ListTunneledMcpServersRequest,
  ListTunneledMcpServersSecurity,
} from "../models/operations/listtunneledmcpservers.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildTunneledMcpServersQuery,
  prefetchTunneledMcpServers,
  queryKeyTunneledMcpServers,
  TunneledMcpServersQueryData,
} from "./tunneledMcpServers.core.js";
export {
  buildTunneledMcpServersQuery,
  prefetchTunneledMcpServers,
  queryKeyTunneledMcpServers,
  type TunneledMcpServersQueryData,
};
export type TunneledMcpServersQueryError =
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
 * listServers tunneledMcp
 *
 * @remarks
 * List all tunneled MCP server sources for a project
 */
export declare function useTunneledMcpServers(
  request?: ListTunneledMcpServersRequest | undefined,
  security?: ListTunneledMcpServersSecurity | undefined,
  options?: QueryHookOptions<
    TunneledMcpServersQueryData,
    TunneledMcpServersQueryError
  >,
): UseQueryResult<TunneledMcpServersQueryData, TunneledMcpServersQueryError>;
/**
 * listServers tunneledMcp
 *
 * @remarks
 * List all tunneled MCP server sources for a project
 */
export declare function useTunneledMcpServersSuspense(
  request?: ListTunneledMcpServersRequest | undefined,
  security?: ListTunneledMcpServersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    TunneledMcpServersQueryData,
    TunneledMcpServersQueryError
  >,
): UseSuspenseQueryResult<
  TunneledMcpServersQueryData,
  TunneledMcpServersQueryError
>;
export declare function setTunneledMcpServersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: TunneledMcpServersQueryData,
): TunneledMcpServersQueryData | undefined;
export declare function invalidateTunneledMcpServers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllTunneledMcpServers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=tunneledMcpServers.d.ts.map
