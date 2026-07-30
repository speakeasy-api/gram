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
  buildListTeamMembersQuery,
  ListTeamMembersQueryData,
  prefetchListTeamMembers,
  queryKeyListTeamMembers,
} from "./listTeamMembers.core.js";
export {
  buildListTeamMembersQuery,
  type ListTeamMembersQueryData,
  prefetchListTeamMembers,
  queryKeyListTeamMembers,
};
export type ListTeamMembersQueryError =
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
 * listMembers teams
 *
 * @remarks
 * List all members of an organization.
 */
export declare function useListTeamMembers(
  request: operations.ListTeamMembersRequest,
  security?: operations.ListTeamMembersSecurity | undefined,
  options?: QueryHookOptions<
    ListTeamMembersQueryData,
    ListTeamMembersQueryError
  >,
): UseQueryResult<ListTeamMembersQueryData, ListTeamMembersQueryError>;
/**
 * listMembers teams
 *
 * @remarks
 * List all members of an organization.
 */
export declare function useListTeamMembersSuspense(
  request: operations.ListTeamMembersRequest,
  security?: operations.ListTeamMembersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListTeamMembersQueryData,
    ListTeamMembersQueryError
  >,
): UseSuspenseQueryResult<ListTeamMembersQueryData, ListTeamMembersQueryError>;
export declare function setListTeamMembersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      organizationId: string;
      gramSession?: string | undefined;
    },
  ],
  data: ListTeamMembersQueryData,
): ListTeamMembersQueryData | undefined;
export declare function invalidateListTeamMembers(
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
export declare function invalidateAllListTeamMembers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listTeamMembers.d.ts.map
