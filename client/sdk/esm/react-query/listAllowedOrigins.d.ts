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
  ListAllowedOriginsRequest,
  ListAllowedOriginsSecurity,
} from "../models/operations/listallowedorigins.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListAllowedOriginsQuery,
  ListAllowedOriginsQueryData,
  prefetchListAllowedOrigins,
  queryKeyListAllowedOrigins,
} from "./listAllowedOrigins.core.js";
export {
  buildListAllowedOriginsQuery,
  type ListAllowedOriginsQueryData,
  prefetchListAllowedOrigins,
  queryKeyListAllowedOrigins,
};
export type ListAllowedOriginsQueryError =
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
 * listAllowedOrigins projects
 *
 * @remarks
 * List allowed origins for a project.
 */
export declare function useListAllowedOrigins(
  request?: ListAllowedOriginsRequest | undefined,
  security?: ListAllowedOriginsSecurity | undefined,
  options?: QueryHookOptions<
    ListAllowedOriginsQueryData,
    ListAllowedOriginsQueryError
  >,
): UseQueryResult<ListAllowedOriginsQueryData, ListAllowedOriginsQueryError>;
/**
 * listAllowedOrigins projects
 *
 * @remarks
 * List allowed origins for a project.
 */
export declare function useListAllowedOriginsSuspense(
  request?: ListAllowedOriginsRequest | undefined,
  security?: ListAllowedOriginsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListAllowedOriginsQueryData,
    ListAllowedOriginsQueryError
  >,
): UseSuspenseQueryResult<
  ListAllowedOriginsQueryData,
  ListAllowedOriginsQueryError
>;
export declare function setListAllowedOriginsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListAllowedOriginsQueryData,
): ListAllowedOriginsQueryData | undefined;
export declare function invalidateListAllowedOrigins(
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
export declare function invalidateAllListAllowedOrigins(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listAllowedOrigins.d.ts.map
