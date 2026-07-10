import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolsetsForOrgRequest, ListToolsetsForOrgSecurity } from "../models/operations/listtoolsetsfororg.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListToolsetsForOrgQuery, ListToolsetsForOrgQueryData, prefetchListToolsetsForOrg, queryKeyListToolsetsForOrg } from "./listToolsetsForOrg.core.js";
export { buildListToolsetsForOrgQuery, type ListToolsetsForOrgQueryData, prefetchListToolsetsForOrg, queryKeyListToolsetsForOrg, };
export type ListToolsetsForOrgQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listToolsetsForOrg toolsets
 *
 * @remarks
 * List all toolsets across the organization (summary view)
 */
export declare function useListToolsetsForOrg(request?: ListToolsetsForOrgRequest | undefined, security?: ListToolsetsForOrgSecurity | undefined, options?: QueryHookOptions<ListToolsetsForOrgQueryData, ListToolsetsForOrgQueryError>): UseQueryResult<ListToolsetsForOrgQueryData, ListToolsetsForOrgQueryError>;
/**
 * listToolsetsForOrg toolsets
 *
 * @remarks
 * List all toolsets across the organization (summary view)
 */
export declare function useListToolsetsForOrgSuspense(request?: ListToolsetsForOrgRequest | undefined, security?: ListToolsetsForOrgSecurity | undefined, options?: SuspenseQueryHookOptions<ListToolsetsForOrgQueryData, ListToolsetsForOrgQueryError>): UseSuspenseQueryResult<ListToolsetsForOrgQueryData, ListToolsetsForOrgQueryError>;
export declare function setListToolsetsForOrgData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
], data: ListToolsetsForOrgQueryData): ListToolsetsForOrgQueryData | undefined;
export declare function invalidateListToolsetsForOrg(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListToolsetsForOrg(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listToolsetsForOrg.d.ts.map