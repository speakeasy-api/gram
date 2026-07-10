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
  ListServerNameOverridesRequest,
  ListServerNameOverridesSecurity,
} from "../models/operations/listservernameoverrides.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildHooksServerNamesListServerNameOverridesQuery,
  HooksServerNamesListServerNameOverridesQueryData,
  prefetchHooksServerNamesListServerNameOverrides,
  queryKeyHooksServerNamesListServerNameOverrides,
} from "./hooksServerNamesListServerNameOverrides.core.js";
export {
  buildHooksServerNamesListServerNameOverridesQuery,
  type HooksServerNamesListServerNameOverridesQueryData,
  prefetchHooksServerNamesListServerNameOverrides,
  queryKeyHooksServerNamesListServerNameOverrides,
};
export type HooksServerNamesListServerNameOverridesQueryError =
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
 * list hooksServerNames
 *
 * @remarks
 * List all server name display overrides for a project
 */
export declare function useHooksServerNamesListServerNameOverrides(
  request?: ListServerNameOverridesRequest | undefined,
  security?: ListServerNameOverridesSecurity | undefined,
  options?: QueryHookOptions<
    HooksServerNamesListServerNameOverridesQueryData,
    HooksServerNamesListServerNameOverridesQueryError
  >,
): UseQueryResult<
  HooksServerNamesListServerNameOverridesQueryData,
  HooksServerNamesListServerNameOverridesQueryError
>;
/**
 * list hooksServerNames
 *
 * @remarks
 * List all server name display overrides for a project
 */
export declare function useHooksServerNamesListServerNameOverridesSuspense(
  request?: ListServerNameOverridesRequest | undefined,
  security?: ListServerNameOverridesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    HooksServerNamesListServerNameOverridesQueryData,
    HooksServerNamesListServerNameOverridesQueryError
  >,
): UseSuspenseQueryResult<
  HooksServerNamesListServerNameOverridesQueryData,
  HooksServerNamesListServerNameOverridesQueryError
>;
export declare function setHooksServerNamesListServerNameOverridesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: HooksServerNamesListServerNameOverridesQueryData,
): HooksServerNamesListServerNameOverridesQueryData | undefined;
export declare function invalidateHooksServerNamesListServerNameOverrides(
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
export declare function invalidateAllHooksServerNamesListServerNameOverrides(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=hooksServerNamesListServerNameOverrides.d.ts.map
