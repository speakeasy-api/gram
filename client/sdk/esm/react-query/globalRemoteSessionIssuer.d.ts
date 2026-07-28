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
  GetGlobalRemoteSessionIssuerRequest,
  GetGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/getglobalremotesessionissuer.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGlobalRemoteSessionIssuerQuery,
  GlobalRemoteSessionIssuerQueryData,
  prefetchGlobalRemoteSessionIssuer,
  queryKeyGlobalRemoteSessionIssuer,
} from "./globalRemoteSessionIssuer.core.js";
export {
  buildGlobalRemoteSessionIssuerQuery,
  type GlobalRemoteSessionIssuerQueryData,
  prefetchGlobalRemoteSessionIssuer,
  queryKeyGlobalRemoteSessionIssuer,
};
export type GlobalRemoteSessionIssuerQueryError =
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
 * getGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Get a global remote_session_issuer by id. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuer(
  request: GetGlobalRemoteSessionIssuerRequest,
  security?: GetGlobalRemoteSessionIssuerSecurity | undefined,
  options?: QueryHookOptions<
    GlobalRemoteSessionIssuerQueryData,
    GlobalRemoteSessionIssuerQueryError
  >,
): UseQueryResult<
  GlobalRemoteSessionIssuerQueryData,
  GlobalRemoteSessionIssuerQueryError
>;
/**
 * getGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Get a global remote_session_issuer by id. Requires platform admin.
 */
export declare function useGlobalRemoteSessionIssuerSuspense(
  request: GetGlobalRemoteSessionIssuerRequest,
  security?: GetGlobalRemoteSessionIssuerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GlobalRemoteSessionIssuerQueryData,
    GlobalRemoteSessionIssuerQueryError
  >,
): UseSuspenseQueryResult<
  GlobalRemoteSessionIssuerQueryData,
  GlobalRemoteSessionIssuerQueryError
>;
export declare function setGlobalRemoteSessionIssuerData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
    },
  ],
  data: GlobalRemoteSessionIssuerQueryData,
): GlobalRemoteSessionIssuerQueryData | undefined;
export declare function invalidateGlobalRemoteSessionIssuer(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGlobalRemoteSessionIssuer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=globalRemoteSessionIssuer.d.ts.map
