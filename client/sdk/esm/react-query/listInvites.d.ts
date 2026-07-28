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
  ListInvitesRequest,
  ListInvitesSecurity,
} from "../models/operations/listinvites.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListInvitesQuery,
  ListInvitesQueryData,
  prefetchListInvites,
  queryKeyListInvites,
} from "./listInvites.core.js";
export {
  buildListInvitesQuery,
  type ListInvitesQueryData,
  prefetchListInvites,
  queryKeyListInvites,
};
export type ListInvitesQueryError =
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
 * listInvites organizations
 *
 * @remarks
 * List pending WorkOS invitations for the active organization.
 */
export declare function useListInvites(
  request?: ListInvitesRequest | undefined,
  security?: ListInvitesSecurity | undefined,
  options?: QueryHookOptions<ListInvitesQueryData, ListInvitesQueryError>,
): UseQueryResult<ListInvitesQueryData, ListInvitesQueryError>;
/**
 * listInvites organizations
 *
 * @remarks
 * List pending WorkOS invitations for the active organization.
 */
export declare function useListInvitesSuspense(
  request?: ListInvitesRequest | undefined,
  security?: ListInvitesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListInvitesQueryData,
    ListInvitesQueryError
  >,
): UseSuspenseQueryResult<ListInvitesQueryData, ListInvitesQueryError>;
export declare function setListInvitesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: ListInvitesQueryData,
): ListInvitesQueryData | undefined;
export declare function invalidateListInvites(
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
export declare function invalidateAllListInvites(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listInvites.d.ts.map
