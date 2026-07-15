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
  ListRemoteMcpServersRequest,
  ListRemoteMcpServersSecurity,
} from "../models/operations/listremotemcpservers.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRemoteMcpServersQuery,
  prefetchRemoteMcpServers,
  queryKeyRemoteMcpServers,
  RemoteMcpServersQueryData,
} from "./remoteMcpServers.core.js";
export {
  buildRemoteMcpServersQuery,
  prefetchRemoteMcpServers,
  queryKeyRemoteMcpServers,
  type RemoteMcpServersQueryData,
};
export type RemoteMcpServersQueryError =
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
 * listServers remoteMcp
 *
 * @remarks
 * List all remote MCP servers for a project
 */
export declare function useRemoteMcpServers(
  request?: ListRemoteMcpServersRequest | undefined,
  security?: ListRemoteMcpServersSecurity | undefined,
  options?: QueryHookOptions<
    RemoteMcpServersQueryData,
    RemoteMcpServersQueryError
  >,
): UseQueryResult<RemoteMcpServersQueryData, RemoteMcpServersQueryError>;
/**
 * listServers remoteMcp
 *
 * @remarks
 * List all remote MCP servers for a project
 */
export declare function useRemoteMcpServersSuspense(
  request?: ListRemoteMcpServersRequest | undefined,
  security?: ListRemoteMcpServersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RemoteMcpServersQueryData,
    RemoteMcpServersQueryError
  >,
): UseSuspenseQueryResult<
  RemoteMcpServersQueryData,
  RemoteMcpServersQueryError
>;
export declare function setRemoteMcpServersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RemoteMcpServersQueryData,
): RemoteMcpServersQueryData | undefined;
export declare function invalidateRemoteMcpServers(
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
export declare function invalidateAllRemoteMcpServers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=remoteMcpServers.d.ts.map
