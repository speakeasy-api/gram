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
  GetInstanceRequest,
  GetInstanceSecurity,
} from "../models/operations/getinstance.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildInstanceQuery,
  InstanceQueryData,
  prefetchInstance,
  queryKeyInstance,
} from "./instance.core.js";
export {
  buildInstanceQuery,
  type InstanceQueryData,
  prefetchInstance,
  queryKeyInstance,
};
export type InstanceQueryError =
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
 * getInstance instances
 *
 * @remarks
 * Load all relevant data for an instance of a toolset and environment
 */
export declare function useInstance(
  request: GetInstanceRequest,
  security?: GetInstanceSecurity | undefined,
  options?: QueryHookOptions<InstanceQueryData, InstanceQueryError>,
): UseQueryResult<InstanceQueryData, InstanceQueryError>;
/**
 * getInstance instances
 *
 * @remarks
 * Load all relevant data for an instance of a toolset and environment
 */
export declare function useInstanceSuspense(
  request: GetInstanceRequest,
  security?: GetInstanceSecurity | undefined,
  options?: SuspenseQueryHookOptions<InstanceQueryData, InstanceQueryError>,
): UseSuspenseQueryResult<InstanceQueryData, InstanceQueryError>;
export declare function setInstanceData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      toolsetSlug: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
      gramKey?: string | undefined;
      gramChatSession?: string | undefined;
    },
  ],
  data: InstanceQueryData,
): InstanceQueryData | undefined;
export declare function invalidateInstance(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        toolsetSlug: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramKey?: string | undefined;
        gramChatSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllInstance(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=instance.d.ts.map
