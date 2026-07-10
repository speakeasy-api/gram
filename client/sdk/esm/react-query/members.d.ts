import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMembersRequest, ListMembersSecurity } from "../models/operations/listmembers.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildMembersQuery, MembersQueryData, prefetchMembers, queryKeyMembers } from "./members.core.js";
export { buildMembersQuery, type MembersQueryData, prefetchMembers, queryKeyMembers, };
export type MembersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listMembers access
 *
 * @remarks
 * List all team members with their role assignments.
 */
export declare function useMembers(request?: ListMembersRequest | undefined, security?: ListMembersSecurity | undefined, options?: QueryHookOptions<MembersQueryData, MembersQueryError>): UseQueryResult<MembersQueryData, MembersQueryError>;
/**
 * listMembers access
 *
 * @remarks
 * List all team members with their role assignments.
 */
export declare function useMembersSuspense(request?: ListMembersRequest | undefined, security?: ListMembersSecurity | undefined, options?: SuspenseQueryHookOptions<MembersQueryData, MembersQueryError>): UseSuspenseQueryResult<MembersQueryData, MembersQueryError>;
export declare function setMembersData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: MembersQueryData): MembersQueryData | undefined;
export declare function invalidateMembers(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllMembers(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=members.d.ts.map