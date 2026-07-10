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
  ListAttributeKeysRequest,
  ListAttributeKeysSecurity,
} from "../models/operations/listattributekeys.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListAttributeKeysQuery,
  ListAttributeKeysQueryData,
  prefetchListAttributeKeys,
  queryKeyListAttributeKeys,
} from "./listAttributeKeys.core.js";
export {
  buildListAttributeKeysQuery,
  type ListAttributeKeysQueryData,
  prefetchListAttributeKeys,
  queryKeyListAttributeKeys,
};
export type ListAttributeKeysQueryError =
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
 * listAttributeKeys telemetry
 *
 * @remarks
 * List distinct attribute keys available for filtering
 */
export declare function useListAttributeKeys(
  request: ListAttributeKeysRequest,
  security?: ListAttributeKeysSecurity | undefined,
  options?: QueryHookOptions<
    ListAttributeKeysQueryData,
    ListAttributeKeysQueryError
  >,
): UseQueryResult<ListAttributeKeysQueryData, ListAttributeKeysQueryError>;
/**
 * listAttributeKeys telemetry
 *
 * @remarks
 * List distinct attribute keys available for filtering
 */
export declare function useListAttributeKeysSuspense(
  request: ListAttributeKeysRequest,
  security?: ListAttributeKeysSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListAttributeKeysQueryData,
    ListAttributeKeysQueryError
  >,
): UseSuspenseQueryResult<
  ListAttributeKeysQueryData,
  ListAttributeKeysQueryError
>;
export declare function setListAttributeKeysData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListAttributeKeysQueryData,
): ListAttributeKeysQueryData | undefined;
export declare function invalidateListAttributeKeys(
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
export declare function invalidateAllListAttributeKeys(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listAttributeKeys.d.ts.map
