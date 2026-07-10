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
  AccessScope,
  Disposition,
  ListShadowMCPAccessRulesRequest,
  ListShadowMCPAccessRulesSecurity,
} from "../models/operations/listshadowmcpaccessrules.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildShadowMCPAccessRulesQuery,
  prefetchShadowMCPAccessRules,
  queryKeyShadowMCPAccessRules,
  ShadowMCPAccessRulesQueryData,
} from "./shadowMCPAccessRules.core.js";
export {
  buildShadowMCPAccessRulesQuery,
  prefetchShadowMCPAccessRules,
  queryKeyShadowMCPAccessRules,
  type ShadowMCPAccessRulesQueryData,
};
export type ShadowMCPAccessRulesQueryError =
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
 * listShadowMCPAccessRules access
 *
 * @remarks
 * List managed Shadow MCP allow and deny rules.
 */
export declare function useShadowMCPAccessRules(
  request?: ListShadowMCPAccessRulesRequest | undefined,
  security?: ListShadowMCPAccessRulesSecurity | undefined,
  options?: QueryHookOptions<
    ShadowMCPAccessRulesQueryData,
    ShadowMCPAccessRulesQueryError
  >,
): UseQueryResult<
  ShadowMCPAccessRulesQueryData,
  ShadowMCPAccessRulesQueryError
>;
/**
 * listShadowMCPAccessRules access
 *
 * @remarks
 * List managed Shadow MCP allow and deny rules.
 */
export declare function useShadowMCPAccessRulesSuspense(
  request?: ListShadowMCPAccessRulesRequest | undefined,
  security?: ListShadowMCPAccessRulesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ShadowMCPAccessRulesQueryData,
    ShadowMCPAccessRulesQueryError
  >,
): UseSuspenseQueryResult<
  ShadowMCPAccessRulesQueryData,
  ShadowMCPAccessRulesQueryError
>;
export declare function setShadowMCPAccessRulesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      disposition?: Disposition | undefined;
      accessScope?: AccessScope | undefined;
      projectId?: string | undefined;
      limit?: number | undefined;
      cursor?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: ShadowMCPAccessRulesQueryData,
): ShadowMCPAccessRulesQueryData | undefined;
export declare function invalidateShadowMCPAccessRules(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        disposition?: Disposition | undefined;
        accessScope?: AccessScope | undefined;
        projectId?: string | undefined;
        limit?: number | undefined;
        cursor?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllShadowMCPAccessRules(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=shadowMCPAccessRules.d.ts.map
