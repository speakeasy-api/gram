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
  buildListTeamInvitesQuery,
  ListTeamInvitesQueryData,
  prefetchListTeamInvites,
  queryKeyListTeamInvites,
} from "./listTeamInvites.core.js";
export {
  buildListTeamInvitesQuery,
  type ListTeamInvitesQueryData,
  prefetchListTeamInvites,
  queryKeyListTeamInvites,
};
export type ListTeamInvitesQueryError =
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
 * listInvites teams
 *
 * @remarks
 * List pending invites for an organization.
 */
export declare function useListTeamInvites(
  request: operations.ListTeamInvitesRequest,
  security?: operations.ListTeamInvitesSecurity | undefined,
  options?: QueryHookOptions<
    ListTeamInvitesQueryData,
    ListTeamInvitesQueryError
  >,
): UseQueryResult<ListTeamInvitesQueryData, ListTeamInvitesQueryError>;
/**
 * listInvites teams
 *
 * @remarks
 * List pending invites for an organization.
 */
export declare function useListTeamInvitesSuspense(
  request: operations.ListTeamInvitesRequest,
  security?: operations.ListTeamInvitesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListTeamInvitesQueryData,
    ListTeamInvitesQueryError
  >,
): UseSuspenseQueryResult<ListTeamInvitesQueryData, ListTeamInvitesQueryError>;
export declare function setListTeamInvitesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      organizationId: string;
      gramSession?: string | undefined;
    },
  ],
  data: ListTeamInvitesQueryData,
): ListTeamInvitesQueryData | undefined;
export declare function invalidateListTeamInvites(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        organizationId: string;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListTeamInvites(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listTeamInvites.d.ts.map
