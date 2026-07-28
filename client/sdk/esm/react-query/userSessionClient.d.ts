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
  GetUserSessionClientRequest,
  GetUserSessionClientSecurity,
} from "../models/operations/getusersessionclient.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildUserSessionClientQuery,
  prefetchUserSessionClient,
  queryKeyUserSessionClient,
  UserSessionClientQueryData,
} from "./userSessionClient.core.js";
export {
  buildUserSessionClientQuery,
  prefetchUserSessionClient,
  queryKeyUserSessionClient,
  type UserSessionClientQueryData,
};
export type UserSessionClientQueryError =
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
 * getUserSessionClient userSessionClients
 *
 * @remarks
 * Get a user_session_client by id.
 */
export declare function useUserSessionClient(
  request: GetUserSessionClientRequest,
  security?: GetUserSessionClientSecurity | undefined,
  options?: QueryHookOptions<
    UserSessionClientQueryData,
    UserSessionClientQueryError
  >,
): UseQueryResult<UserSessionClientQueryData, UserSessionClientQueryError>;
/**
 * getUserSessionClient userSessionClients
 *
 * @remarks
 * Get a user_session_client by id.
 */
export declare function useUserSessionClientSuspense(
  request: GetUserSessionClientRequest,
  security?: GetUserSessionClientSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    UserSessionClientQueryData,
    UserSessionClientQueryError
  >,
): UseSuspenseQueryResult<
  UserSessionClientQueryData,
  UserSessionClientQueryError
>;
export declare function setUserSessionClientData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: UserSessionClientQueryData,
): UserSessionClientQueryData | undefined;
export declare function invalidateUserSessionClient(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllUserSessionClient(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=userSessionClient.d.ts.map
