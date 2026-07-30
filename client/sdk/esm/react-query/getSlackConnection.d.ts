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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetSlackConnectionQuery,
  GetSlackConnectionQueryData,
  prefetchGetSlackConnection,
  queryKeyGetSlackConnection,
} from "./getSlackConnection.core.js";
export {
  buildGetSlackConnectionQuery,
  type GetSlackConnectionQueryData,
  prefetchGetSlackConnection,
  queryKeyGetSlackConnection,
};
export type GetSlackConnectionQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * getSlackConnection slack
 *
 * @remarks
 * get slack connection for an organization and project.
 */
export declare function useGetSlackConnection(
  request?: operations.GetSlackConnectionRequest | undefined,
  security?: operations.GetSlackConnectionSecurity | undefined,
  options?: QueryHookOptions<
    GetSlackConnectionQueryData,
    GetSlackConnectionQueryError
  >,
): UseQueryResult<GetSlackConnectionQueryData, GetSlackConnectionQueryError>;
/**
 * getSlackConnection slack
 *
 * @remarks
 * get slack connection for an organization and project.
 */
export declare function useGetSlackConnectionSuspense(
  request?: operations.GetSlackConnectionRequest | undefined,
  security?: operations.GetSlackConnectionSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetSlackConnectionQueryData,
    GetSlackConnectionQueryError
  >,
): UseSuspenseQueryResult<
  GetSlackConnectionQueryData,
  GetSlackConnectionQueryError
>;
export declare function setGetSlackConnectionData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetSlackConnectionQueryData,
): GetSlackConnectionQueryData | undefined;
export declare function invalidateGetSlackConnection(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetSlackConnection(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getSlackConnection.d.ts.map
