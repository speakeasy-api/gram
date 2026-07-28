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
  GetUserSessionIssuerRequest,
  GetUserSessionIssuerSecurity,
} from "../models/operations/getusersessionissuer.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildUserSessionIssuerQuery,
  prefetchUserSessionIssuer,
  queryKeyUserSessionIssuer,
  UserSessionIssuerQueryData,
} from "./userSessionIssuer.core.js";
export {
  buildUserSessionIssuerQuery,
  prefetchUserSessionIssuer,
  queryKeyUserSessionIssuer,
  type UserSessionIssuerQueryData,
};
export type UserSessionIssuerQueryError =
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
 * getUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Get a user_session_issuer by id or by slug. Provide exactly one.
 */
export declare function useUserSessionIssuer(
  request?: GetUserSessionIssuerRequest | undefined,
  security?: GetUserSessionIssuerSecurity | undefined,
  options?: QueryHookOptions<
    UserSessionIssuerQueryData,
    UserSessionIssuerQueryError
  >,
): UseQueryResult<UserSessionIssuerQueryData, UserSessionIssuerQueryError>;
/**
 * getUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Get a user_session_issuer by id or by slug. Provide exactly one.
 */
export declare function useUserSessionIssuerSuspense(
  request?: GetUserSessionIssuerRequest | undefined,
  security?: GetUserSessionIssuerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    UserSessionIssuerQueryData,
    UserSessionIssuerQueryError
  >,
): UseSuspenseQueryResult<
  UserSessionIssuerQueryData,
  UserSessionIssuerQueryError
>;
export declare function setUserSessionIssuerData(
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
  data: UserSessionIssuerQueryData,
): UserSessionIssuerQueryData | undefined;
export declare function invalidateUserSessionIssuer(
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
export declare function invalidateAllUserSessionIssuer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=userSessionIssuer.d.ts.map
