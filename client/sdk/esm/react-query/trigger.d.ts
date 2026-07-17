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
  GetTriggerInstanceRequest,
  GetTriggerInstanceSecurity,
} from "../models/operations/gettriggerinstance.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildTriggerQuery,
  prefetchTrigger,
  queryKeyTrigger,
  TriggerQueryData,
} from "./trigger.core.js";
export {
  buildTriggerQuery,
  prefetchTrigger,
  queryKeyTrigger,
  type TriggerQueryData,
};
export type TriggerQueryError =
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
 * getTriggerInstance triggers
 *
 * @remarks
 * Get a trigger instance by ID.
 */
export declare function useTrigger(
  request: GetTriggerInstanceRequest,
  security?: GetTriggerInstanceSecurity | undefined,
  options?: QueryHookOptions<TriggerQueryData, TriggerQueryError>,
): UseQueryResult<TriggerQueryData, TriggerQueryError>;
/**
 * getTriggerInstance triggers
 *
 * @remarks
 * Get a trigger instance by ID.
 */
export declare function useTriggerSuspense(
  request: GetTriggerInstanceRequest,
  security?: GetTriggerInstanceSecurity | undefined,
  options?: SuspenseQueryHookOptions<TriggerQueryData, TriggerQueryError>,
): UseSuspenseQueryResult<TriggerQueryData, TriggerQueryError>;
export declare function setTriggerData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: TriggerQueryData,
): TriggerQueryData | undefined;
export declare function invalidateTrigger(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllTrigger(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=trigger.d.ts.map
