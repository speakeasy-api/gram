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
  ListTriggerInstancesRequest,
  ListTriggerInstancesSecurity,
} from "../models/operations/listtriggerinstances.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildTriggersQuery,
  prefetchTriggers,
  queryKeyTriggers,
  TriggersQueryData,
} from "./triggers.core.js";
export {
  buildTriggersQuery,
  prefetchTriggers,
  queryKeyTriggers,
  type TriggersQueryData,
};
export type TriggersQueryError =
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
 * listTriggerInstances triggers
 *
 * @remarks
 * List trigger instances for the current project.
 */
export declare function useTriggers(
  request?: ListTriggerInstancesRequest | undefined,
  security?: ListTriggerInstancesSecurity | undefined,
  options?: QueryHookOptions<TriggersQueryData, TriggersQueryError>,
): UseQueryResult<TriggersQueryData, TriggersQueryError>;
/**
 * listTriggerInstances triggers
 *
 * @remarks
 * List trigger instances for the current project.
 */
export declare function useTriggersSuspense(
  request?: ListTriggerInstancesRequest | undefined,
  security?: ListTriggerInstancesSecurity | undefined,
  options?: SuspenseQueryHookOptions<TriggersQueryData, TriggersQueryError>,
): UseSuspenseQueryResult<TriggersQueryData, TriggersQueryError>;
export declare function setTriggersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: TriggersQueryData,
): TriggersQueryData | undefined;
export declare function invalidateTriggers(
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
export declare function invalidateAllTriggers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=triggers.d.ts.map
