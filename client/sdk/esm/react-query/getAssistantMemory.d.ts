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
  GetAssistantMemoryRequest,
  GetAssistantMemorySecurity,
} from "../models/operations/getassistantmemory.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetAssistantMemoryQuery,
  GetAssistantMemoryQueryData,
  prefetchGetAssistantMemory,
  queryKeyGetAssistantMemory,
} from "./getAssistantMemory.core.js";
export {
  buildGetAssistantMemoryQuery,
  type GetAssistantMemoryQueryData,
  prefetchGetAssistantMemory,
  queryKeyGetAssistantMemory,
};
export type GetAssistantMemoryQueryError =
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
 * getAssistantMemory assistantMemories
 *
 * @remarks
 * Get an assistant memory by ID.
 */
export declare function useGetAssistantMemory(
  request: GetAssistantMemoryRequest,
  security?: GetAssistantMemorySecurity | undefined,
  options?: QueryHookOptions<
    GetAssistantMemoryQueryData,
    GetAssistantMemoryQueryError
  >,
): UseQueryResult<GetAssistantMemoryQueryData, GetAssistantMemoryQueryError>;
/**
 * getAssistantMemory assistantMemories
 *
 * @remarks
 * Get an assistant memory by ID.
 */
export declare function useGetAssistantMemorySuspense(
  request: GetAssistantMemoryRequest,
  security?: GetAssistantMemorySecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetAssistantMemoryQueryData,
    GetAssistantMemoryQueryError
  >,
): UseSuspenseQueryResult<
  GetAssistantMemoryQueryData,
  GetAssistantMemoryQueryError
>;
export declare function setGetAssistantMemoryData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetAssistantMemoryQueryData,
): GetAssistantMemoryQueryData | undefined;
export declare function invalidateGetAssistantMemory(
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
export declare function invalidateAllGetAssistantMemory(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getAssistantMemory.d.ts.map
