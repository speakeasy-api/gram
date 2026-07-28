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
  ListRiskCategoriesRequest,
  ListRiskCategoriesSecurity,
} from "../models/operations/listriskcategories.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskCategoriesQuery,
  prefetchRiskCategories,
  queryKeyRiskCategories,
  RiskCategoriesQueryData,
} from "./riskCategories.core.js";
export {
  buildRiskCategoriesQuery,
  prefetchRiskCategories,
  queryKeyRiskCategories,
  type RiskCategoriesQueryData,
};
export type RiskCategoriesQueryError =
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
 * listRiskCategories risk
 *
 * @remarks
 * Return the canonical risk category definitions: metadata (label/description/icon) plus the classification (source / rule_id list / rule_id prefix) used to bucket findings. Dashboards and CLIs should call this instead of maintaining their own copy of the mapping.
 */
export declare function useRiskCategories(
  request?: ListRiskCategoriesRequest | undefined,
  security?: ListRiskCategoriesSecurity | undefined,
  options?: QueryHookOptions<RiskCategoriesQueryData, RiskCategoriesQueryError>,
): UseQueryResult<RiskCategoriesQueryData, RiskCategoriesQueryError>;
/**
 * listRiskCategories risk
 *
 * @remarks
 * Return the canonical risk category definitions: metadata (label/description/icon) plus the classification (source / rule_id list / rule_id prefix) used to bucket findings. Dashboards and CLIs should call this instead of maintaining their own copy of the mapping.
 */
export declare function useRiskCategoriesSuspense(
  request?: ListRiskCategoriesRequest | undefined,
  security?: ListRiskCategoriesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskCategoriesQueryData,
    RiskCategoriesQueryError
  >,
): UseSuspenseQueryResult<RiskCategoriesQueryData, RiskCategoriesQueryError>;
export declare function setRiskCategoriesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskCategoriesQueryData,
): RiskCategoriesQueryData | undefined;
export declare function invalidateRiskCategories(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskCategories(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskCategories.d.ts.map
