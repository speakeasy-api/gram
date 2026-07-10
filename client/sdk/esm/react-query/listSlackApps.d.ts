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
  buildListSlackAppsQuery,
  ListSlackAppsQueryData,
  prefetchListSlackApps,
  queryKeyListSlackApps,
} from "./listSlackApps.core.js";
export {
  buildListSlackAppsQuery,
  type ListSlackAppsQueryData,
  prefetchListSlackApps,
  queryKeyListSlackApps,
};
export type ListSlackAppsQueryError =
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
 * listSlackApps slack
 *
 * @remarks
 * List Slack apps for a project.
 */
export declare function useListSlackApps(
  request?: operations.ListSlackAppsRequest | undefined,
  security?: operations.ListSlackAppsSecurity | undefined,
  options?: QueryHookOptions<ListSlackAppsQueryData, ListSlackAppsQueryError>,
): UseQueryResult<ListSlackAppsQueryData, ListSlackAppsQueryError>;
/**
 * listSlackApps slack
 *
 * @remarks
 * List Slack apps for a project.
 */
export declare function useListSlackAppsSuspense(
  request?: operations.ListSlackAppsRequest | undefined,
  security?: operations.ListSlackAppsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListSlackAppsQueryData,
    ListSlackAppsQueryError
  >,
): UseSuspenseQueryResult<ListSlackAppsQueryData, ListSlackAppsQueryError>;
export declare function setListSlackAppsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListSlackAppsQueryData,
): ListSlackAppsQueryData | undefined;
export declare function invalidateListSlackApps(
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
export declare function invalidateAllListSlackApps(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listSlackApps.d.ts.map
