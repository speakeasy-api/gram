import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListPackagesRequest, ListPackagesSecurity } from "../models/operations/listpackages.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListPackagesQuery, ListPackagesQueryData, prefetchListPackages, queryKeyListPackages } from "./listPackages.core.js";
export { buildListPackagesQuery, type ListPackagesQueryData, prefetchListPackages, queryKeyListPackages, };
export type ListPackagesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listPackages packages
 *
 * @remarks
 * List all packages for a project.
 */
export declare function useListPackages(request?: ListPackagesRequest | undefined, security?: ListPackagesSecurity | undefined, options?: QueryHookOptions<ListPackagesQueryData, ListPackagesQueryError>): UseQueryResult<ListPackagesQueryData, ListPackagesQueryError>;
/**
 * listPackages packages
 *
 * @remarks
 * List all packages for a project.
 */
export declare function useListPackagesSuspense(request?: ListPackagesRequest | undefined, security?: ListPackagesSecurity | undefined, options?: SuspenseQueryHookOptions<ListPackagesQueryData, ListPackagesQueryError>): UseSuspenseQueryResult<ListPackagesQueryData, ListPackagesQueryError>;
export declare function setListPackagesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListPackagesQueryData): ListPackagesQueryData | undefined;
export declare function invalidateListPackages(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListPackages(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listPackages.d.ts.map