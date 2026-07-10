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
  SessionInfoRequest,
  SessionInfoSecurity,
} from "../models/operations/sessioninfo.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildSessionInfoQuery,
  prefetchSessionInfo,
  queryKeySessionInfo,
  SessionInfoQueryData,
} from "./sessionInfo.core.js";
export {
  buildSessionInfoQuery,
  prefetchSessionInfo,
  queryKeySessionInfo,
  type SessionInfoQueryData,
};
export type SessionInfoQueryError =
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
 * info auth
 *
 * @remarks
 * Provides information about the current authentication status.
 */
export declare function useSessionInfo(
  request?: SessionInfoRequest | undefined,
  security?: SessionInfoSecurity | undefined,
  options?: QueryHookOptions<SessionInfoQueryData, SessionInfoQueryError>,
): UseQueryResult<SessionInfoQueryData, SessionInfoQueryError>;
/**
 * info auth
 *
 * @remarks
 * Provides information about the current authentication status.
 */
export declare function useSessionInfoSuspense(
  request?: SessionInfoRequest | undefined,
  security?: SessionInfoSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    SessionInfoQueryData,
    SessionInfoQueryError
  >,
): UseSuspenseQueryResult<SessionInfoQueryData, SessionInfoQueryError>;
export declare function setSessionInfoData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: SessionInfoQueryData,
): SessionInfoQueryData | undefined;
export declare function invalidateSessionInfo(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllSessionInfo(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=sessionInfo.d.ts.map
