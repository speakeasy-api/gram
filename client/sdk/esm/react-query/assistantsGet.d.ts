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
  GetAssistantRequest,
  GetAssistantSecurity,
} from "../models/operations/getassistant.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  AssistantsGetQueryData,
  buildAssistantsGetQuery,
  prefetchAssistantsGet,
  queryKeyAssistantsGet,
} from "./assistantsGet.core.js";
export {
  type AssistantsGetQueryData,
  buildAssistantsGetQuery,
  prefetchAssistantsGet,
  queryKeyAssistantsGet,
};
export type AssistantsGetQueryError =
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
 * getAssistant assistants
 *
 * @remarks
 * Get an assistant by ID.
 */
export declare function useAssistantsGet(
  request: GetAssistantRequest,
  security?: GetAssistantSecurity | undefined,
  options?: QueryHookOptions<AssistantsGetQueryData, AssistantsGetQueryError>,
): UseQueryResult<AssistantsGetQueryData, AssistantsGetQueryError>;
/**
 * getAssistant assistants
 *
 * @remarks
 * Get an assistant by ID.
 */
export declare function useAssistantsGetSuspense(
  request: GetAssistantRequest,
  security?: GetAssistantSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    AssistantsGetQueryData,
    AssistantsGetQueryError
  >,
): UseSuspenseQueryResult<AssistantsGetQueryData, AssistantsGetQueryError>;
export declare function setAssistantsGetData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: AssistantsGetQueryData,
): AssistantsGetQueryData | undefined;
export declare function invalidateAssistantsGet(
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
export declare function invalidateAllAssistantsGet(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=assistantsGet.d.ts.map
