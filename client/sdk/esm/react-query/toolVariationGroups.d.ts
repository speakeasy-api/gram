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
  ListToolVariationGroupsRequest,
  ListToolVariationGroupsSecurity,
} from "../models/operations/listtoolvariationgroups.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildToolVariationGroupsQuery,
  prefetchToolVariationGroups,
  queryKeyToolVariationGroups,
  ToolVariationGroupsQueryData,
} from "./toolVariationGroups.core.js";
export {
  buildToolVariationGroupsQuery,
  prefetchToolVariationGroups,
  queryKeyToolVariationGroups,
  type ToolVariationGroupsQueryData,
};
export type ToolVariationGroupsQueryError =
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
 * listGroups variations
 *
 * @remarks
 * List the tool variation groups visible to the project. In v1 this returns the project-default group when it exists, or an empty list otherwise.
 */
export declare function useToolVariationGroups(
  request?: ListToolVariationGroupsRequest | undefined,
  security?: ListToolVariationGroupsSecurity | undefined,
  options?: QueryHookOptions<
    ToolVariationGroupsQueryData,
    ToolVariationGroupsQueryError
  >,
): UseQueryResult<ToolVariationGroupsQueryData, ToolVariationGroupsQueryError>;
/**
 * listGroups variations
 *
 * @remarks
 * List the tool variation groups visible to the project. In v1 this returns the project-default group when it exists, or an empty list otherwise.
 */
export declare function useToolVariationGroupsSuspense(
  request?: ListToolVariationGroupsRequest | undefined,
  security?: ListToolVariationGroupsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ToolVariationGroupsQueryData,
    ToolVariationGroupsQueryError
  >,
): UseSuspenseQueryResult<
  ToolVariationGroupsQueryData,
  ToolVariationGroupsQueryError
>;
export declare function setToolVariationGroupsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ToolVariationGroupsQueryData,
): ToolVariationGroupsQueryData | undefined;
export declare function invalidateToolVariationGroups(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllToolVariationGroups(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=toolVariationGroups.d.ts.map
