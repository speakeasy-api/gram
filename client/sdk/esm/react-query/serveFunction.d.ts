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
  ServeFunctionRequest,
  ServeFunctionSecurity,
} from "../models/operations/servefunction.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildServeFunctionQuery,
  prefetchServeFunction,
  queryKeyServeFunction,
  ServeFunctionQueryData,
} from "./serveFunction.core.js";
export {
  buildServeFunctionQuery,
  prefetchServeFunction,
  queryKeyServeFunction,
  type ServeFunctionQueryData,
};
export type ServeFunctionQueryError =
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
 * serveFunction assets
 *
 * @remarks
 * Serve a Gram Functions asset from Gram.
 */
export declare function useServeFunction(
  request: ServeFunctionRequest,
  security?: ServeFunctionSecurity | undefined,
  options?: QueryHookOptions<ServeFunctionQueryData, ServeFunctionQueryError>,
): UseQueryResult<ServeFunctionQueryData, ServeFunctionQueryError>;
/**
 * serveFunction assets
 *
 * @remarks
 * Serve a Gram Functions asset from Gram.
 */
export declare function useServeFunctionSuspense(
  request: ServeFunctionRequest,
  security?: ServeFunctionSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ServeFunctionQueryData,
    ServeFunctionQueryError
  >,
): UseSuspenseQueryResult<ServeFunctionQueryData, ServeFunctionQueryError>;
export declare function setServeFunctionData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      projectId: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: ServeFunctionQueryData,
): ServeFunctionQueryData | undefined;
export declare function invalidateServeFunction(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        projectId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllServeFunction(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=serveFunction.d.ts.map
