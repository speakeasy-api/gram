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
  ListProjectsRequest,
  ListProjectsSecurity,
} from "../models/operations/listprojects.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListProjectsQuery,
  ListProjectsQueryData,
  prefetchListProjects,
  queryKeyListProjects,
} from "./listProjects.core.js";
export {
  buildListProjectsQuery,
  type ListProjectsQueryData,
  prefetchListProjects,
  queryKeyListProjects,
};
export type ListProjectsQueryError =
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
 * listProjects projects
 *
 * @remarks
 * List all projects for an organization.
 */
export declare function useListProjects(
  request: ListProjectsRequest,
  security?: ListProjectsSecurity | undefined,
  options?: QueryHookOptions<ListProjectsQueryData, ListProjectsQueryError>,
): UseQueryResult<ListProjectsQueryData, ListProjectsQueryError>;
/**
 * listProjects projects
 *
 * @remarks
 * List all projects for an organization.
 */
export declare function useListProjectsSuspense(
  request: ListProjectsRequest,
  security?: ListProjectsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListProjectsQueryData,
    ListProjectsQueryError
  >,
): UseSuspenseQueryResult<ListProjectsQueryData, ListProjectsQueryError>;
export declare function setListProjectsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      organizationId: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: ListProjectsQueryData,
): ListProjectsQueryData | undefined;
export declare function invalidateListProjects(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        organizationId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListProjects(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listProjects.d.ts.map
