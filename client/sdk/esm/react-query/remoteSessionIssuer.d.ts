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
  GetRemoteSessionIssuerRequest,
  GetRemoteSessionIssuerSecurity,
} from "../models/operations/getremotesessionissuer.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRemoteSessionIssuerQuery,
  prefetchRemoteSessionIssuer,
  queryKeyRemoteSessionIssuer,
  RemoteSessionIssuerQueryData,
} from "./remoteSessionIssuer.core.js";
export {
  buildRemoteSessionIssuerQuery,
  prefetchRemoteSessionIssuer,
  queryKeyRemoteSessionIssuer,
  type RemoteSessionIssuerQueryData,
};
export type RemoteSessionIssuerQueryError =
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
 * getRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Get a remote_session_issuer by id or by slug. Provide exactly one.
 */
export declare function useRemoteSessionIssuer(
  request?: GetRemoteSessionIssuerRequest | undefined,
  security?: GetRemoteSessionIssuerSecurity | undefined,
  options?: QueryHookOptions<
    RemoteSessionIssuerQueryData,
    RemoteSessionIssuerQueryError
  >,
): UseQueryResult<RemoteSessionIssuerQueryData, RemoteSessionIssuerQueryError>;
/**
 * getRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Get a remote_session_issuer by id or by slug. Provide exactly one.
 */
export declare function useRemoteSessionIssuerSuspense(
  request?: GetRemoteSessionIssuerRequest | undefined,
  security?: GetRemoteSessionIssuerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RemoteSessionIssuerQueryData,
    RemoteSessionIssuerQueryError
  >,
): UseSuspenseQueryResult<
  RemoteSessionIssuerQueryData,
  RemoteSessionIssuerQueryError
>;
export declare function setRemoteSessionIssuerData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id?: string | undefined;
      slug?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RemoteSessionIssuerQueryData,
): RemoteSessionIssuerQueryData | undefined;
export declare function invalidateRemoteSessionIssuer(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRemoteSessionIssuer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=remoteSessionIssuer.d.ts.map
