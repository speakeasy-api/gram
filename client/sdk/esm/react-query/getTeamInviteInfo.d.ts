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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetTeamInviteInfoQuery,
  GetTeamInviteInfoQueryData,
  prefetchGetTeamInviteInfo,
  queryKeyGetTeamInviteInfo,
} from "./getTeamInviteInfo.core.js";
export {
  buildGetTeamInviteInfoQuery,
  type GetTeamInviteInfoQueryData,
  prefetchGetTeamInviteInfo,
  queryKeyGetTeamInviteInfo,
};
export type GetTeamInviteInfoQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * getInviteInfo teams
 *
 * @remarks
 * Get information about a team invite by its token. Used to display invite details before accepting.
 */
export declare function useGetTeamInviteInfo(
  request: operations.GetTeamInviteInfoRequest,
  options?: QueryHookOptions<
    GetTeamInviteInfoQueryData,
    GetTeamInviteInfoQueryError
  >,
): UseQueryResult<GetTeamInviteInfoQueryData, GetTeamInviteInfoQueryError>;
/**
 * getInviteInfo teams
 *
 * @remarks
 * Get information about a team invite by its token. Used to display invite details before accepting.
 */
export declare function useGetTeamInviteInfoSuspense(
  request: operations.GetTeamInviteInfoRequest,
  options?: SuspenseQueryHookOptions<
    GetTeamInviteInfoQueryData,
    GetTeamInviteInfoQueryError
  >,
): UseSuspenseQueryResult<
  GetTeamInviteInfoQueryData,
  GetTeamInviteInfoQueryError
>;
export declare function setGetTeamInviteInfoData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      token: string;
    },
  ],
  data: GetTeamInviteInfoQueryData,
): GetTeamInviteInfoQueryData | undefined;
export declare function invalidateGetTeamInviteInfo(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        token: string;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetTeamInviteInfo(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getTeamInviteInfo.d.ts.map
