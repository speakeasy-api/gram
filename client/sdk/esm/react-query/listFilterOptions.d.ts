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
  ListFilterOptionsRequest,
  ListFilterOptionsSecurity,
} from "../models/operations/listfilteroptions.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListFilterOptionsQuery,
  ListFilterOptionsQueryData,
  prefetchListFilterOptions,
  queryKeyListFilterOptions,
} from "./listFilterOptions.core.js";
export {
  buildListFilterOptionsQuery,
  type ListFilterOptionsQueryData,
  prefetchListFilterOptions,
  queryKeyListFilterOptions,
};
export type ListFilterOptionsQueryError =
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
 * listFilterOptions telemetry
 *
 * @remarks
 * List available filter options (API keys or users) for the observability overview
 */
export declare function useListFilterOptions(
  request: ListFilterOptionsRequest,
  security?: ListFilterOptionsSecurity | undefined,
  options?: QueryHookOptions<
    ListFilterOptionsQueryData,
    ListFilterOptionsQueryError
  >,
): UseQueryResult<ListFilterOptionsQueryData, ListFilterOptionsQueryError>;
/**
 * listFilterOptions telemetry
 *
 * @remarks
 * List available filter options (API keys or users) for the observability overview
 */
export declare function useListFilterOptionsSuspense(
  request: ListFilterOptionsRequest,
  security?: ListFilterOptionsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListFilterOptionsQueryData,
    ListFilterOptionsQueryError
  >,
): UseSuspenseQueryResult<
  ListFilterOptionsQueryData,
  ListFilterOptionsQueryError
>;
export declare function setListFilterOptionsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListFilterOptionsQueryData,
): ListFilterOptionsQueryData | undefined;
export declare function invalidateListFilterOptions(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListFilterOptions(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listFilterOptions.d.ts.map
