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
  GetRemoteMcpServerRequest,
  GetRemoteMcpServerSecurity,
} from "../models/operations/getremotemcpserver.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetRemoteMcpServerQuery,
  GetRemoteMcpServerQueryData,
  prefetchGetRemoteMcpServer,
  queryKeyGetRemoteMcpServer,
} from "./getRemoteMcpServer.core.js";
export {
  buildGetRemoteMcpServerQuery,
  type GetRemoteMcpServerQueryData,
  prefetchGetRemoteMcpServer,
  queryKeyGetRemoteMcpServer,
};
export type GetRemoteMcpServerQueryError =
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
 * getServer remoteMcp
 *
 * @remarks
 * Get a remote MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function useGetRemoteMcpServer(
  request?: GetRemoteMcpServerRequest | undefined,
  security?: GetRemoteMcpServerSecurity | undefined,
  options?: QueryHookOptions<
    GetRemoteMcpServerQueryData,
    GetRemoteMcpServerQueryError
  >,
): UseQueryResult<GetRemoteMcpServerQueryData, GetRemoteMcpServerQueryError>;
/**
 * getServer remoteMcp
 *
 * @remarks
 * Get a remote MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function useGetRemoteMcpServerSuspense(
  request?: GetRemoteMcpServerRequest | undefined,
  security?: GetRemoteMcpServerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetRemoteMcpServerQueryData,
    GetRemoteMcpServerQueryError
  >,
): UseSuspenseQueryResult<
  GetRemoteMcpServerQueryData,
  GetRemoteMcpServerQueryError
>;
export declare function setGetRemoteMcpServerData(
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
  data: GetRemoteMcpServerQueryData,
): GetRemoteMcpServerQueryData | undefined;
export declare function invalidateGetRemoteMcpServer(
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
export declare function invalidateAllGetRemoteMcpServer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getRemoteMcpServer.d.ts.map
